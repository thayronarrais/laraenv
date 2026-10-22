package agents

import "context"

// CachedExecutable reads only the last completed detection. An empty path
// means no cached executable is known; this method never starts provider probes
// or waits for the catalog mutex held by an explicit refresh.
func (c *Catalog) CachedExecutable(ctx context.Context, provider AgentID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	profiles, _ := c.completed.Load().([]AgentProfile)
	for _, profile := range profiles {
		if profile.ID == provider {
			return profile.Executable, ctx.Err()
		}
	}
	return "", ctx.Err()
}

func (m *Manager) CachedActivityExecutable(ctx context.Context, provider AgentID) (string, error) {
	return m.catalog.CachedExecutable(ctx, provider)
}
