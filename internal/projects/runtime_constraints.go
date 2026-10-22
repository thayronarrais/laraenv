package projects

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This is a deliberately bounded subset of Composer/npm's version grammars.
// Unsupported syntax is rejected, never treated as a permissive constraint.
var runtimeVersionPattern = regexp.MustCompile(`^v?([0-9]+|[xX*])(?:\.([0-9]+|[xX*]))?(?:\.([0-9]+|[xX*]))?(?:-([0-9A-Za-z][0-9A-Za-z.-]*)|((?:alpha|beta|RC|rc|dev)[0-9.]*))?(?:\+([0-9A-Za-z.-]+))?$`)
var runtimePHPPrereleasePattern = regexp.MustCompile(`(?i)^(alpha|beta|rc)([0-9]+)$`)
var runtimeOperatorSpace = regexp.MustCompile(`(>=|<=|!=|>|<|=|\^|~)\s+`)

type runtimeVersion struct {
	numbers           [3]uint64
	parts, wildcard   int
	pre               string
	php               bool
	explicitStability bool
}

func (version runtimeVersion) series() string {
	return fmt.Sprintf("%d.%d", version.numbers[0], version.numbers[1])
}

func parseRuntimeVersion(raw, kind string) (runtimeVersion, error) {
	version := runtimeVersion{wildcard: -1, php: kind == "php"}
	matches := runtimeVersionPattern.FindStringSubmatch(raw)
	if matches == nil {
		return version, errors.New("invalid version")
	}
	for index := 0; index < 3; index++ {
		part := matches[index+1]
		if part == "" {
			break
		}
		version.parts++
		if part == "*" || strings.EqualFold(part, "x") {
			if version.wildcard < 0 {
				version.wildcard = index
			}
			continue
		}
		if version.wildcard >= 0 || (len(part) > 1 && part[0] == '0') {
			return version, errors.New("invalid numeric version")
		}
		number, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return version, err
		}
		version.numbers[index] = number
	}
	version.pre = matches[4]
	version.explicitStability = matches[4] != "" || matches[5] != ""
	if matches[5] != "" {
		if kind != "php" {
			return version, errors.New("invalid prerelease suffix")
		}
		version.pre = matches[5]
	}
	if kind == "php" && version.pre != "" {
		version.pre = strings.ToLower(version.pre)
		version.pre = runtimePHPPrereleasePattern.ReplaceAllString(version.pre, "$1.$2")
		if version.pre == "stable" {
			version.pre = ""
		}
	}
	if version.pre != "" && (version.parts != 3 || version.wildcard >= 0) {
		return version, errors.New("prerelease requires full version")
	}
	for _, identifier := range strings.Split(version.pre, ".") {
		if version.pre != "" && identifier == "" {
			return version, errors.New("empty prerelease identifier")
		}
		if numericIdentifier(identifier) && len(identifier) > 1 && identifier[0] == '0' {
			return version, errors.New("invalid prerelease number")
		}
	}
	if matches[6] != "" {
		for _, identifier := range strings.Split(matches[6], ".") {
			if identifier == "" {
				return version, errors.New("invalid build identifier")
			}
		}
	}
	return version, nil
}

func numericIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	for _, char := range identifier {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func compareRuntimeVersions(left, right runtimeVersion) int {
	for index := 0; index < 3; index++ {
		if left.numbers[index] < right.numbers[index] {
			return -1
		}
		if left.numbers[index] > right.numbers[index] {
			return 1
		}
	}
	if left.pre == right.pre {
		return 0
	}
	if left.pre == "" {
		return 1
	}
	if right.pre == "" {
		return -1
	}
	l, r := strings.Split(left.pre, "."), strings.Split(right.pre, ".")
	if left.php && right.php {
		// Composer orders PHP's named stability levels, unlike npm's lexical
		// ordering of arbitrary prerelease identifiers.
		ranks := map[string]int{"dev": 0, "alpha": 1, "beta": 2, "rc": 3}
		leftRank, leftKnown := ranks[l[0]]
		rightRank, rightKnown := ranks[r[0]]
		if leftKnown && rightKnown && leftRank != rightRank {
			if leftRank < rightRank {
				return -1
			}
			return 1
		}
	}
	for index := 0; index < len(l) && index < len(r); index++ {
		if l[index] == r[index] {
			continue
		}
		ln, rn := numericIdentifier(l[index]), numericIdentifier(r[index])
		if ln && rn {
			if len(l[index]) < len(r[index]) {
				return -1
			}
			if len(l[index]) > len(r[index]) {
				return 1
			}
		} else if ln {
			return -1
		} else if rn {
			return 1
		}
		if l[index] < r[index] {
			return -1
		}
		return 1
	}
	if len(l) < len(r) {
		return -1
	}
	return 1
}

type runtimeComparator struct {
	operator string
	version  runtimeVersion
}
type runtimeComparatorSet struct {
	comparisons     []runtimeComparator
	prereleaseBases map[[3]uint64]bool
}
type runtimeRange struct {
	sets []runtimeComparatorSet
	lts  bool
}

func parseRuntimeRange(raw, kind string, explicit bool) (runtimeRange, error) {
	result := runtimeRange{}
	if raw == "" || len(raw) > 4096 {
		return result, errors.New("empty or oversized constraint")
	}
	if kind == "node" && explicit {
		switch raw {
		case "node", "stable":
			raw = "*"
		case "lts/*":
			raw, result.lts = "*", true
		}
	}
	if kind == "php" {
		raw = strings.ReplaceAll(raw, "||", "|")
	} else {
		raw = strings.ReplaceAll(raw, "||", "\x00")
	}
	separator := "|"
	if kind == "node" {
		separator = "\x00"
	}
	for _, group := range strings.Split(raw, separator) {
		group = strings.TrimSpace(group)
		if group == "" {
			return result, errors.New("empty OR branch")
		}
		set := runtimeComparatorSet{prereleaseBases: map[[3]uint64]bool{}}
		if strings.Contains(group, " - ") {
			bounds := strings.Split(group, " - ")
			if len(bounds) != 2 {
				return result, errors.New("invalid hyphen range")
			}
			lower, err := parseRuntimeVersion(strings.TrimSpace(bounds[0]), kind)
			if err != nil || lower.wildcard >= 0 {
				return result, errors.New("invalid lower bound")
			}
			upper, err := parseRuntimeVersion(strings.TrimSpace(bounds[1]), kind)
			if err != nil || upper.wildcard >= 0 {
				return result, errors.New("invalid upper bound")
			}
			set.add(">=", lower)
			if upper.parts < 3 {
				set.addImplicitUpperBound(nextRuntimePartial(upper))
			} else {
				set.add("<=", upper)
			}
		} else {
			if kind == "php" {
				group = strings.ReplaceAll(group, ",", " ")
			}
			group = runtimeOperatorSpace.ReplaceAllString(group, "$1")
			for _, token := range strings.Fields(group) {
				if err := addRuntimeToken(&set, token, kind, explicit); err != nil {
					return result, err
				}
			}
			if len(strings.Fields(group)) == 0 {
				return result, errors.New("empty constraint")
			}
		}
		result.sets = append(result.sets, set)
	}
	return result, nil
}

func (set *runtimeComparatorSet) add(operator string, version runtimeVersion) {
	if version.pre != "" {
		set.prereleaseBases[version.numbers] = true
	}
	// Composer normalizes inclusive lower/exclusive upper bounds to dev;
	// e.g. <8.6 must also exclude 8.6 beta. Implicit dev is a boundary, not
	// permission to automatically select a prerelease runtime.
	if version.php && version.pre == "" && !version.explicitStability && (operator == ">=" || operator == "<") {
		version.pre = "dev"
	}
	set.comparisons = append(set.comparisons, runtimeComparator{operator, version})
}

func (set *runtimeComparatorSet) addImplicitUpperBound(version runtimeVersion) {
	if !version.php {
		// npm partial/caret/tilde bounds end before every prerelease of the
		// excluded version. The synthetic -0 boundary does not opt into beta.
		version.pre, version.parts = "0", 3
		set.comparisons = append(set.comparisons, runtimeComparator{"<", version})
		return
	}
	set.add("<", version)
}

func nextRuntimePartial(version runtimeVersion) runtimeVersion {
	index := version.parts - 1
	if version.wildcard >= 0 {
		index = version.wildcard - 1
	}
	if index < 0 {
		index = 0
	}
	version.numbers[index]++
	for index++; index < 3; index++ {
		version.numbers[index] = 0
	}
	version.pre, version.wildcard, version.parts = "", -1, 3
	version.explicitStability = false
	return version
}

func addRuntimeToken(set *runtimeComparatorSet, token, kind string, explicit bool) error {
	operator := ""
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(token, prefix) {
			operator, token = prefix, strings.TrimPrefix(token, prefix)
			break
		}
	}
	version, err := parseRuntimeVersion(token, kind)
	if err != nil {
		return err
	}
	if kind == "node" && operator == "!=" {
		return errors.New("unsupported npm operator")
	}
	if version.wildcard == 0 {
		if operator != "" && operator != "=" {
			return errors.New("operator on wildcard")
		}
		return nil
	}
	if operator == "^" || operator == "~" {
		if version.wildcard >= 0 {
			version.parts = version.wildcard
			version.wildcard = -1
		}
		upper := version
		upper.pre = ""
		upper.explicitStability = false
		index := 0
		if operator == "~" {
			if (kind == "php" && version.parts == 3) || (kind == "node" && version.parts >= 2) {
				index = 1
			}
		} else {
			for index < version.parts-1 && version.numbers[index] == 0 {
				index++
			}
		}
		upper.numbers[index]++
		for index++; index < 3; index++ {
			upper.numbers[index] = 0
		}
		set.add(">=", version)
		set.addImplicitUpperBound(upper)
		return nil
	}
	partialRange := version.wildcard >= 0 || ((kind == "node" || explicit) && version.parts < 3)
	if partialRange {
		upper := nextRuntimePartial(version)
		if version.wildcard >= 0 {
			version.parts = version.wildcard
			version.wildcard = -1
		}
		version.pre = ""
		switch operator {
		case "", "=":
			set.add(">=", version)
			set.addImplicitUpperBound(upper)
		case ">=":
			set.add(">=", version)
		case ">":
			set.add(">=", upper)
		case "<=":
			set.addImplicitUpperBound(upper)
		case "<":
			set.addImplicitUpperBound(version)
		default:
			return errors.New("unsupported wildcard operator")
		}
		return nil
	}
	if operator == "" {
		operator = "="
	}
	set.add(operator, version)
	return nil
}

func (constraint runtimeRange) matches(candidate RuntimeCandidate, kind string) bool {
	version, err := parseRuntimeVersion(candidate.FullVersion, kind)
	if err != nil || version.parts != 3 || version.wildcard >= 0 || (constraint.lts && !candidate.LTS) {
		return false
	}
	for _, set := range constraint.sets {
		// Automatic selection never opts into beta/RC just because an unbounded
		// stable range happens to include it. npm prereleases require the same
		// major/minor/patch base to appear explicitly in this comparator set.
		if candidate.Prerelease || version.pre != "" {
			if len(set.prereleaseBases) == 0 || (kind == "node" && !set.prereleaseBases[version.numbers]) {
				continue
			}
		}
		matches := true
		for _, comparison := range set.comparisons {
			order := compareRuntimeVersions(version, comparison.version)
			switch comparison.operator {
			case "=":
				matches = order == 0
			case "!=":
				matches = order != 0
			case ">":
				matches = order > 0
			case ">=":
				matches = order >= 0
			case "<":
				matches = order < 0
			case "<=":
				matches = order <= 0
			default:
				matches = false
			}
			if !matches {
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
