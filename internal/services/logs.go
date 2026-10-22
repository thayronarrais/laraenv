package services

import (
	"bytes"
	"sort"
	"strings"
	"sync"
)

const logRingSize = 500

// logRing is a bounded in-memory tail of a service's stdout/stderr, survived
// across restarts so the UI can show recent output for running (or just-exited)
// services.
type logRing struct {
	mu    sync.Mutex
	lines []string
}

func (r *logRing) push(line string) {
	r.mu.Lock()
	r.lines = append(r.lines, line)
	if len(r.lines) > logRingSize {
		r.lines = r.lines[len(r.lines)-logRingSize:]
	}
	r.mu.Unlock()
}

func (r *logRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// lineWriter splits incoming bytes into lines and pushes each into a logRing.
// Wired as a service process's Stdout/Stderr (via io.MultiWriter) so output is
// retained in a bounded buffer the frontend can read.
type lineWriter struct {
	ring *logRing
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.ring.push(strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// logRingForLocked returns (creating if needed) the ring for a service name.
// Caller must hold m.mu.
func (m *Manager) logRingForLocked(name string) *logRing {
	r := m.logs[name]
	if r == nil {
		r = &logRing{}
		m.logs[name] = r
	}
	return r
}

// Logs returns a snapshot of the captured stdout/stderr lines for a service.
func (m *Manager) Logs(name string) []string {
	m.mu.Lock()
	r := m.logs[name]
	m.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.snapshot()
}

// ServicesWithLogs lists every service that has produced output this session
// (i.e. was started at least once), sorted, regardless of dashboard visibility.
func (m *Manager) ServicesWithLogs() []string {
	m.mu.Lock()
	names := make([]string, 0, len(m.logs))
	for name := range m.logs {
		names = append(names, name)
	}
	m.mu.Unlock()
	sort.Strings(names)
	return names
}
