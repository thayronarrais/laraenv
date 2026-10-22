package main

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const maxDocumentWorkspaceBytes = 1024 * 1024

var documentRootKey = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (a *App) LoadDocumentWorkspace() string { return string(a.cfg.GetDocumentWorkspace()) }

// SaveDocumentWorkspace persists references only. It never resolves sources:
// unavailable projects/sessions must retain their tabs across restarts.
func (a *App) SaveDocumentWorkspace(raw string) error {
	if len(raw) > maxDocumentWorkspaceBytes {
		return errors.New("document workspace exceeds 1 MiB")
	}
	if err := validateDocumentWorkspace(raw); err != nil {
		return err
	}
	return a.cfg.SetDocumentWorkspace(json.RawMessage(raw))
}

// Read object tokens explicitly: encoding/json's struct decoder accepts
// duplicate and case-insensitive keys, neither of which is the exact schema.
func documentObject(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected document object")
	}
	fields := make(map[string]json.RawMessage, len(keys))
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || fields[key] != nil {
			return nil, errors.New("unknown or duplicate document field")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		if string(value) == "null" {
			return nil, errors.New("null document field")
		}
		fields[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing document JSON")
	}
	if len(fields) != len(keys) {
		return nil, errors.New("missing document field")
	}
	return fields, nil
}

func documentString(raw json.RawMessage) (string, error) {
	var value string
	err := json.Unmarshal(raw, &value)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", errors.New("invalid document string")
	}
	return value, nil
}

func validateDocumentWorkspace(raw string) error {
	root, err := documentObject(json.RawMessage(raw), "version", "tabs", "activeTabIndex", "visible", "panelPercent")
	if err != nil {
		return err
	}
	var version, active int
	var visible bool
	var percent float64
	var tabs []json.RawMessage
	if json.Unmarshal(root["version"], &version) != nil || version != 1 || json.Unmarshal(root["activeTabIndex"], &active) != nil || json.Unmarshal(root["visible"], &visible) != nil || json.Unmarshal(root["panelPercent"], &percent) != nil || percent < 20 || percent > 80 || json.Unmarshal(root["tabs"], &tabs) != nil {
		return errors.New("invalid document workspace layout")
	}
	if (len(tabs) == 0 && active != -1) || (len(tabs) > 0 && (active < 0 || active >= len(tabs))) {
		return errors.New("invalid active document tab")
	}
	for _, rawTab := range tabs {
		tab, err := documentObject(rawTab, "references", "mode")
		if err != nil {
			return err
		}
		mode, err := documentString(tab["mode"])
		if err != nil || (mode != "source" && mode != "preview") {
			return errors.New("invalid document mode")
		}
		var refs []json.RawMessage
		if json.Unmarshal(tab["references"], &refs) != nil || len(refs) == 0 {
			return errors.New("document tab requires references")
		}
		for _, rawRef := range refs {
			if err := validateDocumentReference(rawRef); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDocumentReference(raw json.RawMessage) error {
	ref, err := documentObject(raw, "context", "rootKey", "relativePath")
	if err != nil {
		return err
	}
	key, err := documentString(ref["rootKey"])
	if err != nil || !documentRootKey.MatchString(key) {
		return errors.New("invalid document root key")
	}
	path, err := documentString(ref["relativePath"])
	if err != nil || strings.ContainsAny(path, "\\:\x00") || strings.HasPrefix(path, "/") {
		return errors.New("invalid document relative path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid document relative path")
		}
	}
	// Inspect kind only to choose the exact discriminated-union fields below.
	var kind struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(ref["context"], &kind) != nil {
		return errors.New("invalid document context")
	}
	field := "projectName"
	if kind.Kind == "agent" {
		field = "sessionId"
	} else if kind.Kind != "project" {
		return errors.New("invalid document context kind")
	}
	context, err := documentObject(ref["context"], "kind", field)
	if err != nil {
		return err
	}
	_, err = documentString(context[field])
	return err
}
