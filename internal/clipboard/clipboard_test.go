package clipboard

import (
	"reflect"
	"strings"
	"testing"
)

func TestClipboardTextRoundTrip(t *testing.T) {
	for _, text := range []string{"", "line one\r\nline two\n", "你好 café 🦊", strings.Repeat("x", 1<<20) + "🦊"} {
		encoded, err := encodeText(text)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeText(encoded)
		if err != nil || got != text {
			t.Fatalf("UTF16 round trip changed text of length %d: error=%v", len(text), err)
		}
	}
}

func TestClipboardTextValidation(t *testing.T) {
	if _, err := encodeText("before\x00after"); err == nil {
		t.Fatal("embedded NUL must be rejected before touching the clipboard")
	}
	if _, err := decodeText([]uint16{'x', 'y'}); err == nil {
		t.Fatal("unterminated data must not be read past its allocation")
	}
	encoded, err := encodeText("line\n")
	if err != nil || !reflect.DeepEqual(encoded, []uint16{'l', 'i', 'n', 'e', '\n', 0}) {
		t.Fatalf("unexpected encoding: %v, %v", encoded, err)
	}
	text, err := decodeText([]uint16{'x', 0, 'y'})
	if err != nil || text != "x" {
		t.Fatalf("must stop at terminator inside allocation: %q, %v", text, err)
	}
}
