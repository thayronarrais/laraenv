package applicationcontext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

type CaptureEvent struct {
	Version int            `json:"v"`
	Project string         `json:"project"`
	Kind    string         `json:"kind"`
	Time    string         `json:"time"`
	TraceID string         `json:"traceId,omitempty"`
	Data    map[string]any `json:"data"`
}
type Capture struct {
	Status      string             `json:"status"`
	Message     string             `json:"message,omitempty"`
	Events      []CaptureEvent     `json:"events"`
	Truncated   bool               `json:"truncated"`
	Traces      []CaptureTrace     `json:"traces"`
	Integration CaptureIntegration `json:"integration"`
}

type CaptureIntegration struct {
	ProviderPresent      bool   `json:"providerPresent"`
	ConfigurationPresent bool   `json:"configurationPresent"`
	Activation           string `json:"activation"`
}
type CaptureTrace struct {
	ID              string         `json:"id"`
	Events          int            `json:"events"`
	Queries         int            `json:"queries"`
	RepeatedQueries int            `json:"repeatedQueries"`
	Fingerprints    map[string]int `json:"fingerprints,omitempty"`
}

var captureFile = regexp.MustCompile(`^context-\d{4}-\d{2}-\d{2}\.jsonl$`)
var captureTraceID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var captureFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)
var captureFields = map[string]map[string]bool{
	"request": {"method": true, "route": true, "status": true, "durationMs": true},
	"query":   {"operation": true, "durationMs": true, "connection": true, "fingerprint": true},
	"job":     {"class": true, "state": true},
	"view":    {"name": true},
	"log":     {"level": true},
	"dump":    {"type": true, "count": true},
}

func ReadCapture(ctx context.Context, path, project string, count int) (Capture, error) {
	out := Capture{Status: "unavailable", Message: "No observed capture. Review integration, register its provider and explicitly enable local capture; no application code is executed by this reader.", Events: []CaptureEvent{}, Traces: []CaptureTrace{}, Integration: CaptureIntegration{Activation: "unknown; requires local environment flag and registered provider"}}
	if count < 0 || count > 500 {
		return out, errors.New("capture limit must be between 1 and 500")
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
	if st, err := root.Stat("app/Providers/LaraEnvContextServiceProvider.php"); err == nil && st.Mode().IsRegular() {
		out.Integration.ProviderPresent = true
	}
	if st, err := root.Stat("config/laraenv-context.php"); err == nil && st.Mode().IsRegular() {
		out.Integration.ConfigurationPresent = true
	}
	dir, err := root.Open("storage/laraenv")
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, errors.New("capture directory unavailable or outside project")
	}
	names, err := dir.Readdirnames(1024)
	dir.Close()
	if err != nil && err != io.EOF {
		return out, err
	}
	selected := []string{}
	for _, name := range names {
		if captureFile.MatchString(name) {
			selected = append(selected, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(selected)))
	if len(selected) > 7 {
		selected = selected[:7]
		out.Truncated = true
	}
	for _, name := range selected {
		data, cut, err := readTail(ctx, root, "storage/laraenv/"+name, evidenceLimit/2)
		if err != nil {
			return out, errors.New("capture file unavailable or outside project")
		}
		out.Truncated = out.Truncated || cut
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var event CaptureEvent
			if len(line) > 8192 || json.Unmarshal([]byte(line), &event) != nil || event.Version != 1 || event.Project != project || captureFields[event.Kind] == nil {
				out.Truncated = true
				continue
			}
			filtered := map[string]any{}
			if !captureTraceID.MatchString(event.TraceID) {
				event.TraceID = ""
			}
			for key, value := range event.Data {
				if !captureFields[event.Kind][key] {
					continue
				}
				switch v := value.(type) {
				case string:
					if key == "fingerprint" && !captureFingerprint.MatchString(v) {
						continue
					}
					filtered[key] = boundedText(Redact(v), 256)
				case float64:
					if v >= 0 && v < 1e12 {
						filtered[key] = v
					}
				}
			}
			event.Data = filtered
			event.Time = boundedText(event.Time, 64)
			out.Events = append(out.Events, event)
		}
	}
	if len(out.Events) == 0 {
		return out, nil
	}
	sort.SliceStable(out.Events, func(i, j int) bool { return out.Events[i].Time < out.Events[j].Time })
	if len(out.Events) > count {
		out.Events = out.Events[len(out.Events)-count:]
		out.Truncated = true
	}
	groups := map[string]int{}
	for _, event := range out.Events {
		if event.TraceID == "" {
			continue
		}
		index, ok := groups[event.TraceID]
		if !ok {
			index = len(out.Traces)
			groups[event.TraceID] = index
			out.Traces = append(out.Traces, CaptureTrace{ID: event.TraceID, Fingerprints: map[string]int{}})
		}
		trace := &out.Traces[index]
		trace.Events++
		if event.Kind == "query" {
			trace.Queries++
			if fingerprint, ok := event.Data["fingerprint"].(string); ok {
				if trace.Fingerprints[fingerprint] > 0 {
					trace.RepeatedQueries++
				}
				trace.Fingerprints[fingerprint]++
			}
		}
	}
	out.Status = "available"
	out.Message = "Observed historical metadata only; presence does not prove capture is currently enabled. SQL text/bindings, log messages and dump values are not collected."
	if out.Truncated {
		out.Status = "partial"
	}
	return out, nil
}
