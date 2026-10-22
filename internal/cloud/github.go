package cloud

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"lara4ever/internal/i18n"
)

// LoginWithGithub runs the desktop OAuth flow:
//   1. Bind a free port on 127.0.0.1.
//   2. Open the user's browser to the server's /auth/github endpoint, passing
//      our loopback URL as the `cb` query parameter.
//   3. The server redirects to GitHub. After the user authorizes, GitHub
//      redirects back to the server's callback, which mints a token and 302s
//      to our loopback URL with ?token=...
//   4. The tiny in-process HTTP listener captures the token and shuts down.
//   5. We persist the token, fetch /me, and return.
//
// The whole loop times out after 5 minutes so a user who closes the browser
// without finishing doesn't leak the listener.
func (c *Client) LoginWithGithub(ctx context.Context) (*User, error) {
	ctx, cancel, snapshot, err := c.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.cloud.bindLoopbackFailed", nil)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/cb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		token := r.URL.Query().Get("token")
		if token == "" {
			_, _ = w.Write(doneHTML("Login failed", "No token returned by the server. You can close this tab and try again."))
			errCh <- i18n.Errorf("errors.cloud.noTokenInCallback", nil)
			return
		}
		_, _ = w.Write(doneHTML("Signed in", "You are now signed in to LaraEnv. You can close this tab and return to the app."))
		tokenCh <- token
	})

	server := &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
	}()

	cb := fmt.Sprintf("http://127.0.0.1:%d/cb", port)
	authURL := fmt.Sprintf("%s/api/v1/auth/github?cb=%s", cloudEndpoint(snapshot.BaseURL), url.QueryEscape(cb))
	if err := openBrowser(authURL); err != nil {
		return nil, i18n.Wrapf(err, "errors.cloud.openBrowserFailed", nil)
	}

	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	select {
	case token := <-tokenCh:
		// Verify the callback token on the endpoint captured when OAuth began,
		// then commit the full new identity and rotate the prior lifetime once.
		captured := *ctx.Value(sessionContextKey{}).(*capturedCloudSession)
		captured.authorization, captured.oauthProbe = token, true
		probe := context.WithValue(ctx, sessionContextKey{}, &captured)
		var me MeResponse
		if err := c.get(probe, "/api/v1/me", &me); err != nil {
			return nil, i18n.Wrapf(err, "errors.cloud.tokenVerifyFailed", nil)
		}
		me.User.Plan, me.User.KdfSalt = me.Plan, me.KdfSalt
		if err := c.persistSession(ctx, AuthResponse{User: me.User, Token: token}, ""); err != nil {
			return nil, err
		}

		return &me.User, nil

	case err := <-errCh:
		return nil, err

	case <-timeout.C:
		return nil, i18n.Errorf("errors.cloud.oauthTimeout", nil)

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// openBrowser launches the user's default browser to the given URL. Uses
// rundll32 on Windows (most reliable across editions/locales).
func openBrowser(u string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

// doneHTML is the small landing page shown after the OAuth flow completes.
// Matches the dark glassy style of the desktop app for a cohesive feel.
func doneHTML(title, body string) []byte {
	return []byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>` + title + `</title>
<style>
body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
     font-family:-apple-system,Segoe UI,Roboto,sans-serif;
     background:linear-gradient(180deg,#1a0b2e,#0b1026);color:#e4e6ff}
.card{background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);
      border-radius:14px;padding:28px 36px;max-width:420px;text-align:center;
      box-shadow:0 0 60px rgba(124,92,255,0.15)}
h1{margin:0 0 8px;font-size:20px}
p{margin:0;opacity:0.7;font-size:14px;line-height:1.5}
</style></head>
<body><div class="card"><h1>` + title + `</h1><p>` + body + `</p></div></body></html>`)
}
