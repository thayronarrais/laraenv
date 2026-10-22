//go:build !windows

package credentials

import "errors"

func protect([]byte) ([]byte, error) {
	return nil, errors.New("SSH password storage requires Windows user protection")
}
func unprotect([]byte) ([]byte, error) {
	return nil, errors.New("SSH password storage requires Windows user protection")
}
