//go:build windows

package webviewruntime

import (
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPublisherRejectsUnsignedFile(t *testing.T) {
	// A plausible Microsoft filename and PE header do not establish its publisher.
	file := filepath.Join(t.TempDir(), "msedgewebview2.exe")
	writePE(t, file, pe.IMAGE_FILE_MACHINE_AMD64, false)
	if err := verifyMicrosoftPublisher(file); err == nil {
		t.Fatal("accepted unsigned executable")
	}
}

func TestPublisherAcceptsInstalledMicrosoftRuntime(t *testing.T) {
	_, folder := installedRuntimeFixture(t)
	for _, file := range []string{filepath.Join(folder, "msedgewebview2.exe"), filepath.Join(folder, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")} {
		if err := verifyMicrosoftPublisher(file); err != nil {
			t.Fatalf("Microsoft signature %s: %v", file, err)
		}
	}
}

func TestPublisherRejectsTamperedSignedFile(t *testing.T) {
	_, folder := installedRuntimeFixture(t)
	data, err := os.ReadFile(filepath.Join(folder, "msedgewebview2.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 0x4001 {
		t.Fatal("unexpectedly small runtime fixture")
	}
	data[0x4000] ^= 1 // Modify executable content, outside the Authenticode exclusions.
	file := filepath.Join(t.TempDir(), "msedgewebview2.exe")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyMicrosoftPublisher(file); err == nil {
		t.Fatal("accepted modified Microsoft executable")
	}
}

func TestPublisherRejectsOtherTrustedPublisher(t *testing.T) {
	// A valid signature alone must not authorize another software publisher.
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Skipf("Program Files fixture unavailable: %v", err)
	}
	file := filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe")
	if _, err := os.Stat(file); err != nil {
		t.Skip("optional signed Google Chrome fixture unavailable")
	}
	err = verifyMicrosoftPublisher(file)
	if err == nil {
		t.Fatal("accepted a signed third-party executable as Microsoft")
	}
	if !strings.Contains(err.Error(), "publisher is not Microsoft Corporation") {
		t.Fatalf("expected a trusted but different publisher, got: %v", err)
	}
}

func TestRuntimeCandidateRequiresBothExpectedBinaries(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "155.0.4283.62")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeCandidate(root, folder, runtime.GOARCH); err == nil {
		t.Fatal("accepted an empty runtime directory")
	}
}

func TestRuntimeCandidateAcceptsIntactInstalledBundle(t *testing.T) {
	root, folder := installedRuntimeFixture(t)
	if err := validateRuntimeCandidate(root, folder, runtime.GOARCH); err != nil {
		t.Fatalf("intact installed runtime: %v", err)
	}
}

func TestPEValidationRejectsWrongArchitectureAndFileType(t *testing.T) {
	for _, tc := range []struct {
		name        string
		machine     uint16
		dll         bool
		wantMachine uint16
		wantDLL     bool
		accepted    bool
	}{
		{"x64 executable", pe.IMAGE_FILE_MACHINE_AMD64, false, pe.IMAGE_FILE_MACHINE_AMD64, false, true},
		{"x64 DLL", pe.IMAGE_FILE_MACHINE_AMD64, true, pe.IMAGE_FILE_MACHINE_AMD64, true, true},
		{"x86 in x64 runtime", pe.IMAGE_FILE_MACHINE_I386, true, pe.IMAGE_FILE_MACHINE_AMD64, true, false},
		{"executable named DLL", pe.IMAGE_FILE_MACHINE_AMD64, false, pe.IMAGE_FILE_MACHINE_AMD64, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "binary")
			writePE(t, file, tc.machine, tc.dll)
			err := validatePE(file, tc.wantMachine, tc.wantDLL)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
		})
	}
}

func installedRuntimeFixture(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOARCH != "amd64" {
		t.Skip("live fixture currently exercises the shipped Windows amd64 build")
	}
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFilesX86, windows.FOLDERID_ProgramFiles} {
		programFiles, err := windows.KnownFolderPath(id, windows.KF_FLAG_DONT_VERIFY)
		if err != nil {
			continue
		}
		root := filepath.Join(programFiles, "Microsoft", "EdgeWebView", "Application")
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for i := len(entries) - 1; i >= 0; i-- {
			folder := filepath.Join(root, entries[i].Name())
			if _, err := os.Stat(filepath.Join(folder, "msedgewebview2.exe")); err != nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(folder, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")); err != nil {
				continue
			}
			t.Logf("read-only Microsoft runtime fixture: %s", folder)
			return root, folder
		}
	}
	t.Skip("no installed machine runtime fixture is available")
	return "", ""
}

func writePE(t *testing.T, filename string, machine uint16, dll bool) {
	t.Helper()
	data := make([]byte, 0x80+4+20+240)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[0x3c:], 0x80)
	copy(data[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(data[0x84:], machine)
	binary.LittleEndian.PutUint16(data[0x94:], 240)
	characteristics := uint16(pe.IMAGE_FILE_EXECUTABLE_IMAGE)
	if dll {
		characteristics |= pe.IMAGE_FILE_DLL
	}
	binary.LittleEndian.PutUint16(data[0x96:], characteristics)
	binary.LittleEndian.PutUint16(data[0x98:], 0x20b)
	binary.LittleEndian.PutUint32(data[0x98+108:], 16)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
}
