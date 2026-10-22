// Package runtimeinventory reads the installed executable, rather than a
// catalog or install-directory label, to identify a managed runtime.
package runtimeinventory

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Metadata struct {
	FullVersion string
	Prerelease  bool
}

type cachedMetadata struct {
	size     int64
	modified time.Time
	metadata Metadata
	err      error
}

type Inventory struct {
	mu           sync.Mutex
	entries      map[string]cachedMetadata
	readMetadata func(string) (Metadata, error)
}

func New() *Inventory {
	return &Inventory{entries: make(map[string]cachedMetadata), readMetadata: readExecutableMetadata}
}

var installed = New()

func Read(executable string) (Metadata, error) { return installed.Read(executable) }

// Read caches both successful and unavailable metadata until the executable
// changes. It never executes the runtime or reads installation markers.
func (inventory *Inventory) Read(executable string) (Metadata, error) {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return Metadata{}, err
	}
	stat, err := os.Stat(executable)
	if err != nil {
		return Metadata{}, err
	}
	if !stat.Mode().IsRegular() {
		return Metadata{}, errors.New("runtime executable is not a regular file")
	}
	inventory.mu.Lock()
	defer inventory.mu.Unlock()
	if cached, ok := inventory.entries[executable]; ok && cached.size == stat.Size() && cached.modified.Equal(stat.ModTime()) {
		return cached.metadata, cached.err
	}
	metadata, err := inventory.readMetadata(executable)
	inventory.entries[executable] = cachedMetadata{size: stat.Size(), modified: stat.ModTime(), metadata: metadata, err: err}
	return metadata, err
}
