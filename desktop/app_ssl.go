package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lara4ever/internal/i18n"
	"lara4ever/internal/netports"
	"lara4ever/internal/paths"
	"lara4ever/internal/vhost"
)

// applyProjectSSL commits the preference only if the serving configuration
// applies. A failed change must leave the previous URL and certificates usable.
func (a *App) applyProjectSSL(name string, enabled bool) error {
	// Startup/runtime reconciliation also writes these files. Serialize with
	// it, and with other SSL changes, while snapshots and preferences are live.
	a.projectRuntimeMu.Lock()
	defer a.projectRuntimeMu.Unlock()

	previous := a.cfg.Get().Projects.SSL[name]
	tld := a.projects.TLD()
	if tld == "" {
		tld = "test"
	}
	host := name + "." + tld
	files := map[string]bool{}
	hostnames := []string{host}
	add := func(folder, hostname string) {
		for _, path := range []string{
			a.nginx.VhostPath(folder), a.apache.VhostPath(folder), a.caddy.VhostPath(folder),
			filepath.Join(paths.NginxCerts(), hostname+".crt"), filepath.Join(paths.NginxCerts(), hostname+".key"),
		} {
			files[path] = true
		}
	}
	add(name, host)
	if a.routes != nil {
		for _, route := range a.routes.Stored(name) {
			add(routeFolder(name, route), route.Hostname)
			hostnames = append(hostnames, route.Hostname)
		}
	}
	ordered := make([]string, 0, len(files))
	for path := range files {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	snapshots := make([]sslFileSnapshot, 0, len(ordered))
	for _, path := range ordered {
		snapshot, err := snapshotSSLFile(path)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot)
	}
	hosts, err := a.hosts.Snapshot()
	if err != nil {
		return i18n.Wrapf(err, "errors.routes.snapshotHostsFileFailed", nil)
	}
	servers, err := a.snapshotSSLWebServers()
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		var failures []string
		if err := a.cfg.SetProjectSSL(name, previous); err != nil {
			failures = append(failures, err.Error())
		}
		changed := false
		for _, snapshot := range snapshots {
			current, err := snapshotSSLFile(snapshot.path)
			if err == nil && equalSSLFileSnapshots(current, snapshot) {
				continue
			}
			changed = true
			if err := restoreSSLFile(snapshot); err != nil {
				failures = append(failures, err.Error())
			}
		}
		// Other projects can update hosts while this apply is pending. Restore
		// only this project's managed names; the editor merges under its lock.
		if restored, err := a.hosts.RestoreEntries(hosts, hostnames); err != nil {
			failures = append(failures, err.Error())
		} else if restored {
			changed = true
		}
		for _, server := range servers {
			if changed || a.services.Status(server.name) != "running" {
				// A failed restart may already have stopped the old process.
				// Restore the captured set, not just services still running now.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				var err error
				if a.services.Status(server.name) == "running" {
					err = a.services.RestartContext(ctx, server.name)
				} else {
					err = a.services.StartContext(ctx, server.name)
				}
				if err == nil {
					err = a.waitSSLWebServers(ctx, []sslWebServerSnapshot{server})
				}
				cancel()
				if err != nil {
					failures = append(failures, err.Error())
				}
			}
		}
		if len(failures) > 0 {
			return i18n.Wrapf(cause, "errors.routes.rollbackErrors", map[string]any{"rollbackErrors": strings.Join(failures, "; ")})
		}
		return cause
	}
	if err := a.cfg.SetProjectSSL(name, enabled); err != nil {
		// Store setters also update memory before a failed save; restore that
		// value even when the original config could not be written.
		return rollback(err)
	}
	if err := a.applyVhostFor(name); err != nil {
		return rollback(err)
	}
	if len(servers) > 0 {
		// exec.Cmd.Start only confirms process creation. In particular nginx
		// can retry an occupied TLS port, then exit after Start returned nil.
		expected := make([]sslWebServerSnapshot, len(servers))
		keepHTTPS := false
		prefs := a.cfg.Get().Projects
		for project, ssl := range prefs.SSL {
			keepHTTPS = keepHTTPS || (ssl && !prefs.Paused[project])
		}
		for index, server := range servers {
			expected[index].name = server.name
			for _, port := range server.ports {
				if port != 443 || keepHTTPS {
					expected[index].ports = append(expected[index].ports, port)
				}
			}
			if enabled && !prefs.Paused[name] {
				expected[index].ports = append(expected[index].ports, 443)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := a.waitSSLWebServers(ctx, expected)
		cancel()
		if err != nil {
			return rollback(err)
		}
	}
	if !enabled {
		vhost.RemoveCert(paths.NginxCerts(), host)
	}
	return nil
}

type sslWebServerSnapshot struct {
	name  string
	ports []int
}

func (a *App) snapshotSSLWebServers() ([]sslWebServerSnapshot, error) {
	var servers []sslWebServerSnapshot
	for _, name := range []string{"nginx", "apache", "caddy"} {
		if a.services.Status(name) == "running" {
			servers = append(servers, sslWebServerSnapshot{name: name})
		}
	}
	if len(servers) == 0 {
		return nil, nil
	}
	listeners, err := a.sslWebServerListeners()
	if err != nil {
		return nil, err
	}
	for index, server := range servers {
		for port := range listeners[server.name] {
			servers[index].ports = append(servers[index].ports, port)
		}
		if len(servers[index].ports) == 0 {
			return nil, fmt.Errorf("web server %s has no ready listening ports; retry HTTPS after it starts", server.name)
		}
		sort.Ints(servers[index].ports)
	}
	return servers, nil
}

// Attribute listeners to the managed root and its workers. A different process
// occupying :443 must never be accepted as a successful HTTPS restart.
func (a *App) sslWebServerListeners() (map[string]map[int]bool, error) {
	var pids []int
	names := map[int]string{}
	for _, process := range a.services.TrackedPIDs() {
		switch process.Name {
		case "nginx", "apache", "caddy":
			pids = append(pids, process.PID)
			names[process.PID] = process.Name
		}
	}
	owners := netports.TreeOwners(pids)
	listeners, err := netports.Listeners()
	if err != nil {
		return nil, err
	}
	result := map[string]map[int]bool{}
	for _, listener := range listeners {
		name := names[owners[int(listener.PID)]]
		if name != "" {
			if result[name] == nil {
				result[name] = map[int]bool{}
			}
			result[name][listener.Port] = true
		}
	}
	return result, nil
}

func (a *App) waitSSLWebServers(ctx context.Context, servers []sslWebServerSnapshot) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		listeners, err := a.sslWebServerListeners()
		if err != nil {
			return err
		}
		ready := true
		for _, server := range servers {
			if a.services.Status(server.name) != "running" {
				logs := a.services.Logs(server.name)
				if len(logs) > 8 {
					logs = logs[len(logs)-8:]
				}
				return fmt.Errorf("web server %s stopped before its listening ports were ready: %s", server.name, strings.Join(logs, "\n"))
			}
			ready = ready && len(listeners[server.name]) > 0
			for _, port := range server.ports {
				ready = ready && listeners[server.name][port]
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("web servers did not restore their listening ports: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
