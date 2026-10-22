package projectroutes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

const routeStoreVersion = 1

type routeFile struct {
	Version int     `json:"version"`
	Routes  []Route `json:"routes"`
}

type StoreNotice struct {
	Message       string `json:"message"`
	PreservedPath string `json:"preservedPath,omitempty"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	routes map[string]Route
	notice *StoreNotice
}

func NewStore(path string) (*Store, error) {
	store := &Store{path: path, routes: make(map[string]Route)}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var file routeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return recoverCorruptStore(store, i18n.Wrapf(err, "errors.projectroutes.decodeRoutesFailed", nil))
	}
	if file.Version > routeStoreVersion {
		return recoverCorruptStore(store, i18n.Errorf("errors.projectroutes.unsupportedRouteSchema", map[string]any{"version": file.Version}))
	}
	migrated := false
	for _, route := range file.Routes {
		if route.Canonical && route.ID == "canonical" {
			route.ID = canonicalRouteID(route.ProjectName)
			migrated = true
		}
		if route.ID != "" {
			store.routes[route.ID] = route
		}
	}
	if migrated {
		if err := store.saveLocked(); err != nil {
			return nil, i18n.Wrapf(err, "errors.projectroutes.migrateRoutesFailed", nil)
		}
	}
	return store, nil
}

func recoverCorruptStore(store *Store, cause error) (*Store, error) {
	preserved := store.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.Rename(store.path, preserved); err != nil {
		return nil, i18n.Wrapf(cause, "errors.projectroutes.preserveCorruptFailed", map[string]any{"renameError": err.Error()})
	}
	// Safe to call i18n.T here: recoverCorruptStore only runs from NewStore,
	// which app.go's NewApp calls — and main.go now loads catalogues into
	// i18n.Global() before NewApp runs at all (LoadCatalogues's doc comment
	// in app.go). Confirmed by reading that ordering, not assumed.
	store.notice = &StoreNotice{Message: i18n.T("backend.projectroutes.routeStateMalformed", nil), PreservedPath: preserved}
	return store, nil
}

func (s *Store) Notice() *StoreNotice {
	if s.notice == nil {
		return nil
	}
	copy := *s.notice
	return &copy
}

func (s *Store) List(projectName string) []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Route, 0)
	for _, route := range s.routes {
		if strings.EqualFold(route.ProjectName, projectName) {
			result = append(result, route)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Canonical != result[j].Canonical {
			return result[i].Canonical
		}
		return result[i].Hostname < result[j].Hostname
	})
	return result
}

func (s *Store) All() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Route, 0, len(s.routes))
	for _, route := range s.routes {
		result = append(result, route)
	}
	return result
}

func (s *Store) ReplaceProject(projectName string, routes []Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := make(map[string]Route, len(s.routes))
	for id, route := range s.routes {
		previous[id] = route
		if strings.EqualFold(route.ProjectName, projectName) {
			delete(s.routes, id)
		}
	}
	for _, route := range routes {
		if route.ID == "" {
			s.routes = previous
			return i18n.Errorf("errors.projectroutes.routeIdRequired", nil)
		}
		s.routes[route.ID] = route
	}
	if err := s.saveLocked(); err != nil {
		s.routes = previous
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	routes := make([]Route, 0, len(s.routes))
	for _, route := range s.routes {
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	data, err := json.MarshalIndent(routeFile{Version: routeStoreVersion, Routes: routes}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, append(data, '\n'), 0644)
}
