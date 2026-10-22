package cron

import (
	"os"
	"strings"
)

// augmentPATH returns a copy of os.Environ with the PATH variable rewritten
// so the given dirs are prepended to the existing PATH (highest priority).
// Used to inject the right PHP/Node binaries before running the user's
// command — the same trick the integrated terminal uses.
func augmentPATH(prepend []string) []string {
	env := os.Environ()
	pathSep := string(os.PathListSeparator)

	prefix := strings.Join(prepend, pathSep)
	for i, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			env[i] = "PATH=" + prefix + pathSep + kv[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+prefix)
}
