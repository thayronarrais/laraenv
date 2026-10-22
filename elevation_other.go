//go:build !windows || bindings

package main

func ensureGUIElevation([]string) (bool, error) { return false, nil }
