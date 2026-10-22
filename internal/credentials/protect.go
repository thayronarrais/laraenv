// Package credentials protects local SSH passwords for the current OS user.
// It does not change in-memory or cloud-encrypted SSHHost representations.
package credentials

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const prefix = "dpapi:v1:"

type System struct{}

func (System) Protect(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	protected, err := protect([]byte(password))
	if err != nil {
		return "", fmt.Errorf("protect SSH password for this Windows account: %w", err)
	}
	return prefix + base64.StdEncoding.EncodeToString(protected), nil
}

func (System) Unprotect(value string) (string, error) {
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("unsupported protected SSH password format")
	}
	b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(b) == 0 {
		return "", errors.New("invalid protected SSH password")
	}
	plain, err := unprotect(b)
	if err != nil {
		return "", fmt.Errorf("unlock SSH password for this Windows account: %w", err)
	}
	defer clear(plain)
	return string(plain), nil
}
