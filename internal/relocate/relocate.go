// Package relocate follows an environment that has been moved, rewriting the
// absolute paths stored in the app's JSON state files.
//
// Most derived configuration repairs itself on the next start - the vhost
// sweep rebuilds every vhost, hosts entry and certificate; EnsureCaCert and
// EnsureIni rewrite the PHP paths. What cannot repair itself is state the app
// persisted: it is the record of where things were, so there is nothing to
// re-derive it from.
package relocate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"lara4ever/internal/paths"
)

// JSONFile rewrites every string in the file that names a path under oldRoot,
// returning how many it moved. A missing file is not an error: a fresh
// install has not written its state yet.
//
// Values are matched by content rather than by field name. These files gain
// fields over time, and a hardcoded list of names would silently go stale -
// leaving exactly the kind of stale absolute path this package exists to fix.
//
// The file is decoded and re-encoded, so key order is not preserved. That is
// acceptable for machine-written state and buys correctness over any string
// substitution on escaped JSON text.
func JSONFile(path, oldRoot, newRoot string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		// Leave the file exactly as it was rather than risk truncating state
		// we could not read.
		return 0, err
	}

	moved := 0
	doc = walk(doc, oldRoot, newRoot, &moved)
	if moved == 0 {
		return 0, nil
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return 0, err
	}
	return moved, nil
}

func walk(node any, oldRoot, newRoot string, moved *int) any {
	switch v := node.(type) {
	case string:
		if rebased, ok := paths.Rebase(oldRoot, newRoot, v); ok {
			*moved++
			return rebased
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = walk(item, oldRoot, newRoot, moved)
		}
		return v
	case map[string]any:
		for k, item := range v {
			v[k] = walk(item, oldRoot, newRoot, moved)
		}
		return v
	default:
		return node
	}
}
