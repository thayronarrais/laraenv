package main

import "bytes"

// SSL rollback includes private keys. Keep the permissions from the original
// file, rather than the permissions of a replacement left by a failed apply.
type sslFileSnapshot struct {
	routeFileSnapshot
	permissions sslFilePermissions
}

func snapshotSSLFile(path string) (sslFileSnapshot, error) {
	return snapshotSSLFilePlatform(path)
}

func restoreSSLFile(snapshot sslFileSnapshot) error {
	return restoreSSLFilePlatform(snapshot)
}

func equalSSLFileSnapshots(a, b sslFileSnapshot) bool {
	return a.exists == b.exists && a.mode == b.mode && bytes.Equal(a.data, b.data) && a.permissions.equal(b.permissions)
}
