//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"lara4ever/internal/atomicfile"
)

type sslFilePermissions struct{}

func (sslFilePermissions) equal(sslFilePermissions) bool { return true }

func snapshotSSLFilePlatform(path string) (sslFileSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return sslFileSnapshot{routeFileSnapshot: routeFileSnapshot{path: path}}, nil
	}
	if err != nil {
		return sslFileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return sslFileSnapshot{}, fmt.Errorf("SSL snapshot requires a regular file: %s", path)
	}
	snapshot, err := snapshotRouteFile(path)
	return sslFileSnapshot{routeFileSnapshot: snapshot}, err
}

func restoreSSLFilePlatform(snapshot sslFileSnapshot) error {
	if !snapshot.exists {
		return restoreRouteFile(snapshot.routeFileSnapshot)
	}
	return atomicfile.Write(snapshot.path, snapshot.data, snapshot.mode)
}
