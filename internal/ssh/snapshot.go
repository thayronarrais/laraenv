package ssh

import (
	"errors"
	"fmt"
	"strings"

	"lara4ever/internal/config"
)

// Snapshot resolves the target and its complete ProxyJump chain synchronously.
// The returned manager has an independent frozen lookup, and the target-first
// hosts are independent copies suitable for an execution's credential redactor.
// No connection is opened and private keys/known_hosts are never changed.
func (m *Manager) Snapshot(host config.SSHHost) (*Manager, []config.SSHHost, error) {
	if m == nil {
		return nil, nil, errors.New("SSH manager unavailable")
	}
	frozen := *m
	lookup := m.hostLookup
	captured := map[string]config.SSHHost{}
	seen := map[string]bool{}
	if host.Name != "" {
		seen[host.Name] = true
	}
	snapshots := []config.SSHHost{}
	current := snapshotHostClone(host)
	for {
		if current.PasswordUnavailable && !((current.AuthMode == "key" || current.AuthMode == "both") && current.KeyName != "") {
			return nil, nil, errors.New("saved SSH password is unavailable for this Windows account")
		}
		snapshots = append(snapshots, snapshotHostClone(current))
		captured[current.Name] = snapshotHostClone(current)
		// Match proxyDial exactly: ProxyCommand takes precedence, and jump names
		// are trimmed before being passed to the configured saved-host lookup.
		if strings.TrimSpace(current.ProxyCommand) != "" {
			break
		}
		jump := strings.TrimSpace(current.ProxyJump)
		if jump == "" {
			break
		}
		if seen[jump] {
			return nil, nil, fmt.Errorf("SSH ProxyJump cycle at %q", jump)
		}
		seen[jump] = true
		if lookup == nil {
			return nil, nil, errors.New("SSH ProxyJump saved-host lookup unavailable")
		}
		next, ok := lookup(jump)
		if !ok {
			return nil, nil, fmt.Errorf("SSH ProxyJump host %q not found", jump)
		}
		if next.Name != "" && next.Name != jump {
			if seen[next.Name] {
				return nil, nil, fmt.Errorf("SSH ProxyJump cycle at %q", next.Name)
			}
			seen[next.Name] = true
		}
		// Preserve the resolver's exact key as well as the resolved host's Name.
		captured[jump] = snapshotHostClone(next)
		current = snapshotHostClone(next)
	}
	frozen.hostLookup = func(name string) (config.SSHHost, bool) {
		h, ok := captured[name]
		if !ok {
			return config.SSHHost{}, false
		}
		return snapshotHostClone(h), true
	}
	return &frozen, snapshots, nil
}
func snapshotHostClone(host config.SSHHost) config.SSHHost {
	host.Tags = append([]string(nil), host.Tags...)
	return host
}
