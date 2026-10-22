package projectroutes

import (
	"context"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lara4ever/internal/i18n"

	"github.com/google/uuid"
)

var hostLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type ProjectResolver func(context.Context, string) (Project, error)
type TargetResolver func(context.Context, Project, string) (Target, error)
type ApplyFunc func(context.Context, Project, []Route, []Route) error

type Manager struct {
	store   *Store
	project ProjectResolver
	target  TargetResolver
	apply   ApplyFunc
	now     func() time.Time
}

func NewManager(store *Store, project ProjectResolver, target TargetResolver, apply ApplyFunc) *Manager {
	return &Manager{store: store, project: project, target: target, apply: apply, now: time.Now}
}

func (m *Manager) List(ctx context.Context, projectName string) ([]Route, error) {
	project, err := m.project(ctx, projectName)
	if err != nil {
		return nil, err
	}
	routes := m.store.List(project.Name)
	if canonicalIndex(routes) >= 0 {
		return routes, nil
	}
	routes = append(routes, defaultCanonical(project, m.now().UTC()))
	if err := m.store.ReplaceProject(project.Name, routes); err != nil {
		return nil, err
	}
	return sortRoutes(routes), nil
}

// Stored returns persisted routes without resolving the project or mutating
// state. It is used while a project is being removed or global settings are
// being migrated.
func (m *Manager) Stored(projectName string) []Route {
	return m.store.List(projectName)
}

// Forget removes route metadata after the caller has removed its managed
// vhost, hosts and certificate artifacts.
func (m *Manager) Forget(projectName string) error {
	return m.store.ReplaceProject(projectName, nil)
}

func (m *Manager) MigrateTLD(ctx context.Context, projectName, oldTLD string) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	before := append([]Route(nil), routes...)
	oldSuffix := "." + strings.ToLower(project.Name) + "." + normalizeTLD(oldTLD)
	newSuffix := "." + strings.ToLower(project.Name) + "." + normalizeTLD(project.TLD)
	for index := range routes {
		if routes[index].Canonical {
			routes[index].Hostname = canonicalHostname(project)
		} else {
			host := strings.ToLower(routes[index].Hostname)
			if !strings.HasSuffix(host, oldSuffix) {
				return nil, i18n.Errorf("errors.projectroutes.previewHostnameDomainMismatch", map[string]any{"host": host})
			}
			routes[index].Hostname = strings.TrimSuffix(host, oldSuffix) + newSuffix
		}
		routes[index].SSL = project.SSL
		routes[index].UpdatedAt = m.now().UTC()
	}
	return m.commit(ctx, project, before, routes)
}

func (m *Manager) SetCanonical(ctx context.Context, projectName, targetPath string, proxyPort int) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	target, err := m.target(ctx, project, targetPath)
	if err != nil {
		return nil, err
	}
	if err := validateProxyIsolation(project, target, proxyPort); err != nil {
		return nil, err
	}
	index := canonicalIndex(routes)
	if index < 0 {
		return nil, i18n.Errorf("errors.projectroutes.canonicalRouteMissing", nil)
	}
	route := routes[index]
	route.TargetPath = target.Path
	route.TargetKind = target.Kind
	route.WorktreePath = ""
	if target.Kind == TargetWorktree {
		route.WorktreePath = target.Path
	}
	route.ProxyPort = resolvedProxyPort(project, target, proxyPort)
	route.ServeMode = serveMode(route.ProxyPort)
	route.UpdatedAt = m.now().UTC()
	routes[index] = route
	return m.commit(ctx, project, m.store.List(project.Name), routes)
}

func (m *Manager) RestoreCanonical(ctx context.Context, projectName string) ([]Route, error) {
	project, _, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	return m.SetCanonical(ctx, project.Name, project.MainPath, project.MainProxyPort)
}

// RepairMissingTargets restores a missing canonical owner to the main checkout
// and removes previews whose managed worktree no longer exists.
func (m *Manager) RepairMissingTargets(ctx context.Context, projectNames []string) ([]Repair, error) {
	var repairs []Repair
	for _, projectName := range projectNames {
		project, routes, err := m.load(ctx, projectName)
		if err != nil {
			return repairs, err
		}
		before := append([]Route(nil), routes...)
		next := make([]Route, 0, len(routes))
		changed := false
		for _, route := range routes {
			if _, err := m.target(ctx, project, route.TargetPath); err == nil {
				next = append(next, route)
				continue
			}
			changed = true
			if route.Canonical {
				restored := defaultCanonical(project, m.now().UTC())
				next = append(next, restored)
				repairs = append(repairs, Repair{ProjectName: project.Name, Message: i18n.T("backend.projectroutes.canonicalRestored", nil)})
				continue
			}
			repairs = append(repairs, Repair{ProjectName: project.Name, Message: i18n.T("backend.projectroutes.previewRemoved", map[string]any{"hostname": route.Hostname})})
		}
		if changed {
			if _, err := m.commit(ctx, project, before, next); err != nil {
				return repairs, err
			}
		}
	}
	return repairs, nil
}

func (m *Manager) CreatePreview(ctx context.Context, projectName, targetPath, hostname string, proxyPort int) (Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return Route{}, err
	}
	target, err := m.target(ctx, project, targetPath)
	if err != nil {
		return Route{}, err
	}
	if target.Kind != TargetWorktree {
		return Route{}, i18n.Errorf("errors.projectroutes.previewRequiresWorktree", nil)
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if err := ValidatePreviewHostname(hostname, project.Name, project.TLD); err != nil {
		return Route{}, err
	}
	for _, existing := range m.store.All() {
		if strings.EqualFold(existing.Hostname, hostname) {
			return Route{}, i18n.Errorf("errors.projectroutes.hostnameAlreadyUsed", map[string]any{"hostname": hostname, "project": existing.ProjectName})
		}
	}
	if err := validateProxyIsolation(project, target, proxyPort); err != nil {
		return Route{}, err
	}
	now := m.now().UTC()
	route := Route{
		ID: uuid.NewString(), ProjectName: project.Name, Hostname: hostname,
		TargetKind: TargetWorktree, TargetPath: target.Path, WorktreePath: target.Path,
		ProxyPort: proxyPort, ServeMode: serveMode(proxyPort), SSL: project.SSL, UpdatedAt: now,
	}
	before := append([]Route(nil), routes...)
	routes = append(routes, route)
	if _, err := m.commit(ctx, project, before, routes); err != nil {
		return Route{}, err
	}
	return route, nil
}

// ConfigureWorktree applies the launcher's optional canonical and preview
// choices as one route transaction, avoiding a half-applied UI operation.
func (m *Manager) ConfigureWorktree(ctx context.Context, projectName, targetPath string, canonical bool, previewHostname string, proxyPort int) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	target, err := m.target(ctx, project, targetPath)
	if err != nil {
		return nil, err
	}
	if target.Kind != TargetWorktree {
		return nil, i18n.Errorf("errors.projectroutes.worktreeRoutingRequiresWorktree", nil)
	}
	if canonical || strings.TrimSpace(previewHostname) != "" {
		if err := validateProxyIsolation(project, target, proxyPort); err != nil {
			return nil, err
		}
	}
	before := append([]Route(nil), routes...)
	now := m.now().UTC()
	if canonical {
		index := canonicalIndex(routes)
		if index < 0 {
			return nil, i18n.Errorf("errors.projectroutes.canonicalRouteMissing", nil)
		}
		routes[index].TargetKind = TargetWorktree
		routes[index].TargetPath = target.Path
		routes[index].WorktreePath = target.Path
		routes[index].ProxyPort = proxyPort
		routes[index].ServeMode = serveMode(proxyPort)
		routes[index].UpdatedAt = now
	}
	previewHostname = strings.ToLower(strings.TrimSpace(previewHostname))
	if previewHostname != "" {
		if err := ValidatePreviewHostname(previewHostname, project.Name, project.TLD); err != nil {
			return nil, err
		}
		for _, existing := range m.store.All() {
			if strings.EqualFold(existing.Hostname, previewHostname) {
				return nil, i18n.Errorf("errors.projectroutes.hostnameAlreadyUsed", map[string]any{"hostname": previewHostname, "project": existing.ProjectName})
			}
		}
		routes = append(routes, Route{
			ID: uuid.NewString(), ProjectName: project.Name, Hostname: previewHostname,
			TargetKind: TargetWorktree, TargetPath: target.Path, WorktreePath: target.Path,
			ProxyPort: proxyPort, ServeMode: serveMode(proxyPort), SSL: project.SSL, UpdatedAt: now,
		})
	}
	if !canonical && previewHostname == "" {
		return routes, nil
	}
	return m.commit(ctx, project, before, routes)
}

// UpdateWorktreeProxyPort moves every route for one worktree to the dev
// server's actual listening port. This is needed when the preferred port was
// occupied and the process manager safely selected the next free port.
func (m *Manager) UpdateWorktreeProxyPort(ctx context.Context, projectName, targetPath string, proxyPort int) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	target, err := m.target(ctx, project, targetPath)
	if err != nil {
		return nil, err
	}
	if target.Kind != TargetWorktree {
		return nil, i18n.Errorf("errors.projectroutes.proxyPortUpdateRequiresWorktree", nil)
	}
	if err := validateProxyIsolation(project, target, proxyPort); err != nil {
		return nil, err
	}
	before := append([]Route(nil), routes...)
	changed := false
	for index := range routes {
		if !strings.EqualFold(filepath.Clean(routes[index].TargetPath), filepath.Clean(target.Path)) {
			continue
		}
		routes[index].ProxyPort = proxyPort
		routes[index].ServeMode = ServeProxy
		routes[index].UpdatedAt = m.now().UTC()
		changed = true
	}
	if !changed {
		return nil, i18n.Errorf("errors.projectroutes.noRouteForWorktree", nil)
	}
	return m.commit(ctx, project, before, routes)
}

// DetachWorktree restores the canonical hostname to the main checkout (when
// needed) and removes every preview for the target in one route transaction.
// Callers use this before deleting a managed worktree.
func (m *Manager) DetachWorktree(ctx context.Context, projectName, targetPath string) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	target, err := m.target(ctx, project, targetPath)
	if err != nil {
		return nil, err
	}
	if target.Kind != TargetWorktree {
		return nil, i18n.Errorf("errors.projectroutes.routeCleanupRequiresWorktree", nil)
	}
	before := append([]Route(nil), routes...)
	next := make([]Route, 0, len(routes))
	changed := false
	for _, route := range routes {
		if !strings.EqualFold(filepath.Clean(route.TargetPath), filepath.Clean(target.Path)) {
			next = append(next, route)
			continue
		}
		changed = true
		if route.Canonical {
			next = append(next, defaultCanonical(project, m.now().UTC()))
		}
	}
	if !changed {
		return routes, nil
	}
	return m.commit(ctx, project, before, next)
}

func (m *Manager) Remove(ctx context.Context, projectName, routeID string) ([]Route, error) {
	project, routes, err := m.load(ctx, projectName)
	if err != nil {
		return nil, err
	}
	before := append([]Route(nil), routes...)
	result := make([]Route, 0, len(routes))
	found := false
	for _, route := range routes {
		if route.ID != routeID {
			result = append(result, route)
			continue
		}
		found = true
		if route.Canonical {
			return nil, i18n.Errorf("errors.projectroutes.canonicalRouteCannotBeRemoved", nil)
		}
	}
	if !found {
		return nil, i18n.Errorf("errors.projectroutes.routeNotFound", map[string]any{"id": routeID})
	}
	return m.commit(ctx, project, before, result)
}

func (m *Manager) SuggestPreview(projectName, tld, worktreeName string) string {
	label := sanitizeLabel(worktreeName)
	if label == "" {
		label = "preview"
	}
	return label + "." + strings.ToLower(projectName) + "." + normalizeTLD(tld)
}

func (m *Manager) load(ctx context.Context, projectName string) (Project, []Route, error) {
	project, err := m.project(ctx, projectName)
	if err != nil {
		return Project{}, nil, err
	}
	routes, err := m.List(ctx, project.Name)
	return project, routes, err
}

func (m *Manager) commit(ctx context.Context, project Project, before, after []Route) ([]Route, error) {
	after = sortRoutes(after)
	if err := validateRoutes(project, after); err != nil {
		return nil, err
	}
	if err := m.store.ReplaceProject(project.Name, after); err != nil {
		return nil, err
	}
	if m.apply != nil {
		if err := m.apply(ctx, project, before, after); err != nil {
			if rollbackErr := m.store.ReplaceProject(project.Name, before); rollbackErr != nil {
				return nil, i18n.Wrapf(err, "errors.projectroutes.applyRoutesFailed", map[string]any{"rollbackError": rollbackErr.Error()})
			}
			return nil, err
		}
	}
	return after, nil
}

func validateRoutes(project Project, routes []Route) error {
	canonical := 0
	hosts := map[string]bool{}
	for _, route := range routes {
		if !strings.EqualFold(route.ProjectName, project.Name) {
			return i18n.Errorf("errors.projectroutes.routeBelongsToAnotherProject", nil)
		}
		host := strings.ToLower(route.Hostname)
		if hosts[host] {
			return i18n.Errorf("errors.projectroutes.duplicateHostname", map[string]any{"host": host})
		}
		hosts[host] = true
		if route.Canonical {
			canonical++
			if host != canonicalHostname(project) {
				return i18n.Errorf("errors.projectroutes.canonicalHostnameMismatch", map[string]any{"expected": canonicalHostname(project)})
			}
		}
	}
	if canonical != 1 {
		return i18n.Errorf("errors.projectroutes.canonicalRouteCountInvalid", map[string]any{"count": canonical})
	}
	return nil
}

func ValidatePreviewHostname(hostname, projectName, tld string) error {
	if len(hostname) > 253 || net.ParseIP(hostname) != nil {
		return i18n.Errorf("errors.projectroutes.invalidPreviewHostname", map[string]any{"hostname": hostname})
	}
	labels := strings.Split(hostname, ".")
	if len(labels) < 3 {
		return i18n.Errorf("errors.projectroutes.previewHostnameMissingLabel", nil)
	}
	for _, label := range labels {
		if !hostLabelPattern.MatchString(label) {
			return i18n.Errorf("errors.projectroutes.invalidHostnameLabel", map[string]any{"label": label})
		}
	}
	suffix := "." + strings.ToLower(projectName) + "." + normalizeTLD(tld)
	if !strings.HasSuffix(hostname, suffix) || hostname == strings.TrimPrefix(suffix, ".") {
		return i18n.Errorf("errors.projectroutes.previewHostnameSuffixMismatch", map[string]any{"suffix": suffix})
	}
	first := labels[0]
	if first == "www" || first == "localhost" || first == "main" {
		return i18n.Errorf("errors.projectroutes.previewLabelReserved", map[string]any{"label": first})
	}
	return nil
}

func defaultCanonical(project Project, now time.Time) Route {
	return Route{
		ID: canonicalRouteID(project.Name), ProjectName: project.Name, Hostname: canonicalHostname(project), Canonical: true,
		TargetKind: TargetMainCheckout, TargetPath: filepath.Clean(project.MainPath),
		ProxyPort: project.MainProxyPort, ServeMode: serveMode(project.MainProxyPort), SSL: project.SSL, UpdatedAt: now,
	}
}

func canonicalRouteID(projectName string) string {
	return "canonical:" + strings.ToLower(strings.TrimSpace(projectName))
}

func canonicalHostname(project Project) string {
	return strings.ToLower(project.Name) + "." + normalizeTLD(project.TLD)
}

func normalizeTLD(tld string) string {
	tld = strings.Trim(strings.ToLower(strings.TrimSpace(tld)), ".")
	if tld == "" {
		return "test"
	}
	return tld
}

func serveMode(port int) ServeMode {
	if port > 0 {
		return ServeProxy
	}
	return ServeDocRoot
}

func resolvedProxyPort(project Project, target Target, requested int) int {
	if target.Kind == TargetMainCheckout && requested <= 0 {
		return project.MainProxyPort
	}
	return requested
}

func validateProxyIsolation(project Project, target Target, requested int) error {
	if requested < 0 || requested > 65535 {
		return i18n.Errorf("errors.projectroutes.proxyPortOutOfRange", nil)
	}
	if target.Kind != TargetWorktree || project.MainProxyPort <= 0 {
		return nil
	}
	if requested <= 0 {
		return i18n.Errorf("errors.projectroutes.dedicatedWorktreePortRequired", nil)
	}
	if requested == project.MainProxyPort {
		return i18n.Errorf("errors.projectroutes.worktreeProxyPortConflict", map[string]any{"port": requested})
	}
	return nil
}

func canonicalIndex(routes []Route) int {
	for index := range routes {
		if routes[index].Canonical {
			return index
		}
	}
	return -1
}

func sortRoutes(routes []Route) []Route {
	result := append([]Route(nil), routes...)
	for left := 0; left < len(result); left++ {
		for right := left + 1; right < len(result); right++ {
			if (!result[left].Canonical && result[right].Canonical) || (result[left].Canonical == result[right].Canonical && result[right].Hostname < result[left].Hostname) {
				result[left], result[right] = result[right], result[left]
			}
		}
	}
	return result
}

func sanitizeLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	dash := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			result.WriteRune(char)
			dash = false
		} else if result.Len() > 0 && !dash {
			result.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(result.String(), "-")
}
