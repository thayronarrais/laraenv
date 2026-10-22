//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"lara4ever/internal/i18n"
)

// apply hands the upgrade off to msiexec. The trick on Windows is that the
// running LaraEnv.exe holds a lock on its own binary, so we can't have
// msiexec touch Program Files until *we* are gone. Approach:
//
//  1. Write a tiny launcher .cmd next to the cached MSI.
//  2. Spawn it detached (CREATE_NO_WINDOW + CREATE_NEW_PROCESS_GROUP) so it
//     keeps running after the parent exits.
//  3. The .cmd waits 4 seconds, then runs `msiexec /i ... /passive /norestart`.
//  4. Caller (app.go's InstallUpdate) calls wruntime.Quit a moment later.
//
// /passive: shows a non-interactive progress bar — without it (/qn) the user
// sees nothing at all. /norestart: prevents an unwanted reboot prompt.
//
// The LaunchApp custom action in laraenv.wxs re-spawns LaraEnv.exe once the
// MSI finishes, so the user lands back in the app.
//
// All output (stdout+stderr from msiexec) is appended to update.log next to
// the MSI so failures can be inspected after the fact.
// applySwap replaces a portable installation's executable. The script does
// the work because the running process holds a lock on the file it needs to
// overwrite; the caller quits right after this returns.
func applySwap(targetExe, newExe, logPath string) error {
	batPath := filepath.Join(filepath.Dir(newExe), "run-portable-update.cmd")
	if err := os.WriteFile(batPath, []byte(portableSwapScript(targetExe, newExe, logPath)), 0644); err != nil {
		return i18n.Wrapf(err, "errors.updater.writePortableScriptFailed", nil)
	}
	return startDetached(batPath)
}

func apply(msiPath string) error {
	dir := filepath.Dir(msiPath)
	batPath := filepath.Join(dir, "run-update.cmd")
	logPath := filepath.Join(dir, "update.log")

	script := fmt.Sprintf(`@echo off
echo [%%date%% %%time%%] update launcher started >> "%s"
echo waiting for LaraEnv.exe to release file locks... >> "%s"
timeout /t 4 /nobreak >nul
echo [%%date%% %%time%%] running msiexec /i "%s" /passive /norestart >> "%s"
msiexec.exe /i "%s" /passive /norestart >> "%s" 2>&1
echo [%%date%% %%time%%] msiexec exit code: %%errorlevel%% >> "%s"
`, logPath, logPath, msiPath, logPath, msiPath, logPath, logPath)

	if err := os.WriteFile(batPath, []byte(script), 0644); err != nil {
		return i18n.Wrapf(err, "errors.updater.writeUpdateScriptFailed", nil)
	}

	return startDetached(batPath)
}

// startDetached runs a .cmd that must outlive this process.
func startDetached(batPath string) error {
	cmd := exec.Command("cmd.exe", "/C", batPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NO_WINDOW (0x08000000) hides the cmd window but still gives
		// it a console, unlike DETACHED_PROCESS which can break console apps.
		// CREATE_NEW_PROCESS_GROUP (0x00000200) keeps the child alive after
		// the parent exits.
		CreationFlags: 0x08000000 | 0x00000200,
	}
	if err := cmd.Start(); err != nil {
		return i18n.Wrapf(err, "errors.updater.launchUpdateScriptFailed", nil)
	}
	// Detach: don't Wait. Parent app will Quit shortly.
	return nil
}
