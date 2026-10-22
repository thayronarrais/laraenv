//go:build windows

package webviewruntime

import (
	"crypto/x509"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wintrust                     = windows.NewLazySystemDLL("wintrust.dll")
	providerDataFromState        = wintrust.NewProc("WTHelperProvDataFromStateData")
	providerSignerFromChain      = wintrust.NewProc("WTHelperGetProvSignerFromChain")
	providerCertificateFromChain = wintrust.NewProc("WTHelperGetProvCertFromChain")
)

// providerCertificatePrefix is the documented leading portion of
// CRYPT_PROVIDER_CERT. Windows owns it until WTD_STATEACTION_CLOSE.
type providerCertificatePrefix struct {
	Size        uint32
	Certificate *windows.CertContext
}

func verifyMicrosoftPublisher(filename string) error {
	for _, procedure := range []*windows.LazyProc{providerDataFromState, providerSignerFromChain, providerCertificateFromChain} {
		if err := procedure.Find(); err != nil {
			return fmt.Errorf("load Authenticode verifier: %w", err)
		}
	}
	path, err := windows.UTF16PtrFromString(filename)
	if err != nil {
		return err
	}
	file := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: path}
	data := windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_WHOLECHAIN,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		ProvFlags:                       windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_REVOCATION_CHECK_CHAIN_EXCLUDE_ROOT | windows.WTD_DISABLE_MD2_MD4,
	}
	defer func() {
		data.StateAction = windows.WTD_STATEACTION_CLOSE
		_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
		runtime.KeepAlive(file)
		runtime.KeepAlive(path)
	}()
	if err := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data); err != nil {
		return fmt.Errorf("Authenticode verification failed: %w", err)
	}
	provider, _, _ := providerDataFromState.Call(uintptr(data.StateData))
	if provider == 0 {
		return errors.New("Authenticode verifier returned no provider state")
	}
	signer, _, _ := providerSignerFromChain.Call(provider, 0, 0, 0)
	if signer == 0 {
		return errors.New("Authenticode verifier returned no validated signer")
	}
	certificate, _, _ := providerCertificateFromChain.Call(signer, 0)
	if certificate == 0 {
		return errors.New("Authenticode verifier returned no signer certificate")
	}
	// Copy the native prefix while the provider state is open. This avoids an
	// unchecked uintptr-to-Go-pointer conversion of the DLL's return value.
	var leaf providerCertificatePrefix
	var copied uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), certificate, (*byte)(unsafe.Pointer(&leaf)), unsafe.Sizeof(leaf), &copied); err != nil {
		return fmt.Errorf("read Authenticode provider certificate: %w", err)
	}
	if copied != unsafe.Sizeof(leaf) || leaf.Size < uint32(unsafe.Sizeof(providerCertificatePrefix{})) || leaf.Certificate == nil || leaf.Certificate.EncodedCert == nil || leaf.Certificate.Length == 0 || leaf.Certificate.Length > 1<<20 {
		return errors.New("Authenticode verifier returned an invalid signer certificate")
	}
	parsed, err := x509.ParseCertificate(unsafe.Slice(leaf.Certificate.EncodedCert, int(leaf.Certificate.Length)))
	if err != nil {
		return fmt.Errorf("read validated signer certificate: %w", err)
	}
	for _, organization := range parsed.Subject.Organization {
		if organization == "Microsoft Corporation" {
			return nil
		}
	}
	return errors.New("validated Authenticode publisher is not Microsoft Corporation")
}

func validateRuntimeCandidate(root, folder, architecture string) error {
	version, valid := parseBrowserVersion(filepath.Base(folder))
	if !valid || compareVersions(version, minimumBrowserVersion) < 0 {
		return errors.New("invalid or unsupported runtime version")
	}
	var machine uint16
	var dllArchitecture string
	switch architecture {
	case "amd64":
		machine, dllArchitecture = pe.IMAGE_FILE_MACHINE_AMD64, "x64"
	case "386":
		machine, dllArchitecture = pe.IMAGE_FILE_MACHINE_I386, "x86"
	case "arm64":
		machine, dllArchitecture = pe.IMAGE_FILE_MACHINE_ARM64, "arm64"
	default:
		return fmt.Errorf("unsupported WebView2 process architecture %q", architecture)
	}
	for _, binary := range []struct {
		path string
		dll  bool
	}{
		{filepath.Join(folder, "msedgewebview2.exe"), false},
		{filepath.Join(folder, "EBWebView", dllArchitecture, "EmbeddedBrowserWebView.dll"), true},
	} {
		if err := containedWithoutReparse(root, binary.path); err != nil {
			return err
		}
		info, err := os.Stat(binary.path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime binary is not a regular file: %q", binary.path)
		}
		if err := validatePE(binary.path, machine, binary.dll); err != nil {
			return err
		}
		fileVersion, err := productVersion(binary.path)
		if err != nil {
			return err
		}
		if fileVersion != version {
			return fmt.Errorf("runtime binary version differs from installation directory: %q", binary.path)
		}
		if err := verifyMicrosoftPublisher(binary.path); err != nil {
			return fmt.Errorf("verify %q: %w", binary.path, err)
		}
	}
	return nil
}

func validatePE(filename string, machine uint16, dll bool) error {
	file, err := pe.Open(filename)
	if err != nil {
		return fmt.Errorf("read runtime PE %q: %w", filename, err)
	}
	defer file.Close()
	if file.Machine != machine || file.OptionalHeader == nil || file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || (file.Characteristics&pe.IMAGE_FILE_DLL != 0) != dll {
		return fmt.Errorf("runtime binary has an unexpected PE architecture or file type: %q", filename)
	}
	return nil
}

func productVersion(filename string) (browserVersion, error) {
	var version browserVersion
	size, err := windows.GetFileVersionInfoSize(filename, nil)
	if err != nil {
		return version, fmt.Errorf("read runtime version size: %w", err)
	}
	if size == 0 || size > 1<<20 {
		return version, errors.New("runtime binary has invalid version resources")
	}
	block := make([]byte, int(size))
	if err := windows.GetFileVersionInfo(filename, 0, size, unsafe.Pointer(&block[0])); err != nil {
		return version, err
	}
	var fixed *windows.VS_FIXEDFILEINFO
	var length uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&block[0]), "\\", unsafe.Pointer(&fixed), &length); err != nil {
		return version, err
	}
	if fixed == nil || length < uint32(unsafe.Sizeof(windows.VS_FIXEDFILEINFO{})) || fixed.Signature != 0xfeef04bd {
		return version, errors.New("runtime binary has invalid fixed version information")
	}
	version = browserVersion{uint16(fixed.ProductVersionMS >> 16), uint16(fixed.ProductVersionMS), uint16(fixed.ProductVersionLS >> 16), uint16(fixed.ProductVersionLS)}
	runtime.KeepAlive(block)
	return version, nil
}
