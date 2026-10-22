package workspacefiles

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageBoundsAndActualType(t *testing.T) {
	svc, ref, path := fixtureService(t)
	key := mustKey(t, svc, ref)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/pXcAAAAASUVORK5CYII=")
	if err := os.WriteFile(filepath.Join(path, "pixel.bin"), png, 0600); err != nil {
		t.Fatal(err)
	}
	got := svc.ReadImage(ref, key, "pixel.bin")
	wantCode(t, got, "ok")
	if got.Image == nil || got.Image.Mime != "image/png" || got.Image.Size != int64(len(png)) || !strings.HasPrefix(got.Image.DataURL, "data:image/png;base64,") {
		t.Fatalf("image=%#v", got.Image)
	}
	if err := os.WriteFile(filepath.Join(path, "fake.png"), []byte("<html>unsafe</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.ReadImage(ref, key, "fake.png"), "unsupported_format")
	if err := os.WriteFile(filepath.Join(path, "fake.webp"), []byte("RIFF0000WEBPVP8 "), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.ReadImage(ref, key, "fake.webp"), "unsupported_format")
	if err := os.WriteFile(filepath.Join(path, "large.png"), append(png, bytes.Repeat([]byte{'x'}, (8<<20)+1)...), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.ReadImage(ref, key, "large.png"), "too_large")
}

func TestAnimatedWebPReadAndMalformedFrames(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	animated, err := os.ReadFile(filepath.Join("testdata", "animated.webp"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(animated, []byte("ANMF")) {
		t.Fatal("fixture is not an animated WebP")
	}
	if err := os.WriteFile(filepath.Join(root, "animated.bin"), animated, 0600); err != nil {
		t.Fatal(err)
	}
	got := svc.ReadImage(ref, key, "animated.bin")
	wantCode(t, got, "ok")
	if got.Image == nil || got.Image.Mime != "image/webp" || got.Image.Size != int64(len(animated)) {
		t.Fatalf("animated image = %#v", got.Image)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got.Image.DataURL, "data:image/webp;base64,"))
	if err != nil || !bytes.Equal(decoded, animated) {
		t.Fatal("animated data URL did not preserve the file")
	}

	broken := append([]byte(nil), animated...)
	frameAt := bytes.Index(broken, []byte("ANMF"))
	if frameAt < 0 {
		t.Fatal("missing ANMF chunk")
	}
	broken[frameAt+28] = 0xff // nested bitstream claims to exceed its ANMF frame
	if err := os.WriteFile(filepath.Join(root, "broken.webp"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.ReadImage(ref, key, "broken.webp"), "unsupported_format")

	invalidFlags := append([]byte(nil), animated...)
	invalidFlags[frameAt+8+15] = 0xfc // reserved ANMF frame bits
	if err := os.WriteFile(filepath.Join(root, "invalid-flags.webp"), invalidFlags, 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.ReadImage(ref, key, "invalid-flags.webp"), "unsupported_format")
}
