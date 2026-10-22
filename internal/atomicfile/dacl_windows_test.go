//go:build windows

package atomicfile

import "testing"

func TestSameStagedDACLAllowsOnlyAutoInheritedControl(t *testing.T) {
	const original = "D:(A;;FA;;;BA)(A;;FR;;;WD)"
	for _, tc := range []struct {
		name, expected, staged string
		want                   bool
	}{
		{"same", original, original, true},
		{"auto-inherited-only", original, "D:AI(A;;FA;;;BA)(A;;FR;;;WD)", true},
		{"incidental-owner-group", "O:BAG:SYD:(A;;FA;;;BA)(A;;FR;;;WD)", "D:AI(A;;FA;;;BA)(A;;FR;;;WD)", true},
		{"incidental-owner-group-with-ace-change", "O:BAG:SYD:(A;;FA;;;BA)(A;;FR;;;WD)", "D:AI(A;;FA;;;BA)(A;;FW;;;WD)", false},
		{"protected-auto-inherited-only", "D:P(A;;FA;;;BA)", "D:PAI(A;;FA;;;BA)", true},
		{"protection", original, "D:P(A;;FA;;;BA)(A;;FR;;;WD)", false},
		{"auto-inherit-request", original, "D:AR(A;;FA;;;BA)(A;;FR;;;WD)", false},
		{"mask", original, "D:(A;;FA;;;BA)(A;;FW;;;WD)", false},
		{"sid", original, "D:(A;;FA;;;BA)(A;;FR;;;BU)", false},
		{"type", original, "D:(A;;FA;;;BA)(D;;FR;;;WD)", false},
		{"order", original, "D:(A;;FR;;;WD)(A;;FA;;;BA)", false},
		{"inherited-flag", original, "D:(A;;FA;;;BA)(A;ID;FR;;;WD)", false},
		{"object-inherit-flag", original, "D:(A;;FA;;;BA)(A;OI;FR;;;WD)", false},
		{"container-inherit-flag", original, "D:(A;;FA;;;BA)(A;CI;FR;;;WD)", false},
		{"inherit-only-flag", original, "D:(A;;FA;;;BA)(A;IO;FR;;;WD)", false},
		{"no-propagate-flag", original, "D:(A;;FA;;;BA)(A;NP;FR;;;WD)", false},
		{"extra-ace", original, "D:(A;;FA;;;BA)(A;;FR;;;WD)(A;ID;FR;;;BU)", false},
		{"null-versus-empty", "D:NO_ACCESS_CONTROL", "D:", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SameStagedDACL(tc.expected, tc.staged)
			if err != nil || got != tc.want {
				t.Fatalf("staged DACL compatibility = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	// The live source guard must still detect even this metadata change.
	before := permissionState{representation: original}
	after := permissionState{representation: "D:AI(A;;FA;;;BA)(A;;FR;;;WD)"}
	if before.equal(after) {
		t.Fatal("original-versus-current DACL guard became non-strict")
	}
}

func TestSameStagedDACLRejectsAbsentOrInvalidDACL(t *testing.T) {
	for _, sddl := range []string{"", "invalid", "O:SY"} {
		if same, err := SameStagedDACL("D:P(A;;FA;;;SY)", sddl); same || err == nil {
			t.Fatalf("accepted absent or invalid DACL %q: %v, %v", sddl, same, err)
		}
	}
}
