//go:build windows

package atomicfile

import (
	"crypto/rand"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var guestDACLProbeParent = flag.String("guest-acl-probe-parent", "", "diagnose synthetic siblings only in the approved graphical Sandbox product directory")

func logGuestProbePermissions(t *testing.T, label string, file *os.File) permissionState {
	t.Helper()
	p, err := readPermissions(file)
	if err != nil {
		t.Fatalf("%s readPermissions: %v", label, err)
	}
	control, revision, err := p.descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	full, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s mode=%#o control=%#04x revision=%d protected=%v DACL=%s full=%s", label, p.mode,
		uint16(control), revision, p.protected, p.representation, full.String())
	if p.acl == nil {
		t.Logf("%s DACL=null", label)
		return p
	}
	t.Logf("%s ACE count=%d", label, p.acl.AceCount)
	for i := uint32(0); i < uint32(p.acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(p.acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(ace)), int(ace.Header.AceSize))
		t.Logf("%s ACE[%d] type=%#x flags=%#x size=%d bytes=%x", label, i,
			ace.Header.AceType, ace.Header.AceFlags, ace.Header.AceSize, data)
	}
	runtime.KeepAlive(p.descriptor)
	return p
}

// This diagnostic is opt-in and guest-only. It reads real ACL metadata but
// never reads config bytes, changes its ACL, or writes any existing file.
func TestGuestDACLProbe(t *testing.T) {
	if *guestDACLProbeParent == "" {
		t.Skip("pass -guest-acl-probe-parent in the approved graphical Sandbox")
	}
	parentPath := filepath.Clean(*guestDACLProbeParent)
	if !strings.EqualFold(parentPath, `C:\LaraEnvComparison\Products\LaraEnv`) ||
		!strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatal("probe requires ExistingLogin WDAGUtilityAccount and the approved Sandbox product path")
	}
	t.Logf("runtime=%s OS=%s ARCH=%s username=%s parent=%s", runtime.Version(), runtime.GOOS,
		runtime.GOARCH, os.Getenv("USERNAME"), parentPath)
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("token user=%s elevated=%v", user.User.Sid.String(), token.IsElevated())
	groups, err := token.GetTokenGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups.AllGroups() {
		t.Logf("token group=%s attributes=%#x", group.Sid.String(), group.Attributes)
	}
	root, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := openDirectory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	logGuestProbePermissions(t, "actual-parent", dir)
	config, err := root.Open("config.json")
	if err != nil {
		t.Fatal(err)
	}
	configPermissions := logGuestProbePermissions(t, "actual-config-metadata-only", config)
	config.Close()
	for _, which := range []string{"inherited-sibling", "exact-config-dacl-sibling"} {
		t.Run(which, func(t *testing.T) {
			leaf := ".laraenv-dacl-probe-" + rand.Text() + ".fixture"
			path := filepath.Join(parentPath, leaf)
			source, err := root.OpenFile(leaf, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.Write([]byte("synthetic ACL diagnostic before")); err != nil {
				source.Close()
				t.Fatal(err)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := root.Remove(leaf); err != nil {
					t.Errorf("remove synthetic source: %v", err)
				}
			}()
			if which == "exact-config-dacl-sibling" {
				// Only the new, exclusively-created fixture receives this ACL.
				setLegacyFixtureDACL(t, path, configPermissions.representation)
			}
			original, err := openTarget(dir, leaf)
			if err != nil {
				t.Fatal(err)
			}
			permissions := logGuestProbePermissions(t, "synthetic-original", original)
			original.Close()
			stage, err := newRootStage(dir, root, ".laraenv-dacl-probe-"+rand.Text()+".tmp", permissions)
			if err != nil {
				t.Fatal(err)
			}
			defer stage.release()
			defer func() {
				if err := stage.cleanup(); err != nil {
					t.Errorf("cleanup empty stage: %v", err)
				}
			}()
			defer stage.Close()
			logGuestProbePermissions(t, "stage-created-empty", stage.retained)
			if err := stage.preserve(permissions); err != nil {
				t.Fatal(err)
			}
			current := logGuestProbePermissions(t, "stage-preserved-empty", stage.retained)
			same, compareErr := SameStagedDACL(permissions.representation, current.representation)
			verifyErr := stage.verify(permissions)
			t.Logf("comparison sameStagedDACL=%v compareErr=%v protectedEqual=%v strictEqual=%v verifyErr=%v",
				same, compareErr, current.protected == permissions.protected, permissions.equal(current), verifyErr)
			if verifyErr != nil {
				t.Errorf("real stage verification failed: %v", verifyErr)
			}
			writeErr := WritePreservingPermissions(path, []byte("synthetic ACL diagnostic after"), 0600)
			t.Logf("WritePreservingPermissions synthetic sibling result=%v", writeErr)
			if writeErr != nil {
				t.Errorf("synthetic sibling save failed: %v", writeErr)
			}
		})
	}
}
