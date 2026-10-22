package deploy

import (
	"encoding/json"
	"strings"
	"testing"
)

func accessConfig(t *testing.T, extra string) ApplyConfig {
	t.Helper()
	var cfg ApplyConfig
	if err := json.Unmarshal([]byte(`{"name":"my-app","project_kind":"laravel","database":"mysql",`+extra+`}`), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDatabaseAccessConfigurationRoundTrips(t *testing.T) {
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42","2001:db8::2"]`)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["database_remote_access"] != true {
		t.Fatal("remote-access choice was lost")
	}
	ips, ok := saved["database_allowed_ips"].([]any)
	if !ok || len(ips) != 2 || ips[0] != "203.0.113.42" || ips[1] != "2001:db8::2" {
		t.Fatalf("allowed IPs were lost: %v", saved)
	}
}

func TestDatabaseAccessRejectsInvalidConfigBeforeAnyWork(t *testing.T) {
	for _, extra := range []string{
		`"database_remote_access":true`,
		`"database_remote_access":true,"database_allowed_ips":["%"]`,
		`"database_remote_access":true,"database_allowed_ips":["0.0.0.0"]`,
		`"database_remote_access":true,"database_allowed_ips":["::"]`,
		`"database_remote_access":true,"database_allowed_ips":["203.0.113.42; touch /tmp/injected"]`,
	} {
		t.Run(extra, func(t *testing.T) {
			steps := BuildPlan(accessConfig(t, extra), nil, "", true)
			if len(steps) != 1 || !strings.Contains(steps[0].Cmd, "exit 1") || steps[0].Sudo {
				t.Fatalf("invalid access config can mutate the server: %v", stepTitles(steps))
			}
		})
	}
}

func TestDatabaseAccessPlanDefaultsToLocalAndSupportsBothEngines(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		for _, remote := range []bool{false, true} {
			extra := `"database_remote_access":false`
			if remote {
				extra = `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`
			}
			cfg := accessConfig(t, extra)
			cfg.Database = engine
			steps := BuildPlan(cfg, nil, "", true)
			access := planStep(t, steps, "Configure database access")
			if !access.Sudo || access.Ignore {
				t.Fatal("database access must require root and stop on failure")
			}
		}
	}
}

func TestDatabaseAccessRejectsProtectedBootstrapAccountBeforeAnyWork(t *testing.T) {
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Name = "root"
	steps := BuildPlan(cfg, nil, "", true)
	if len(steps) != 1 || !strings.Contains(steps[0].Cmd, "exit 1") {
		t.Fatal("remote configuration can bootstrap over a protected database account")
	}
}
