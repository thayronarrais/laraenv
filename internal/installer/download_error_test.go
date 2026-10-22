package installer

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/i18n"
)

// TestMain loads the real catalogues before running this package's tests, so
// an *i18n.Error's message (and i18n.T's rendering) show the actual English
// prose (with vars interpolated) instead of degrading to a bare key path —
// downloadFailureReason's tests below assert on that English text ("try
// again", "timed out"). See internal/agents/manager_test.go and
// internal/services/manager_test.go for the same pattern.
func TestMain(m *testing.M) {
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

// The failure a user actually hit: a GitHub release download whose signed
// asset URL runs to hundreds of characters, wrapped by Go around a DNS
// error. Reported verbatim, "no such host" lands past a wall of query
// parameters and reads like the app is broken rather than the network.
func TestDownloadFailureReasonDropsTheSignedURL(t *testing.T) {
	signed := "https://release-assets.githubusercontent.com/github-production-release-asset/29207621/" +
		"ea854b0d-45b9-44d7-ae2a-5d1b706b3876?sp=r&sv=2018-11-09&sr=b&spr=https&sig=" + strings.Repeat("x", 300)
	err := &url.Error{
		Op:  "Get",
		URL: signed,
		Err: &net.DNSError{Name: "release-assets.githubusercontent.com", Err: "no such host", IsNotFound: true},
	}

	got := downloadFailureReason(err)

	if strings.Contains(got, "sig=") || strings.Contains(got, signed) {
		t.Fatalf("the signed URL survived into the message:\n%s", got)
	}
	if !strings.Contains(got, "release-assets.githubusercontent.com") {
		t.Errorf("the host is what makes it diagnosable, and it is missing:\n%s", got)
	}
	if !strings.Contains(got, "try again") {
		t.Errorf("nothing tells the user this is worth retrying:\n%s", got)
	}
	if len(got) > 200 {
		t.Errorf("message is %d chars — still a wall of text:\n%s", len(got), got)
	}
}

func TestDownloadFailureReasonNamesATimeout(t *testing.T) {
	got := downloadFailureReason(&url.Error{Op: "Get", URL: "https://example.test/x.zip", Err: timeoutErr{}})

	if !strings.Contains(got, "timed out") {
		t.Fatalf("a timeout should say so: %s", got)
	}
}

// Anything unrecognised is passed through rather than swallowed — a message
// we cannot improve is still better than one we hide.
func TestDownloadFailureReasonPassesThroughWhatItCannotClassify(t *testing.T) {
	got := downloadFailureReason(errors.New("HTTP 451"))

	if got != "HTTP 451" {
		t.Fatalf("got %q, want the original error", got)
	}
}

func TestDownloadFailureReasonHandlesNoError(t *testing.T) {
	if got := downloadFailureReason(nil); got == "" {
		t.Fatal("a nil error must still produce something to show")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "context deadline exceeded" }
func (timeoutErr) Timeout() bool { return true }
func (timeoutErr) Temporary() bool { return true }
