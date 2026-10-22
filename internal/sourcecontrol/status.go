package sourcecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"lara4ever/internal/workspacefiles"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validMode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}
func validXY(s string) bool {
	return len(s) == 2 && strings.ContainsRune(".MTADRCU", rune(s[0])) && strings.ContainsRune(".MTADRCU", rune(s[1]))
}
func validSub(s string) bool {
	return s == "N..." || (len(s) == 4 && s[0] == 'S' && strings.ContainsRune(".C", rune(s[1])) && strings.ContainsRune(".M", rune(s[2])) && strings.ContainsRune(".U", rune(s[3])))
}

func parseStatus(data []byte) (*Snapshot, error) {
	snap := &Snapshot{Entries: []Entry{}, Remotes: []Remote{}}
	if len(data) == 0 {
		return snap, nil
	}
	if data[len(data)-1] != 0 {
		return nil, errors.New("truncated porcelain status")
	}
	records := bytes.Split(data[:len(data)-1], []byte{0})
	seen := map[string]bool{}
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		if len(record) < 2 || record[1] != ' ' {
			return nil, errors.New("malformed porcelain record")
		}
		if record[0] == '#' {
			key, value, ok := strings.Cut(record[2:], " ")
			if !ok && strings.HasPrefix(key, "branch.") {
				return nil, errors.New("malformed status header")
			}
			switch key {
			case "branch.oid":
				if value == "(initial)" {
					snap.Unborn = true
				} else if validOID(value) {
					snap.HeadOID = value
				} else {
					return nil, errors.New("invalid HEAD oid")
				}
			case "branch.head":
				if value == "(detached)" {
					snap.Detached = true
				} else {
					snap.Branch = value
				}
			case "branch.upstream":
				snap.Upstream = value
			case "branch.ab":
				fields := strings.Fields(value)
				if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
					return nil, errors.New("invalid branch counts")
				}
				ahead, e1 := strconv.Atoi(fields[0][1:])
				behind, e2 := strconv.Atoi(fields[1][1:])
				if e1 != nil || e2 != nil || ahead < 0 || behind < 0 {
					return nil, errors.New("invalid branch counts")
				}
				snap.CountsKnown = true
				snap.Ahead = ahead
				snap.Behind = behind
			}
			continue
		}
		entry := Entry{}
		path, original := "", ""
		switch record[0] {
		case '?', '!':
			path = record[2:]
			entry.Kind = "untracked"
			entry.IndexCode = "?"
			entry.WorktreeCode = "?"
			entry.CanStage = record[0] == '?'
			if record[0] == '!' {
				continue
			}
		case '1', '2':
			count := 9
			if record[0] == '2' {
				count = 10
			}
			p := strings.SplitN(record, " ", count)
			if len(p) != count || !validXY(p[1]) || !validSub(p[2]) || !validMode(p[3]) || !validMode(p[4]) || !validMode(p[5]) || !validOID(p[6]) || !validOID(p[7]) {
				return nil, errors.New("malformed tracked status")
			}
			entry.Kind = "tracked"
			entry.IndexCode = p[1][:1]
			entry.WorktreeCode = p[1][1:]
			entry.SubmoduleState = p[2]
			entry.HeadMode = p[3]
			entry.IndexMode = p[4]
			entry.WorktreeMode = p[5]
			entry.HeadOID = p[6]
			entry.IndexOID = p[7]
			path = p[count-1]
			if record[0] == '2' {
				if len(p[8]) < 2 || !strings.ContainsRune("RC", rune(p[8][0])) {
					return nil, errors.New("invalid rename score")
				}
				score, err := strconv.Atoi(p[8][1:])
				if err != nil || score < 0 || score > 100 {
					return nil, errors.New("invalid rename score")
				}
				i++
				if i >= len(records) {
					return nil, errors.New("missing rename source")
				}
				original = string(records[i])
				if original == "" {
					return nil, errors.New("empty rename source")
				}
			}
			entry.CanStage = entry.WorktreeCode != "."
			entry.CanUnstage = entry.IndexCode != "."
		case 'u':
			p := strings.SplitN(record, " ", 11)
			if len(p) != 11 || !strings.Contains(" DD AU UD UA DU AA UU ", " "+p[1]+" ") || !validSub(p[2]) {
				return nil, errors.New("malformed conflict")
			}
			for _, m := range p[3:7] {
				if !validMode(m) {
					return nil, errors.New("invalid conflict mode")
				}
			}
			for _, oid := range p[7:10] {
				if !validOID(oid) {
					return nil, errors.New("invalid conflict oid")
				}
			}
			path = p[10]
			entry.Kind = "conflict"
			entry.IndexCode = p[1][:1]
			entry.WorktreeCode = p[1][1:]
			entry.SubmoduleState = p[2]
			entry.WorktreeMode = p[6]
			entry.DisabledReason = "Resolve conflicts before changing the index"
			snap.HasConflicts = true
		default:
			return nil, errors.New("unknown porcelain record")
		}
		if path == "" || seen[path] || strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") {
			return nil, errors.New("invalid status path")
		}
		seen[path] = true
		// Unsafe and undecodable names remain addressable only by their private hash.
		// No replacement-rune string is ever returned as a usable filesystem path.
		entry.ID = digest(path, original)
		entry.DisplayPath = path
		if !utf8.ValidString(path) || !utf8.ValidString(original) {
			entry.DisplayPath = strconv.QuoteToASCII(path)
			entry.Kind = "blocked"
			entry.CanStage = false
			entry.CanUnstage = false
			entry.DisabledReason = "Filename is not valid UTF-8"
		} else {
			if !workspacefiles.ValidRelativePath(path, false) || (original != "" && !workspacefiles.ValidRelativePath(original, false)) {
				for _, name := range []string{path, original} {
					for _, part := range strings.Split(name, "/") {
						if part == ".." || part == "." {
							return nil, errors.New("invalid status relative path")
						}
					}
				}
				entry.Kind = "blocked"
				entry.CanStage = false
				entry.CanUnstage = false
				entry.DisabledReason = "Filename cannot be opened safely"
			} else {
				entry.RelativePath = path
				entry.OriginalPath = original
			}
		}
		if entry.Kind != "blocked" && (entry.HeadMode == "160000" || entry.IndexMode == "160000" || entry.WorktreeMode == "160000" || strings.HasPrefix(entry.SubmoduleState, "S")) {
			entry.Kind = "gitlink"
			entry.CanStage = false
			entry.CanUnstage = false
			entry.DisabledReason = "Submodule changes are unsupported"
		}
		snap.Entries = append(snap.Entries, entry)
	}
	return snap, nil
}

func (s *Service) status(ctx context.Context, a *authority) Result {
	r, problem := s.command(ctx, a, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if problem.Code != "" {
		return problem
	}
	snap, err := parseStatus(r.Stdout)
	if err != nil {
		return issue("unsupported_format", err)
	}
	index, problem := s.command(ctx, a, "ls-files", "--stage", "-z")
	if problem.Code != "" {
		return problem
	}
	if err := validateIndex(index.Stdout); err != nil {
		return issue("unsupported_format", err)
	}
	snap.Repository = a.ref
	snap.Root = a.info
	snap.IndexRevision = digest(string(index.Stdout))
	snap.GitOperation = gitOperation(a)
	remotes, problem := s.remotes(ctx, a)
	if problem.Code != "" {
		return problem
	}
	snap.Remotes = remotes
	sort.Slice(snap.Entries, func(i, j int) bool { return snap.Entries[i].ID < snap.Entries[j].ID })
	normalized, err := json.Marshal(snap)
	if err != nil {
		return issue("unsupported_format", err)
	}
	// The DTO is larger than porcelain records. Reserve space for the result
	// envelope and revision itself so the wire response also stays bounded.
	if int64(len(normalized))+256 > structuredLimit {
		return issue("too_large", errors.New("Source control snapshot exceeds the structured output limit"))
	}
	snap.SnapshotRevision = digest(string(normalized))
	s.snapshotBusy(a, snap)
	return Result{Code: "ok", Snapshot: snap}
}
func gitOperation(a *authority) string {
	for _, candidate := range []struct{ path, name string }{{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"}, {"sequencer", "sequencer"}} {
		if _, err := a.git.Stat(candidate.path); err == nil {
			return candidate.name
		}
	}
	return ""
}
func (s *Service) remotes(ctx context.Context, a *authority) ([]Remote, Result) {
	remotes := []Remote{}
	r, problem := s.command(ctx, a, "remote")
	if problem.Code != "" {
		return nil, problem
	}
	for _, name := range strings.Split(strings.TrimSpace(string(r.Stdout)), "\n") {
		name = strings.TrimSuffix(name, "\r")
		if name == "" {
			continue
		}
		urls, problem := s.command(ctx, a, "remote", "get-url", "--push", "--all", name)
		if problem.Code != "" {
			return nil, problem
		}
		config, problem := s.command(ctx, a, "config", "--null", "--get-regexp", "^remote\\."+regexpQuote(name)+"\\.")
		if problem.Code != "" {
			return nil, problem
		}
		// Use Git's effective boolean value, including nonzero integers, bare
		// keys, explicit empty values, and config precedence. Invalid values
		// fail the command and cannot advertise a push-capable remote.
		mirror, problem := s.command(ctx, a, "config", "--type=bool", "--default=false", "--get", "remote."+name+".mirror")
		if problem.Code != "" {
			return nil, problem
		}
		dests := strings.Split(strings.TrimSuffix(string(urls.Stdout), "\n"), "\n")
		remote := Remote{Name: name, ID: digest(name, string(urls.Stdout), string(config.Stdout)), DisplayURL: redact(strings.Join(dests, ", ")), CanPush: len(dests) == 1 && dests[0] != ""}
		if !remote.CanPush {
			remote.DisabledReason = "Multiple or missing push destinations"
		}
		switch strings.TrimSpace(string(mirror.Stdout)) {
		case "true":
			remote.CanPush = false
			remote.DisabledReason = "Mirror remotes are unsupported"
		case "false":
		default:
			return nil, issue("unsupported_format", errors.New("Git returned an invalid mirror boolean"))
		}
		remotes = append(remotes, remote)
	}
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].Name < remotes[j].Name })
	return remotes, Result{}
}
func validateIndex(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[len(data)-1] != 0 {
		return errors.New("truncated index response")
	}
	for _, record := range bytes.Split(data[:len(data)-1], []byte{0}) {
		metadata, path, ok := strings.Cut(string(record), "\t")
		fields := strings.Split(metadata, " ")
		if !ok || path == "" || len(fields) != 3 || !validMode(fields[0]) || !validOID(fields[1]) || len(fields[2]) != 1 || fields[2][0] < '0' || fields[2][0] > '3' {
			return errors.New("malformed index response")
		}
	}
	return nil
}

func regexpQuote(s string) string {
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(`\.^$|?*+()[]{}`, c) {
			b.WriteByte('\\')
		}
		fmt.Fprint(&b, string(c))
	}
	return b.String()
}
