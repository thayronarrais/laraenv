package main

import (
	"fmt"
	"os"
	"path/filepath"

	"lara4ever/internal/config"
	"lara4ever/internal/localtls"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/vhost"
)

func (a *App) configureLocalTLS() {
	a.devsrv.SetEnvironmentResolver(a.localProjectTLSEnvironment)
	a.queue.SetEnvironmentResolver(a.localProjectTLSEnvironment)
	a.crons.SetEnvironmentResolver(a.localProjectTLSEnvironment)
	a.terminals.SetEnvironmentResolver(a.localTerminalTLSEnvironment)
}

func (a *App) localTerminalTLSEnvironment(cwd string, env []string) ([]string, error) {
	name := ""
	if a.projects != nil && cwd != "" {
		name = a.projectNameForRuntimePath(cwd)
	}
	return a.localProjectTLSEnvironment(name, cwd, env)
}

func (a *App) localProjectTLSEnvironment(name, cwd string, env []string) ([]string, error) {
	var routes []projectroutes.Route
	if a.routes != nil && name != "" {
		routes = a.routes.Stored(name)
	}
	var project *localtls.Project
	if a.cfg != nil {
		project = localTLSProject(a.cfg.Get().Projects, name, cwd, routes)
	}
	return localtls.Environment(env, vhost.LocalCACertPath(paths.NginxCerts()), project)
}

func localTLSProject(prefs config.Projects, name, cwd string, routes []projectroutes.Route) *localtls.Project {
	if name == "" || cwd == "" || prefs.Paused[name] || !prefs.SSL[name] {
		return nil
	}
	host := ""
	if routesNeedManagedApply(routes, filepath.Join(prefs.Root, name)) {
		for _, route := range routes {
			// Route ownership is persisted separately from the project's current
			// SSL and proxy preferences. Match the effective vhost configuration,
			// rather than stale preference values captured when a route was saved.
			proxyPort := route.ProxyPort
			if route.TargetKind == projectroutes.TargetMainCheckout && prefs.ProxyPorts[name] > 0 {
				proxyPort = prefs.ProxyPorts[name]
			}
			if proxyPort == 0 && sameWindowsPath(route.TargetPath, cwd) {
				host = route.Hostname
				break
			}
		}
	} else if prefs.SSL[name] && prefs.ProxyPorts[name] == 0 && sameWindowsPath(filepath.Join(prefs.Root, name), cwd) {
		tld := prefs.TLD
		if tld == "" {
			tld = "test"
		}
		host = name + "." + tld
	}
	if host == "" {
		return nil
	}
	return &localtls.Project{Path: cwd, Hostname: host, CertPath: filepath.Join(paths.NginxCerts(), host+".crt"), KeyPath: filepath.Join(paths.NginxCerts(), host+".key")}
}

// A CA may be created when SSL is first enabled, after startup's PHP setup.
// Refresh existing bundles then too; never download/install runtimes here.
func (a *App) refreshLocalPHPTrust() {
	if a.php == nil {
		return
	}
	versions, err := a.php.List()
	if err != nil {
		return
	}
	for _, version := range versions {
		if _, err := os.Stat(filepath.Join(version.Path, "extras", "ssl", "cacert.pem")); err != nil {
			continue
		}
		if err := php.EnsureCaCert(version.Path, vhost.LocalCACertPath(paths.NginxCerts())); err != nil {
			fmt.Printf("[app] local PHP trust (%s): %v\n", version.Version, err)
		}
	}
}
