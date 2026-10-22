package updater

import "fmt"

// portableSwapScript builds the batch file that replaces a portable
// installation's executable once the app itself has exited.
//
// The running executable cannot be overwritten while the process is alive, so
// the caller quits immediately after launching this and the script waits for
// the file locks to clear — the same handoff the installer path uses.
//
// The old executable is moved aside rather than deleted: if the copy fails
// (read-only media, antivirus holding the handle) it is put back, so the user
// is never left with no application. Either way the app is relaunched.
func portableSwapScript(targetExe, newExe, logPath string) string {
	return fmt.Sprintf(`@echo off
echo [%%date%% %%time%%] portable update started >> "%[3]s"
echo target: %[1]s >> "%[3]s"
echo source: %[2]s >> "%[3]s"
echo waiting for LaraEnv.exe to release file locks... >> "%[3]s"
timeout /t 4 /nobreak >nul

if exist "%[1]s.old" del /q "%[1]s.old" >> "%[3]s" 2>&1

move /y "%[1]s" "%[1]s.old" >> "%[3]s" 2>&1
if errorlevel 1 goto fail

copy /y "%[2]s" "%[1]s" >> "%[3]s" 2>&1
if errorlevel 1 goto restore

echo [%%date%% %%time%%] replaced, restarting >> "%[3]s"
del /q "%[1]s.old" >> "%[3]s" 2>&1
start "" "%[1]s"
exit /b 0

:restore
echo [%%date%% %%time%%] copy failed, restoring previous executable >> "%[3]s"
move /y "%[1]s.old" "%[1]s" >> "%[3]s" 2>&1

:fail
echo [%%date%% %%time%%] portable update FAILED >> "%[3]s"
start "" "%[1]s"
exit /b 1
`, targetExe, newExe, logPath)
}
