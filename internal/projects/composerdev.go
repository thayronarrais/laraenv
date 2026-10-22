package projects

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lara4ever/internal/i18n"
)

// quotedRE matches a double-quoted segment inside a shell command line.
var quotedRE = regexp.MustCompile(`"[^"]*"`)

// namesRE matches concurrently's --names=<csv> flag.
var namesRE = regexp.MustCompile(`--names=([^\s]+)`)

// StripPailFromDevScript rewrites <projectPath>/composer.json so its `dev`
// script no longer runs `php artisan pail`. Laravel 11's default `dev` script
// runs Pail via `npx concurrently … --kill-others`, but Pail needs the
// POSIX-only `pcntl` extension, which has no Windows build — so on Windows it
// crashes instantly and (`--kill-others`) takes the whole dev stack down.
//
// Removing the Pail process (and its positional `--names` entry) makes
// `composer run dev` work natively on Windows. The edit is a targeted raw-bytes
// replacement so the rest of composer.json (key order, formatting) is preserved.
// Idempotent: a no-op when there's no Pail to remove. Returns (changed, summary).
func StripPailFromDevScript(projectPath string) (bool, string, error) {
	path := filepath.Join(projectPath, "composer.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, "", i18n.Wrapf(err, "errors.projects.readComposerJsonFailed", nil)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, "", i18n.Wrapf(err, "errors.projects.parseComposerJsonFailed", nil)
	}
	scriptsRaw, ok := doc["scripts"]
	if !ok {
		return false, "No `scripts` section in composer.json — nothing to change.", nil
	}
	var scripts map[string]json.RawMessage
	if err := json.Unmarshal(scriptsRaw, &scripts); err != nil {
		return false, "", i18n.Wrapf(err, "errors.projects.parseScriptsFailed", nil)
	}
	devRaw, ok := scripts["dev"]
	if !ok {
		return false, "No `dev` script in composer.json — nothing to change.", nil
	}

	// `dev` is usually an array of strings, but may be a single string.
	var elems []string
	if err := json.Unmarshal(devRaw, &elems); err != nil {
		var single string
		if err2 := json.Unmarshal(devRaw, &single); err2 != nil {
			return false, "", i18n.Errorf("errors.projects.unexpectedDevScriptShape", nil)
		}
		elems = []string{single}
	}

	out := raw
	changed := false
	for _, el := range elems {
		fixed, didChange := stripPailFromLine(el)
		if !didChange {
			continue
		}
		oldJSON := jsonEncodeString(el)
		newJSON := jsonEncodeString(fixed)
		if next := bytes.Replace(out, []byte(oldJSON), []byte(newJSON), 1); !bytes.Equal(next, out) {
			out = next
			changed = true
		}
	}

	if !changed {
		return false, "No `php artisan pail` found in the dev script — already Windows-safe.", nil
	}

	info, _ := os.Stat(path)
	mode := os.FileMode(0644)
	if info != nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return false, "", i18n.Wrapf(err, "errors.projects.writeComposerJsonFailed", nil)
	}
	return true, "Removed `php artisan pail` from the composer.json dev script — `composer run dev` is now Windows-safe.", nil
}

// stripPailFromLine removes the Pail process (and its positional --names entry)
// from a `concurrently` command line. Returns the cleaned line and whether it
// changed.
func stripPailFromLine(s string) (string, bool) {
	if !strings.Contains(s, "concurrently") || !strings.Contains(strings.ToLower(s), "pail") {
		return s, false
	}
	matches := quotedRE.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return s, false
	}

	// The quoted segment immediately after `-c` is concurrently's color list,
	// not a process; exclude it so process indexes line up with --names.
	colorsIdx := -1
	if ci := strings.Index(s, "-c"); ci >= 0 {
		for i, m := range matches {
			if m[0] > ci {
				colorsIdx = i
				break
			}
		}
	}

	// Walk the process segments (everything quoted except the colors), find the
	// Pail one, and record its position among processes.
	pailMatch := -1
	pailPos := -1
	pos := 0
	for i, m := range matches {
		if i == colorsIdx {
			continue
		}
		seg := strings.ToLower(s[m[0]:m[1]])
		if strings.Contains(seg, "artisan pail") {
			pailMatch = i
			pailPos = pos
			break
		}
		pos++
	}
	if pailMatch < 0 {
		return s, false
	}

	// Remove the Pail quoted segment plus one adjacent space.
	start, end := matches[pailMatch][0], matches[pailMatch][1]
	if start > 0 && s[start-1] == ' ' {
		start--
	} else if end < len(s) && s[end] == ' ' {
		end++
	}
	out := s[:start] + s[end:]

	// Drop the Pail name from --names=<csv> at the same position.
	out = namesRE.ReplaceAllStringFunc(out, func(m string) string {
		csv := namesRE.FindStringSubmatch(m)[1]
		names := strings.Split(csv, ",")
		if pailPos >= 0 && pailPos < len(names) {
			names = append(names[:pailPos], names[pailPos+1:]...)
		}
		return "--names=" + strings.Join(names, ",")
	})
	return out, true
}

// jsonEncodeString returns the JSON string literal for s without HTML escaping
// and without a trailing newline, matching how composer.json stores values.
func jsonEncodeString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}
