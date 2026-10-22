package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

type passwordProtector interface {
	Protect(string) (string, error)
	Unprotect(string) (string, error)
}

// Disk-only envelopes avoid a custom SSHHost marshaler: UI and E2EE sync
// still need Password in memory, while only config.json uses DPAPI blobs.
type diskSSHHost struct {
	SSHHost
	Password            string  `json:"password,omitempty"`
	PasswordUnavailable bool    `json:"passwordUnavailable,omitempty"`
	PasswordProtected   *string `json:"passwordProtected,omitempty"`
}
type diskProjects struct {
	Projects
	SSHHosts map[string]diskSSHHost `json:"sshHosts"`
}
type diskConfig struct {
	Config
	Projects diskProjects `json:"projects"`
}

func encodeDiskConfig(cfg Config, locked map[string]string, protector passwordProtector) ([]byte, error) {
	hosts := make(map[string]diskSSHHost, len(cfg.Projects.SSHHosts))
	for name, host := range cfg.Projects.SSHHosts {
		disk := diskSSHHost{SSHHost: host}
		if host.Password != "" {
			protected, err := protector.Protect(host.Password)
			if err != nil {
				return nil, fmt.Errorf("save SSH password for host %q: %w", name, err)
			}
			if protected == "" {
				return nil, errors.New("SSH password protection returned an empty value")
			}
			disk.PasswordProtected = &protected
		} else if protected, ok := locked[name]; ok {
			// Unavailable on this account: preserve the exact blob until a
			// replacement password is successfully protected and persisted.
			disk.PasswordProtected = &protected
		}
		hosts[name] = disk
	}
	return json.MarshalIndent(diskConfig{Config: cfg, Projects: diskProjects{Projects: cfg.Projects, SSHHosts: hosts}}, "", "  ")
}

func decodeDiskConfig(raw []byte, protector passwordProtector) (Config, map[string]string, bool, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, nil, false, err
	}
	var disk struct {
		Projects struct {
			SSHHosts map[string]struct {
				PasswordProtected json.RawMessage `json:"passwordProtected"`
			} `json:"sshHosts"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return Config{}, nil, false, err
	}
	locked := make(map[string]string)
	legacy := false
	for name, host := range cfg.Projects.SSHHosts {
		protected := disk.Projects.SSHHosts[name].PasswordProtected
		host.PasswordUnavailable = false // disk/UI metadata is never authoritative
		if len(protected) != 0 {
			var value string
			if string(protected) == "null" || json.Unmarshal(protected, &value) != nil {
				return Config{}, nil, false, errors.New("invalid protected SSH password field")
			}
			if host.Password != "" {
				legacy = true
			}
			password, err := protector.Unprotect(value)
			host.Password = password
			if err != nil {
				host.Password, host.PasswordUnavailable = "", true
				locked[name] = value
			}
		} else if host.Password != "" {
			legacy = true
		}
		cfg.Projects.SSHHosts[name] = host
	}
	return cfg, locked, legacy, nil
}
