package vhost

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

const marker = "# lara4ever"

type HostsEditor struct {
	mu   sync.Mutex
	path string
}

func NewHostsEditor() *HostsEditor {
	p := `C:\Windows\System32\drivers\etc\hosts`
	if runtime.GOOS != "windows" {
		p = "/etc/hosts"
	}
	return NewHostsEditorAt(p)
}

// NewHostsEditorAt allows an explicitly supplied hosts file, including isolated
// fixture files. The normal application uses NewHostsEditor's system path.
func NewHostsEditorAt(path string) *HostsEditor { return &HostsEditor{path: path} }

func (h *HostsEditor) AddEntry(ip, host string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.path)
	if err != nil {
		return i18n.Wrapf(err, "errors.vhost.readHostsFailed", nil)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, field := range fields[1:] {
			if strings.EqualFold(field, host) {
				return nil
			}
		}
	}
	line := fmt.Sprintf("%s %s %s\n", ip, host, marker)
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return i18n.Wrapf(err, "errors.vhost.openHostsFailed", nil)
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(line)
	return err
}

func (h *HostsEditor) RemoveEntry(host string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.Contains(ln, marker) {
			fields := strings.Fields(ln)
			remove := false
			for _, field := range fields {
				if strings.EqualFold(field, host) {
					remove = true
					break
				}
			}
			if remove {
				continue
			}
		}
		out = append(out, ln)
	}
	return os.WriteFile(h.path, []byte(strings.Join(out, "\n")), 0644)
}

func (h *HostsEditor) Snapshot() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return os.ReadFile(h.path)
}

func (h *HostsEditor) Restore(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return os.WriteFile(h.path, data, 0644)
}

// RestoreEntries rolls back managed entries for the supplied hostnames while
// preserving current unrelated lines, aliases and unmanaged entries. The merge
// and permission-preserving replacement share the lock used by every writer.
// It skips replacement when these hostnames did not change.
func (h *HostsEditor) RestoreEntries(snapshot []byte, hostnames []string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, err := os.ReadFile(h.path)
	if err != nil {
		return false, err
	}
	targets := make(map[string]bool, len(hostnames))
	for _, host := range hostnames {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			targets[host] = true
		}
	}
	split := func(data []byte) (string, string) {
		var keep, selected strings.Builder
		for _, line := range strings.SplitAfter(string(data), "\n") {
			remainder, owned := splitManagedHostsLine(line, targets)
			keep.WriteString(remainder)
			selected.WriteString(owned)
		}
		return keep.String(), selected.String()
	}
	_, before := split(snapshot)
	keep, after := split(current)
	if before == after {
		return false, nil
	}
	if before != "" && keep != "" && !strings.HasSuffix(keep, "\n") {
		if bytes.Contains(current, []byte("\r\n")) {
			keep += "\r\n"
		} else {
			keep += "\n"
		}
	}
	if err := atomicfile.WritePreservingPermissions(h.path, []byte(keep+before), 0644); err != nil {
		return false, err
	}
	return true, nil
}

var hostsFieldPattern = regexp.MustCompile(`\S+`)

// splitManagedHostsLine returns the untouched remainder and the selected
// managed aliases. A shared line keeps every other alias and its whitespace.
func splitManagedHostsLine(line string, targets map[string]bool) (string, string) {
	content := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	ending := line[len(content):]
	comment := strings.IndexByte(content, '#')
	if comment < 0 {
		return line, ""
	}
	note := strings.TrimSpace(content[comment:])
	if note != marker && !strings.HasPrefix(note, marker+" ") && !strings.HasPrefix(note, marker+"\t") {
		return line, ""
	}
	fields := hostsFieldPattern.FindAllStringIndex(content[:comment], -1)
	if len(fields) < 2 {
		return line, ""
	}
	var selected []string
	var indexes []int
	for index := 1; index < len(fields); index++ {
		field := content[fields[index][0]:fields[index][1]]
		if targets[strings.ToLower(field)] {
			selected = append(selected, field)
			indexes = append(indexes, index)
		}
	}
	if len(selected) == 0 {
		return line, ""
	}
	if len(selected) == len(fields)-1 {
		return "", line
	}
	owned := content[:fields[0][1]] + " " + strings.Join(selected, " ") + " " + content[comment:] + ending
	remainder := content
	for index := len(indexes) - 1; index >= 0; index-- {
		field := indexes[index]
		remainder = remainder[:fields[field-1][1]] + remainder[fields[field][1]:]
	}
	return remainder + ending, owned
}
