package deploy

import (
	"embed"
	"strings"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
)

//go:embed presets/laravel-production.sh presets/laravel-development.sh presets/static-node.sh
var presetFS embed.FS

// BuiltinPresets returns the deploy presets compiled into the binary.
// They're read-only in the UI — users duplicate them into custom presets
// to adapt. IDs are stable ("builtin:<slug>") so deployments can reference
// them across app updates.
func BuiltinPresets() []config.DeployPreset {
	read := func(name string) string {
		b, err := presetFS.ReadFile("presets/" + name)
		if err != nil {
			// Embedded at compile time — can only fail if the path list above
			// drifts from the files on disk, which `go build` catches.
			return "# preset missing: " + name
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	return []config.DeployPreset{
		{
			ID:          "builtin:laravel-production",
			Name:        "Laravel Production",
			Description: i18n.T("backend.cloud.presetLaravelProductionDescription", nil),
			Script:      read("laravel-production.sh"),
			BuiltIn:     true,
		},
		{
			ID:          "builtin:laravel-development",
			Name:        "Laravel Development",
			Description: i18n.T("backend.cloud.presetLaravelDevelopmentDescription", nil),
			Script:      read("laravel-development.sh"),
			BuiltIn:     true,
		},
		{
			ID:          "builtin:static-node",
			Name:        "Static / Node",
			Description: i18n.T("backend.cloud.presetStaticNodeDescription", nil),
			Script:      read("static-node.sh"),
			BuiltIn:     true,
		},
	}
}

// FindPreset resolves a preset ID against built-ins first, then the user's
// custom presets in the config store. cfg may be nil (built-ins only).
func FindPreset(cfg *config.Store, id string) (*config.DeployPreset, bool) {
	if id == "" {
		return nil, false
	}
	for _, p := range BuiltinPresets() {
		if p.ID == id {
			p := p
			return &p, true
		}
	}
	if cfg != nil {
		for _, p := range cfg.ListDeployPresets() {
			if p.ID == id {
				p := p
				return &p, true
			}
		}
	}
	return nil, false
}
