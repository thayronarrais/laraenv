package deploy

import (
	_ "embed"
	"net"
	"strings"

	"lara4ever/internal/i18n"
)

//go:embed scripts/database_access_common.sh
var databaseAccessCommon string

//go:embed scripts/database_access_mysql.sh
var databaseAccessMySQL string

//go:embed scripts/database_access_postgres.sh
var databaseAccessPostgres string

//go:embed scripts/database_bootstrap_postgres.sh
var databaseBootstrapPostgres string

//go:embed scripts/database_destroy_postgres.sh
var databaseDestroyPostgres string

// NormalizeDatabaseAccess also validates configurations received directly over
// the bridge. Never interpolate an unvalidated host into SQL or shell commands.
func (c *ApplyConfig) NormalizeDatabaseAccess() error {
	if !c.DatabaseRemoteAccess {
		c.DatabaseAllowedIPs = nil
		return nil
	}
	if c.Database != "mysql" && c.Database != "postgres" {
		return i18n.Errorf("errors.cloud.databaseRequiredForRemoteAccess", nil)
	}
	if c.Database == "mysql" && c.ProjectKind == "laravel" {
		user := strings.ReplaceAll(safeName(c.Name), "-", "_")
		switch user {
		case "root", "mysql", "postgres":
			return i18n.Errorf("errors.cloud.databaseProtectedAccount", nil)
		}
	}
	if len(c.DatabaseAllowedIPs) == 0 {
		return i18n.Errorf("errors.cloud.databaseAllowedIpsRequired", nil)
	}
	seen := make(map[string]bool)
	ips := make([]string, 0, len(c.DatabaseAllowedIPs))
	for _, raw := range c.DatabaseAllowedIPs {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return i18n.Errorf("errors.cloud.databaseAllowedIpInvalid", map[string]any{"ip": raw})
		}
		canonical := ip.String()
		if !seen[canonical] {
			seen[canonical] = true
			ips = append(ips, canonical)
		}
	}
	c.DatabaseAllowedIPs = ips
	return nil
}

func databaseAccessStep(cfg ApplyConfig) Step {
	body := databaseAccessMySQL
	if cfg.Database == "postgres" {
		body = databaseAccessPostgres
	}
	return Step{
		Title: "Configure database access", TitleKey: "deployments.steps.configureDatabaseAccess", Sudo: true,
		Cmd: databaseScript(cfg, body),
	}
}

func databaseScript(cfg ApplyConfig, body string) string {
	remote := "0"
	if cfg.DatabaseRemoteAccess {
		remote = "1"
	}
	script := "ENGINE=" + shQuote(cfg.Database) + "\nPROJECT=" + shQuote(safeName(cfg.Name)) + "\nDEPLOY_ROOT=" + shQuote("/var/www/"+safeName(cfg.Name)) + "\nREMOTE=" + remote + "\nALLOWED_IPS=" + shQuote(strings.Join(cfg.DatabaseAllowedIPs, " ")) + "\n" + databaseAccessCommon + "\n" + body
	// Windows builds must still send Unix shell scripts over SSH.
	return strings.ReplaceAll(script, "\r\n", "\n")
}

// Settings can remove or change the selected engine. Revoke any managed access
// on the previous engine as well; otherwise its accounts and port rules survive.
func databaseAccessCleanupSteps(name, exclude string) []Step {
	var steps []Step
	for _, engine := range []string{"mysql", "postgres"} {
		if engine == exclude {
			continue
		}
		cfg := ApplyConfig{Name: name, Database: engine}
		step := databaseAccessStep(cfg)
		step.Title = "Revoke previous " + engine + " database access"
		step.TitleKey = "deployments.steps.revokePreviousDatabaseAccess"
		step.TitleVars = map[string]any{"database": engine}
		prefix := shQuote("/etc/laraenv/database-access/" + engine + "/" + safeName(name))
		step.Cmd = "if [ -f " + prefix + ".remote ] || [ -f " + prefix + ".drop.sql ] || [ -f " + prefix + ".hba ]; then\n" + step.Cmd + "\nfi"
		steps = append(steps, step)
	}
	return steps
}
