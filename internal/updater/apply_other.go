//go:build !windows

package updater

import "lara4ever/internal/i18n"

func applySwap(targetExe, newExe, logPath string) error {
	return i18n.Errorf("errors.updater.portableUpdateWindowsOnly", nil)
}

func apply(msiPath string) error {
	return i18n.Errorf("errors.updater.applyWindowsOnly", nil)
}
