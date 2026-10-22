package agents

import (
	"context"
	"sort"
	"sync"
	"time"

	"lara4ever/internal/i18n"
)

type UsageManager struct {
	catalog   *Catalog
	mu        sync.RWMutex
	cache     []UsageSnapshot
	refreshMu sync.Mutex
	claude    *claudeUsageReader
}

func NewUsageManager(catalog *Catalog) *UsageManager {
	return &UsageManager{catalog: catalog, claude: newClaudeUsageReader()}
}

func (m *UsageManager) List(ctx context.Context) []UsageSnapshot {
	m.mu.RLock()
	cache := append([]UsageSnapshot(nil), m.cache...)
	m.mu.RUnlock()
	if len(cache) > 0 {
		return cache
	}
	return m.Refresh(ctx)
}

func (m *UsageManager) Refresh(ctx context.Context) []UsageSnapshot {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	profiles := m.catalog.Profiles(ctx)
	now := time.Now().UTC()
	result := make([]UsageSnapshot, 0, len(profiles))
	for _, profile := range profiles {
		snapshot := UsageSnapshot{Provider: profile.ID, ReadAt: now, Status: "unsupported", Windows: []UsageWindow{}}
		if !profile.Available {
			snapshot.Status = "unavailable"
			snapshot.Message = profile.Reason
		} else if profile.ID == AgentClaude {
			snapshot = m.claude.Read(ctx)
		} else if profile.ID == AgentCodex {
			readerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			var err error
			snapshot, err = ReadCodexUsage(readerCtx, profile.Executable)
			cancel()
			if err != nil {
				snapshot = UsageSnapshot{Provider: AgentCodex, Status: "error", Message: err.Error(), ReadAt: now}
			}
		} else {
			snapshot.Message = unsupportedUsageMessage(profile.ID)
		}
		if snapshot.Windows == nil {
			snapshot.Windows = []UsageWindow{}
		}
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Provider < result[j].Provider })
	m.mu.Lock()
	m.cache = result
	m.mu.Unlock()
	return append([]UsageSnapshot(nil), result...)
}

func unsupportedUsageMessage(provider AgentID) string {
	switch provider {
	case AgentKimi:
		return i18n.T("backend.agents.kimiUsageUnsupported", nil)
	case AgentOpenCode:
		return i18n.T("backend.agents.openCodeUsageUnsupported", nil)
	default:
		return i18n.T("backend.agents.usageNotSupportedFor", map[string]any{"provider": provider})
	}
}
