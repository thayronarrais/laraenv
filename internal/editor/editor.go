// Package editor detects installed code editors and opens a project folder in one.
package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"lara4ever/internal/i18n"
)

type Kind string

const (
	KindVSCode   Kind = "vscode"
	KindCursor   Kind = "cursor"
	KindPhpStorm Kind = "phpstorm"
	KindSublime  Kind = "sublime"
	KindCustom   Kind = "custom"
)

type Profile struct {
	Kind      Kind   `json:"kind"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Path      string `json:"path"`
}

// Detect probes well-known install locations for each editor and reports availability.
func Detect() []Profile {
	profs := []Profile{
		{Kind: KindVSCode, Label: "VS Code"},
		{Kind: KindCursor, Label: "Cursor"},
		{Kind: KindPhpStorm, Label: "PhpStorm"},
		{Kind: KindSublime, Label: "Sublime Text"},
		{Kind: KindCustom, Label: i18n.T("backend.editor.customLabel", nil)},
	}
	for i := range profs {
		if profs[i].Kind == KindCustom {
			profs[i].Available = true
			continue
		}
		if p, ok := locate(profs[i].Kind); ok {
			profs[i].Available = true
			profs[i].Path = p
		}
	}
	return profs
}

func locate(k Kind) (string, bool) {
	local := os.Getenv("LOCALAPPDATA")
	progFiles := os.Getenv("ProgramFiles")
	progW6432 := os.Getenv("ProgramW6432")

	switch k {
	case KindVSCode:
		if p, err := exec.LookPath("code.cmd"); err == nil {
			return p, true
		}
		if p, err := exec.LookPath("code"); err == nil {
			return p, true
		}
		candidates := []string{
			filepath.Join(local, `Programs\Microsoft VS Code\Code.exe`),
			filepath.Join(progFiles, `Microsoft VS Code\Code.exe`),
			filepath.Join(progW6432, `Microsoft VS Code\Code.exe`),
		}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}

	case KindCursor:
		if p, err := exec.LookPath("cursor.cmd"); err == nil {
			return p, true
		}
		if p, err := exec.LookPath("cursor"); err == nil {
			return p, true
		}
		candidates := []string{
			filepath.Join(local, `Programs\cursor\Cursor.exe`),
			filepath.Join(local, `Programs\Cursor\Cursor.exe`),
			filepath.Join(progFiles, `Cursor\Cursor.exe`),
		}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}

	case KindPhpStorm:
		if p, err := exec.LookPath("phpstorm.bat"); err == nil {
			return p, true
		}
		if p, err := exec.LookPath("phpstorm64.exe"); err == nil {
			return p, true
		}
		// JetBrains Toolbox installs under %LOCALAPPDATA%\JetBrains\Toolbox\apps\PhpStorm
		toolboxBase := filepath.Join(local, `JetBrains\Toolbox\apps\PhpStorm`)
		if p := findInToolbox(toolboxBase, "phpstorm64.exe"); p != "" {
			return p, true
		}
		candidates := []string{
			filepath.Join(progFiles, `JetBrains\PhpStorm\bin\phpstorm64.exe`),
			filepath.Join(progW6432, `JetBrains\PhpStorm\bin\phpstorm64.exe`),
		}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}

	case KindSublime:
		if p, err := exec.LookPath("subl.exe"); err == nil {
			return p, true
		}
		if p, err := exec.LookPath("subl"); err == nil {
			return p, true
		}
		candidates := []string{
			`C:\Program Files\Sublime Text\subl.exe`,
			`C:\Program Files\Sublime Text 3\subl.exe`,
			`C:\Program Files\Sublime Text 4\subl.exe`,
			filepath.Join(local, `Programs\Sublime Text\subl.exe`),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}
	}
	return "", false
}

// findInToolbox walks up to two levels under base looking for exeName.
func findInToolbox(base, exeName string) string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// e.g. ch-0 / ch-1 / …
		sub := filepath.Join(base, e.Name())
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() {
				continue
			}
			p := filepath.Join(sub, s.Name(), "bin", exeName)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// Open launches the given editor at projectDir. customCmd is only used when
// profile.Kind == KindCustom; it is the bare executable name (e.g. "antigravity").
func Open(p Profile, projectDir, customCmd string) error {
	cmd, err := buildCommand(p, projectDir, projectDir, customCmd)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// OpenFile launches an editor for one file, using its parent as the working
// directory. The existing Open function continues to open a project folder.
func OpenFile(p Profile, filePath, customCmd string) error {
	cmd, err := openFileCommand(p, filePath, customCmd)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func openFileCommand(p Profile, filePath, customCmd string) (*exec.Cmd, error) {
	return buildCommand(p, filePath, filepath.Dir(filePath), customCmd)
}

func buildCommand(p Profile, argument, cwd, customCmd string) (*exec.Cmd, error) {
	var exe string
	switch p.Kind {
	case KindCustom:
		parts := strings.Fields(customCmd)
		if len(parts) == 0 {
			return nil, i18n.Errorf("errors.editor.noCustomCommand", nil)
		}
		exe = parts[0]
	default:
		if !p.Available || p.Path == "" {
			return nil, i18n.Errorf("errors.editor.notAvailable", map[string]any{"kind": p.Kind})
		}
		exe = p.Path
	}

	cmd := exec.Command(exe, argument)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd, nil
}
