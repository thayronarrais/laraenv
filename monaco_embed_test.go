package main

import (
	"io/fs"
	"testing"
)

// The built-in editors render with Monaco, which @monaco-editor/react would
// otherwise fetch from cdn.jsdelivr.net on first use — leaving every editor
// stuck on "Loading..." whenever the machine cannot reach it (issue #30).
// frontend/scripts/vendor-monaco.mjs copies the AMD bundle into
// frontend/public/vs so `npm run build` emits it under dist/ and this embed
// carries it into the binary. A build that skipped that step ships an editor
// that only works online, so fail here rather than in front of a user.
func TestAssetsEmbedMonaco(t *testing.T) {
	for _, name := range []string{
		"frontend/dist/vs/loader.js",
		"frontend/dist/vs/editor/editor.main.js",
		"frontend/dist/vs/editor/editor.main.css",
		"frontend/dist/vs/base/worker/workerMain.js",
		"frontend/dist/vs/basic-languages/ini/ini.js",
	} {
		info, err := fs.Stat(assets, name)
		if err != nil {
			t.Errorf("%s is not embedded: %v (run `npm run vendor:monaco` and rebuild the frontend)", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is embedded but empty", name)
		}
	}
}
