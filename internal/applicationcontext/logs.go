// Package applicationcontext reads bounded local diagnostic evidence. It never
// boots a Laravel application, executes its code or installs dependencies.
package applicationcontext

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const evidenceLimit = 256 << 10

type LogEntry struct {
	Time        string `json:"time,omitempty"`
	Environment string `json:"environment,omitempty"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	Stack       string `json:"stack,omitempty"`
	Source      string `json:"source"`
}
type Logs struct {
	Status    string     `json:"status"`
	Message   string     `json:"message,omitempty"`
	Lines     []string   `json:"lines"`
	Entries   []LogEntry `json:"entries"`
	Errors    []LogEntry `json:"errors"`
	Sources   []string   `json:"sources"`
	Truncated bool       `json:"truncated"`
}

var logHeader = regexp.MustCompile(`^\[([^\]]{1,64})\] ([A-Za-z0-9_-]{1,64})\.([A-Z]{3,12}): ?(.*)$`)
var dailyLog = regexp.MustCompile(`^laravel-(\d{4}-\d{2}-\d{2})\.log$`)
var secretAssignment = regexp.MustCompile(`(?i)(["']?(?:password|passwd|token|secret|authorization|api[_-]?key|cookie)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;}]+)`)
var bearer = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)

func boundedText(s string, maximum int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) > maximum {
		s = strings.ToValidUTF8(s[:maximum], "") + " [truncated]"
	}
	return s
}
func Redact(s string) string {
	s = secretAssignment.ReplaceAllString(s, "${1}[redacted]")
	return bearer.ReplaceAllString(s, "Bearer [redacted]")
}
func readTail(ctx context.Context, root *os.Root, name string, maximum int64) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !st.Mode().IsRegular() {
		return nil, false, errors.New("evidence is not a regular file")
	}
	truncated := st.Size() > maximum
	if truncated {
		if _, err = f.Seek(-maximum, io.SeekEnd); err != nil {
			return nil, false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum))
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if truncated {
		if first := strings.IndexByte(string(data), '\n'); first >= 0 {
			data = data[first+1:]
		}
	}
	return data, truncated, nil
}

func ReadLogs(ctx context.Context, path string, count int) (Logs, error) {
	out := Logs{Status: "unavailable", Lines: []string{}, Entries: []LogEntry{}, Errors: []LogEntry{}, Sources: []string{}}
	if count < 0 || count > 500 {
		return out, errors.New("lines must be between 1 and 500")
	}
	if count == 0 {
		count = 100
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return out, err
	}
	defer root.Close()
	dir, err := root.Open("storage/logs")
	if os.IsNotExist(err) {
		out.Message = "No Laravel log files found"
		return out, nil
	}
	if err != nil {
		return out, errors.New("Laravel log directory is unavailable or outside the project")
	}
	names, readErr := dir.Readdirnames(4096)
	dir.Close()
	if readErr != nil && readErr != io.EOF {
		return out, readErr
	}
	if len(names) == 4096 {
		out.Truncated = true
	}
	selected := []string{}
	for _, name := range names {
		if name == "laravel.log" {
			selected = append(selected, name)
			continue
		}
		if match := dailyLog.FindStringSubmatch(name); match != nil {
			if _, err := time.Parse("2006-01-02", match[1]); err == nil {
				selected = append(selected, name)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(selected)))
	if len(selected) > 8 {
		selected = selected[:8]
		out.Truncated = true
	}
	type logLine struct{ time, text string }
	mergedLines := []logLine{}
	for _, name := range selected {
		data, cut, err := readTail(ctx, root, "storage/logs/"+name, evidenceLimit/4)
		if err != nil {
			return out, errors.New("Laravel log file is unavailable or outside the project")
		}
		out.Truncated = out.Truncated || cut
		out.Sources = append(out.Sources, name)
		var entry *LogEntry
		stamp := ""
		if match := dailyLog.FindStringSubmatch(name); match != nil {
			stamp = match[1] + " 00:00:00"
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			line = boundedText(Redact(strings.TrimSuffix(line, "\r")), 8192)
			if line == "" {
				continue
			}
			if match := logHeader.FindStringSubmatch(line); match != nil {
				stamp = match[1]
				out.Entries = append(out.Entries, LogEntry{Time: match[1], Environment: match[2], Level: match[3], Message: match[4], Source: name})
				entry = &out.Entries[len(out.Entries)-1]
			} else if entry != nil {
				entry.Stack = boundedText(entry.Stack+"\n"+line, 8192)
			}
			mergedLines = append(mergedLines, logLine{time: stamp, text: line})
		}
	}
	if len(selected) == 0 {
		out.Message = "No Laravel single or daily log files found"
		return out, nil
	}
	out.Status = "available"
	sort.SliceStable(mergedLines, func(i, j int) bool { return mergedLines[i].time < mergedLines[j].time })
	for _, line := range mergedLines {
		out.Lines = append(out.Lines, line.text)
	}
	sort.SliceStable(out.Entries, func(i, j int) bool { return out.Entries[i].Time < out.Entries[j].Time })
	if len(out.Entries) > count {
		out.Entries = out.Entries[len(out.Entries)-count:]
		out.Truncated = true
	}
	budget := evidenceLimit / 2
	for i := len(out.Entries) - 1; i >= 0; i-- {
		size := len(out.Entries[i].Message) + len(out.Entries[i].Stack)
		if size > budget {
			out.Entries = out.Entries[i+1:]
			out.Truncated = true
			break
		}
		budget -= size
	}
	for _, entry := range out.Entries {
		switch entry.Level {
		case "ERROR", "CRITICAL", "ALERT", "EMERGENCY":
			out.Errors = append(out.Errors, entry)
		}
	}
	if len(out.Lines) > count {
		out.Lines = out.Lines[len(out.Lines)-count:]
		out.Truncated = true
	}
	budget = evidenceLimit / 2
	for i := len(out.Lines) - 1; i >= 0; i-- {
		budget -= len(out.Lines[i])
		if budget < 0 {
			out.Lines = out.Lines[i+1:]
			out.Truncated = true
			break
		}
	}
	return out, nil
}
