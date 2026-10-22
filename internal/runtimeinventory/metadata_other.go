//go:build !windows

package runtimeinventory

import "errors"

func readExecutableMetadata(string) (Metadata, error) {
	return Metadata{}, errors.New("executable runtime metadata is unavailable on this platform")
}
