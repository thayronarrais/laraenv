package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"lara4ever/internal/config"
	"lara4ever/internal/deploy"
	"lara4ever/internal/deployments"
	"lara4ever/internal/i18n"
	pkgssh "lara4ever/internal/ssh"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type deploymentRepository interface {
	List() ([]deployments.Deployment, error)
	Get(string) (*deployments.Deployment, error)
	Create(string, string) (*deployments.Deployment, error)
	Patch(string, map[string]any) (*deployments.Deployment, error)
	Delete(string) error
	StartRun(string, string) (*deployments.RunSummary, error)
	FinishRun(string, string, string, string, string) error
	ReadRunLog(string, string) (string, error)
	Import(deployments.ImportBundle) (deployments.ImportResult, error)
}

// Both implementations receive immutable snapshots. The production adapter
// opens SSH only after the run has been durably recorded by the caller.
type deploymentExecutor interface {
	Apply(config.SSHHost, []deploy.Step, func(string), func(int, int, string, string)) (string, error)
	Detect(config.SSHHost, func(string)) (*deploy.DetectionResult, error)
	Activity(config.SSHHost, string) (string, error)
}

type sshDeploymentExecutor struct{ manager *pkgssh.Manager }

func (a *App) acquireDeploymentOperation() (func(), error) {
	release, err := a.deploymentCoordinator.Acquire()
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.offlineDeployments.busy", nil)
	}
	return release, nil
}

func (e sshDeploymentExecutor) Apply(host config.SSHHost, steps []deploy.Step, line func(string), step func(int, int, string, string)) (string, error) {
	client, err := e.manager.Dial(host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return deploy.RunApply(client, steps, line, step)
}
func (e sshDeploymentExecutor) Detect(host config.SSHHost, line func(string)) (*deploy.DetectionResult, error) {
	client, err := e.manager.Dial(host)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return deploy.RunDetection(client, line)
}
func (e sshDeploymentExecutor) Activity(host config.SSHHost, path string) (string, error) {
	client, err := e.manager.Dial(host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput("tail -n 200 " + shQuote(path) + " 2>/dev/null || true")
	return string(out), err
}

func (a *App) deploymentStore() (deploymentRepository, error) {
	if a.localDeploymentsErr != nil {
		return nil, i18n.Wrapf(a.localDeploymentsErr, "errors.offlineDeployments.storageUnavailable", map[string]any{"error": a.localDeploymentsErr.Error()})
	}
	if a.localDeployments == nil {
		return nil, i18n.Errorf("errors.offlineDeployments.storageUnavailable", map[string]any{"error": "not initialized"})
	}
	return a.localDeployments, nil
}
func (a *App) emitDeployment(kind, event, id string, value any) {
	name := "deployment:" + kind + ":" + event + ":" + id
	if a.deploymentEmit != nil {
		a.deploymentEmit(name, value)
	} else if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, name, value)
	}
}
func (a *App) deploymentHost(name string) (config.SSHHost, error) {
	if a.cfg != nil {
		for _, h := range a.cfg.ListSSHHosts() {
			if h.Name == name {
				if h.PasswordUnavailable && !((h.AuthMode == "key" || h.AuthMode == "both") && h.KeyName != "") {
					return config.SSHHost{}, i18n.Errorf("errors.offlineDeployments.passwordUnavailable", nil)
				}
				h.Tags = append([]string(nil), h.Tags...)
				return h, nil
			}
		}
	}
	return config.SSHHost{}, i18n.Errorf("errors.app.sshHostNotFoundLocally", map[string]any{"host": name})
}

func (a *App) snapshotDeploymentExecutor(host config.SSHHost) (deploymentExecutor, []config.SSHHost, error) {
	executor := a.deploymentExecutor
	if executor == nil {
		return nil, nil, i18n.Errorf("errors.offlineDeployments.executorUnavailable", nil)
	}
	if ssh, ok := executor.(sshDeploymentExecutor); ok {
		manager, hosts, err := ssh.manager.Snapshot(host)
		if err != nil {
			return nil, nil, err
		}
		return sshDeploymentExecutor{manager: manager}, hosts, nil
	}
	return executor, []config.SSHHost{host}, nil
}

func (a *App) ListDeployments() ([]deployments.Deployment, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return nil, err
	}
	list, err := s.List()
	// Locked rows must be visible so the user can recover the original profile.
	if errors.Is(err, deployments.ErrSecretsUnavailable) {
		return list, nil
	}
	return list, err
}
func (a *App) GetDeployment(id string) (*deployments.Deployment, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func (a *App) CreateDeployment(name, host string) (*deployments.Deployment, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return nil, err
	}
	if _, err = a.deploymentHost(host); err != nil {
		return nil, err
	}
	return s.Create(name, host)
}

// DeleteDeployment removes only local registration/history. It never opens SSH.
func (a *App) DeleteDeployment(id string) error {
	s, err := a.deploymentStore()
	if err != nil {
		return err
	}
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	defer release()
	return s.Delete(id)
}
func (a *App) ReadDeploymentRunLog(id, runID string) (string, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return "", err
	}
	return s.ReadRunLog(id, runID)
}

func snapshotApply(cfg deploy.ApplyConfig) (deploy.ApplyConfig, error) {
	// Clone nested slices/maps before the asynchronous operation captures them.
	b, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	var out deploy.ApplyConfig
	err = json.Unmarshal(b, &out)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(out.Name) == "" {
		return out, i18n.Errorf("errors.offlineDeployments.nameRequired", nil)
	}
	if err = out.NormalizeDatabaseAccess(); err != nil {
		return out, err
	}
	out.GitURL = deploy.NormalizeGitURL(out.GitURL)
	// Old hosted configs sometimes embedded a PAT in userinfo. Preserve it only
	// in the protected credential field, never in the persisted URL.
	if u, err := url.Parse(out.GitURL); err == nil && u.User != nil && (u.Scheme == "https" || u.Scheme == "http") {
		if out.GitPAT == "" {
			if p, ok := u.User.Password(); ok {
				out.GitPAT = p
			} else {
				out.GitPAT = u.User.Username()
			}
		}
		u.User = nil
		out.GitURL = u.String()
	}
	return out, nil
}
func deploymentRedactor(cfg deploy.ApplyConfig, hosts ...config.SSHHost) func(string) string {
	secrets := []string{strings.TrimSpace(cfg.GitPAT)}
	if u, err := url.Parse(cfg.GitURL); err == nil && u.User != nil {
		if password, ok := u.User.Password(); ok {
			secrets = append(secrets, password)
		} else {
			secrets = append(secrets, u.User.Username())
		}
	}
	for _, host := range hosts {
		secrets = append(secrets, host.Password)
	}
	return func(s string) string {
		for _, secret := range secrets {
			if secret != "" {
				for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
					s = strings.ReplaceAll(s, value, "[REDACTED]")
				}
			}
		}
		return s
	}
}
func savedApply(d *deployments.Deployment) (deploy.ApplyConfig, error) {
	b, err := json.Marshal(d.Config["apply"])
	if err != nil {
		return deploy.ApplyConfig{}, err
	}
	var cfg deploy.ApplyConfig
	if json.Unmarshal(b, &cfg) != nil || cfg.Name == "" {
		return cfg, i18n.Errorf("errors.app.deploymentNoSavedConfig", nil)
	}
	return snapshotApply(cfg)
}

func (a *App) ApplyDeployment(id string, cfg deploy.ApplyConfig) error {
	cfg, err := snapshotApply(cfg)
	if err != nil {
		return err
	}
	return a.startLocalMutation(id, "apply", &cfg)
}
func (a *App) DeployDeployment(id string) error  { return a.startLocalMutation(id, "deploy", nil) }
func (a *App) DestroyDeployment(id string) error { return a.startLocalMutation(id, "destroy", nil) }

func (a *App) startLocalMutation(id, kind string, supplied *deploy.ApplyConfig) error {
	s, err := a.deploymentStore()
	if err != nil {
		return err
	}
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
		}
	}()
	d, err := s.Get(id)
	if err != nil {
		return err
	}
	host, err := a.deploymentHost(d.SSHHostExternalID)
	if err != nil {
		return err
	}
	var cfg deploy.ApplyConfig
	if supplied != nil {
		cfg = *supplied
	} else {
		cfg, err = savedApply(d)
		if err != nil {
			return err
		} // Deleting an unapplied registration is a separate explicit action.
	}
	executor, hosts, err := a.snapshotDeploymentExecutor(host)
	if err != nil {
		return err
	}
	redact := deploymentRedactor(cfg, hosts...)
	executionCfg := cfg
	executionCfg.GitURL = deploy.SpliceGitPAT(cfg.GitURL, strings.TrimSpace(cfg.GitPAT))
	var plan []deploy.Step
	switch kind {
	case "apply":
		rendered, renderErr := deploy.RenderDeployPipeline(executionCfg, a.presetScriptLookup)
		if renderErr != nil {
			return renderErr
		}
		var detection *deploy.DetectionResult
		if m, ok := d.Config["detection"].(map[string]any); ok {
			detection = decodeDetection(m)
		}
		plan = deploy.BuildPlan(executionCfg, detection, rendered.Pipeline, rendered.Legacy)
		if d.Config == nil {
			d.Config = map[string]any{}
		}
		d.Config["apply"] = cfg
		if _, err = s.Patch(id, map[string]any{"project_kind": cfg.ProjectKind, "config": d.Config}); err != nil {
			return err
		}
	case "deploy":
		rendered, renderErr := deploy.DeployNowScript(executionCfg, a.presetScriptLookup)
		if renderErr != nil {
			return renderErr
		}
		if rendered.Script == "" {
			return i18n.Errorf("errors.app.noDeployScriptConfigured", nil)
		}
		plan = []deploy.Step{{Title: "Deploy " + cfg.Name, TitleKey: "deployments.steps.deployProject", TitleVars: map[string]any{"name": cfg.Name}, Cmd: rendered.Script}}
	case "destroy":
		plan = deploy.BuildDestroyPlan(cfg)
	default:
		return i18n.Errorf("errors.offlineDeployments.unknownOperation", nil)
	}
	run, err := s.StartRun(id, kind)
	if err != nil {
		return err
	}
	launched = true
	go func() {
		defer release()
		a.emitDeployment(kind, "plan", id, deployStepInfos(plan))
		line := func(s string) { a.emitDeployment(kind, "log", id, redact(s)) }
		step := func(idx, total int, title, status string) {
			a.emitDeployment(kind, "step", id, map[string]any{"idx": idx, "total": total, "title": redact(title), "status": status})
		}
		log, remoteErr := executor.Apply(host, plan, line, step)
		status, message := "ok", ""
		if remoteErr != nil {
			status, message = "error", redact(remoteErr.Error())
		}
		if saveErr := s.FinishRun(id, run.ID, status, redact(log), message); saveErr != nil {
			outcome := i18n.T("errors.offlineDeployments.remoteSuccessHistoryFailed", map[string]any{"error": redact(saveErr.Error())})
			if remoteErr != nil {
				outcome = i18n.T("errors.offlineDeployments.remoteFailureHistoryFailed", map[string]any{"remoteError": message, "error": redact(saveErr.Error())})
			}
			a.emitDeployment(kind, "error", id, outcome)
			return
		}
		if remoteErr != nil {
			a.emitDeployment(kind, "error", id, message)
			return
		}
		if kind == "destroy" {
			if err := s.Delete(id); err != nil {
				a.emitDeployment(kind, "error", id, i18n.T("errors.offlineDeployments.remoteDeletedRegistrationFailed", map[string]any{"error": redact(err.Error())}))
				return
			}
		}
		a.emitDeployment(kind, "done", id, cfg.EffectiveDomain())
	}()
	return nil
}

func (a *App) DetectDeployment(id string) error {
	s, err := a.deploymentStore()
	if err != nil {
		return err
	}
	// Detection shares the gate so its config patch cannot race an apply/delete.
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
		}
	}()
	d, err := s.Get(id)
	if err != nil {
		return err
	}
	host, err := a.deploymentHost(d.SSHHostExternalID)
	if err != nil {
		return err
	}
	executor, hosts, err := a.snapshotDeploymentExecutor(host)
	if err != nil {
		return err
	}
	knownCfg, _ := savedApply(d)
	redact := deploymentRedactor(knownCfg, hosts...)
	run, err := s.StartRun(id, "detect")
	if err != nil {
		return err
	}
	launched = true
	go func() {
		defer release()
		result, remoteErr := executor.Detect(host, func(line string) { a.emitDeployment("detect", "log", id, redact(line)) })
		status, message, log := "ok", "", ""
		if result != nil {
			log = redact(result.Log)
		}
		if remoteErr != nil {
			status, message = "error", redact(remoteErr.Error())
		} else if result == nil {
			status, message = "error", i18n.T("errors.offlineDeployments.emptyDetection", nil)
		}
		if status == "ok" {
			if d.Config == nil {
				d.Config = map[string]any{}
			}
			d.Config["detection"] = result
			if _, err := s.Patch(id, map[string]any{"os_family": result.OSFamily, "os_version": result.OSVersion, "config": d.Config}); err != nil {
				status, message = "error", i18n.T("errors.offlineDeployments.detectionConfigFailed", map[string]any{"error": redact(err.Error())})
			}
		}
		if err := s.FinishRun(id, run.ID, status, log, message); err != nil {
			a.emitDeployment("detect", "error", id, i18n.T("errors.offlineDeployments.detectionHistoryFailed", map[string]any{"error": redact(err.Error())}))
			return
		}
		if status != "ok" {
			a.emitDeployment("detect", "error", id, message)
			return
		}
		a.emitDeployment("detect", "done", id, result)
	}()
	return nil
}

func (a *App) DeploymentActivity(id string) ([]ActivityEntry, error) {
	d, err := a.GetDeployment(id)
	if err != nil {
		return nil, err
	}
	cfg, err := savedApply(d)
	if err != nil {
		return nil, err
	}
	host, err := a.deploymentHost(d.SSHHostExternalID)
	if err != nil {
		return nil, err
	}
	executor, _, err := a.snapshotDeploymentExecutor(host)
	if err != nil {
		return nil, err
	}
	out, err := executor.Activity(host, "/tmp/laraenv-"+sanitizeName(cfg.Name)+".log")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	entries := []ActivityEntry{}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "[laraenv-autopull] ") {
			continue
		}
		p := strings.Fields(strings.TrimPrefix(line, "[laraenv-autopull] "))
		if len(p) == 4 && p[2] == "->" {
			entries = append(entries, ActivityEntry{At: p[0], Before: p[1], After: p[3]})
		}
	}
	return entries, nil
}

func (a *App) validateDeploymentImport(bundle *deployments.ImportBundle) error {
	for index := range bundle.Deployments {
		d := &bundle.Deployments[index]
		host, ok := bundle.HostMappings[d.SSHHostExternalID]
		if !ok || host == "" {
			return i18n.Errorf("errors.offlineDeployments.mappingRequired", map[string]any{"host": d.SSHHostExternalID})
		}
		mappedHost, err := a.deploymentHost(host)
		if err != nil {
			return err
		}
		knownHosts := []config.SSHHost{mappedHost}
		byName := map[string]config.SSHHost{}
		for _, saved := range a.cfg.ListSSHHosts() {
			byName[saved.Name] = saved
		}
		seen := map[string]bool{mappedHost.Name: true}
		for current := mappedHost; strings.TrimSpace(current.ProxyCommand) == "" && strings.TrimSpace(current.ProxyJump) != ""; {
			jump := strings.TrimSpace(current.ProxyJump)
			next, ok := byName[jump]
			if !ok || seen[jump] {
				break
			}
			seen[jump] = true
			knownHosts = append(knownHosts, next)
			current = next
		}
		var knownCfg deploy.ApplyConfig
		if d.Config != nil && d.Config["apply"] != nil {
			raw, _ := json.Marshal(d.Config["apply"])
			_ = json.Unmarshal(raw, &knownCfg)
			cfg, err := savedApply(d)
			if err != nil {
				return err
			}
			d.Config["apply"] = cfg
		}
		redact := deploymentRedactor(knownCfg, knownHosts...)
		for i := range d.Runs {
			d.Runs[i].ErrorMessage = redact(d.Runs[i].ErrorMessage)
		}
	}
	return nil
}
func (a *App) ImportDeploymentsJSON(raw string) (deployments.ImportResult, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return deployments.ImportResult{}, err
	}
	if len(raw) > deployments.MaxImportBytes {
		return deployments.ImportResult{}, deployments.ErrInvalidData
	}
	var bundle deployments.ImportBundle
	if err = json.Unmarshal([]byte(raw), &bundle); err != nil {
		return deployments.ImportResult{}, i18n.Wrapf(err, "errors.offlineDeployments.invalidImport", map[string]any{"error": err.Error()})
	}
	if err = a.validateDeploymentImport(&bundle); err != nil {
		return deployments.ImportResult{}, err
	}
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return deployments.ImportResult{}, err
	}
	defer release()
	return s.Import(bundle)
}
func (a *App) ImportCloudDeployments(ids []int, mappings map[string]string) (deployments.ImportResult, error) {
	s, err := a.deploymentStore()
	if err != nil {
		return deployments.ImportResult{}, err
	}
	opCtx, cancel, cl, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		return deployments.ImportResult{}, err
	}
	defer cancel()
	if cl.UserID <= 0 {
		return deployments.ImportResult{}, i18n.Errorf("errors.offlineDeployments.accountRequired", nil)
	}
	if len(ids) == 0 || len(ids) > 100 {
		return deployments.ImportResult{}, deployments.ErrInvalidData
	}
	bundle := deployments.ImportBundle{Version: deployments.SchemaVersion, Source: deployments.ImportSource{ServiceURL: a.cloud.Status().BaseURL, AccountID: strconv.Itoa(cl.UserID)}, HostMappings: mappings}
	for _, id := range ids {
		if id <= 0 {
			return deployments.ImportResult{}, deployments.ErrInvalidData
		}
		d, err := a.cloud.GetDeployment(opCtx, id)
		if err != nil {
			return deployments.ImportResult{}, err
		}
		if d.ID != id {
			return deployments.ImportResult{}, deployments.ErrInvalidData
		}
		local := deployments.Deployment{ID: strconv.Itoa(d.ID), Name: d.Name, SSHHostExternalID: d.SSHHostExternalID, OSFamily: d.OSFamily, OSVersion: d.OSVersion, ProjectKind: d.ProjectKind, Config: d.Config, LastStatus: d.LastStatus, LastRunAt: d.LastRunAt}
		// The hosted endpoint exposes summaries, never complete historical logs.
		for _, r := range d.Runs {
			local.Runs = append(local.Runs, deployments.RunSummary{ID: strconv.Itoa(r.ID), Kind: r.Kind, Status: r.Status, ErrorMessage: r.ErrorMessage, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, HasLog: false})
		}
		bundle.Deployments = append(bundle.Deployments, local)
	}
	current := a.cfg.Get().Cloud
	if !current.Enabled || current.Token != cl.Token || current.UserID != cl.UserID || current.BaseURL != cl.BaseURL {
		return deployments.ImportResult{}, i18n.Errorf("errors.offlineDeployments.profileChanged", nil)
	}
	if err = a.validateDeploymentImport(&bundle); err != nil {
		return deployments.ImportResult{}, err
	}
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return deployments.ImportResult{}, err
	}
	defer release()
	return s.Import(bundle)
}
