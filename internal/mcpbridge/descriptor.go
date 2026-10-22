package mcpbridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

type Descriptor struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func (d Descriptor) validate() error {
	u, err := url.Parse(d.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/mcp" || u.RawPath != "" {
		return errors.New("invalid local MCP connection")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		return errors.New("invalid local MCP connection")
	}
	token, err := base64.RawURLEncoding.DecodeString(d.Token)
	if err != nil || len(token) != 32 {
		return errors.New("invalid local MCP credential")
	}
	return nil
}

func ReadDescriptor(path string) (Descriptor, error) {
	var d Descriptor
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return d, errors.New("LaraEnv is not running or its MCP connection is unavailable")
	}
	if err := checkDescriptorPrivacy(path, info); err != nil {
		return d, err
	}
	f, err := os.Open(path)
	if err != nil {
		return d, errors.New("cannot read local MCP connection")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return d, errors.New("invalid local MCP connection")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return Descriptor{}, errors.New("invalid local MCP connection")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Descriptor{}, errors.New("invalid local MCP connection")
	}
	if err := d.validate(); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}

func writeDescriptor(path string, d Descriptor) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create local MCP connection directory")
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("local MCP connection path must be a regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mcp-*.tmp")
	if err != nil {
		return errors.New("cannot create local MCP connection")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	// Restrict access before writing the secret. chmod alone does not install
	// a restrictive DACL on Windows, so the platform helper does that there.
	if err := f.Chmod(0600); err != nil {
		return errors.New("cannot protect local MCP connection")
	}
	if err := protectDescriptor(tmp); err != nil {
		return errors.New("cannot protect local MCP connection")
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.New("cannot write local MCP connection")
	}
	if err := f.Sync(); err != nil {
		return errors.New("cannot write local MCP connection")
	}
	if err := f.Close(); err != nil {
		return errors.New("cannot write local MCP connection")
	}
	if err := replaceDescriptor(tmp, path); err != nil {
		return errors.New("cannot publish local MCP connection")
	}
	return nil
}
