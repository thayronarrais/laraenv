// Package cloud is the client-side counterpart to laraenv-server.
// Handles auth, /me refresh, and (in later phases) zero-knowledge sync of
// SSH hosts / crons / preferences and remote-deploy orchestration.
//
// Session state is persisted via internal/config (the same config.json that
// holds projects/services). Token is stored in plaintext for now — Phase 5
// will move it to the Windows Credential Manager.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
)

// User mirrors the User model returned by the Laravel server.
type User struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Plan      string `json:"plan"`
	GithubID  string `json:"github_id"`
	KdfSalt   string `json:"kdf_salt"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// MeResponse is the shape of GET /api/v1/me.
type MeResponse struct {
	User         User            `json:"user"`
	Plan         string          `json:"plan"`
	KdfSalt      string          `json:"kdf_salt"`
	Entitlements map[string]bool `json:"entitlements"`
}

// AuthResponse is the shape returned by /register and /login.
type AuthResponse struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

// Status is what the frontend reads to render the Account tab.
type Status struct {
	Enabled      bool            `json:"enabled"`
	AccountID    int             `json:"accountId"`
	LoggedIn     bool            `json:"loggedIn"`
	BaseURL      string          `json:"baseUrl"`
	Email        string          `json:"email,omitempty"`
	Name         string          `json:"name,omitempty"`
	GithubID     string          `json:"githubId,omitempty"`
	Plan         string          `json:"plan,omitempty"`
	Entitlements map[string]bool `json:"entitlements,omitempty"`
}

// Client is the HTTP wrapper around laraenv-server. Mutates the persisted
// config.Cloud block on login/logout/refresh.
type Client struct {
	mu              sync.RWMutex
	cfg             *config.Store
	http            *http.Client
	session         context.Context
	cancelSession   context.CancelFunc
	sessionIdentity config.Cloud
	generation      uint64
	nextWork        uint64
	activeWork      map[uint64]context.CancelFunc
	lastSyncError   string
	syncReachable   bool
}

func NewClient(cfg *config.Store) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func cloudEndpoint(url string) string {
	if url == "" {
		url = "http://laraenv-server.test"
	}
	return strings.TrimRight(url, "/")
}
func (c *Client) baseURL() string { return cloudEndpoint(c.cfg.Get().Cloud.BaseURL) }

func (c *Client) token() string { return c.cfg.Get().Cloud.Token }

// Status returns the cached session info for the UI. Cheap, no network.
func (c *Client) Status() Status {
	cl := c.cfg.Get().Cloud
	return Status{
		Enabled:   cl.Enabled,
		AccountID: cl.UserID,
		LoggedIn:  cl.Token != "",
		BaseURL:   cloudEndpoint(cl.BaseURL),
		Email:     cl.Email,
		Name:      cl.Name,
		GithubID:  cl.GithubID,
		Plan:      cl.Plan,
		Entitlements: map[string]bool{
			"sync":        cl.Plan == "pro",
			"deployments": cl.Plan == "pro",
		},
	}
}

// A captured session is immutable and belongs to one client and lifetime.
type sessionContextKey struct{}
type capturedCloudSession struct {
	owner         *Client
	generation    uint64
	cloud         config.Cloud
	authorization string
	oauthProbe    bool
	logout        bool
}

func sameCloudIdentity(a, b config.Cloud) bool {
	return a.Enabled == b.Enabled && a.BaseURL == b.BaseURL && a.Token == b.Token &&
		a.UserID == b.UserID && a.SyncKey == b.SyncKey && a.KdfSalt == b.KdfSalt
}

func (c *Client) invalidateSessionLocked(resetSync bool) {
	c.generation++
	if c.cancelSession != nil {
		c.cancelSession()
	}
	for id, cancel := range c.activeWork {
		cancel()
		delete(c.activeWork, id)
	}
	c.session, c.cancelSession = nil, nil
	if resetSync {
		c.lastSyncError, c.syncReachable = "", false
	}
}

func (c *Client) ensureSessionLocked(cl config.Cloud) {
	if c.session != nil && !sameCloudIdentity(c.sessionIdentity, cl) {
		c.invalidateSessionLocked(true)
	}
	if c.session == nil {
		c.session, c.cancelSession = context.WithCancel(context.Background())
		c.sessionIdentity = cl
	}
}

func (c *Client) validateSessionLocked(ctx context.Context) (config.Cloud, error) {
	cl := c.cfg.Get().Cloud
	if !cl.Enabled {
		return config.Cloud{}, ErrDisabled
	}
	captured, ok := ctx.Value(sessionContextKey{}).(*capturedCloudSession)
	if !ok || captured.owner != c || captured.generation != c.generation || !sameCloudIdentity(captured.cloud, cl) {
		return config.Cloud{}, ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return config.Cloud{}, err
	}
	return captured.cloud, nil
}

// BeginSession captures the account, endpoint and encryption key for one whole
// operation. Pass this context through SSH completion, history writes and sync
// retries. It never adopts a later login or server, even if IDs coincide.
func (c *Client) BeginSession(ctx context.Context) (context.Context, context.CancelFunc, config.Cloud, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl := c.cfg.Get().Cloud
	if !cl.Enabled {
		return nil, nil, config.Cloud{}, ErrDisabled
	}
	c.ensureSessionLocked(cl)
	captured, bound := ctx.Value(sessionContextKey{}).(*capturedCloudSession)
	if bound {
		var err error
		cl, err = c.validateSessionLocked(ctx)
		if err != nil {
			return nil, nil, config.Cloud{}, err
		}
	} else {
		if err := ctx.Err(); err != nil {
			return nil, nil, config.Cloud{}, err
		}
		captured = &capturedCloudSession{owner: c, generation: c.generation, cloud: cl, authorization: cl.Token}
	}
	work, cancel := context.WithCancel(ctx)
	c.nextWork++
	id := c.nextWork
	if c.activeWork == nil {
		c.activeWork = make(map[uint64]context.CancelFunc)
	}
	c.activeWork[id] = cancel
	return context.WithValue(work, sessionContextKey{}, captured), func() {
		cancel()
		c.mu.Lock()
		delete(c.activeWork, id)
		c.mu.Unlock()
	}, cl, nil
}

func (c *Client) cloudContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	work, cancel, _, err := c.BeginSession(ctx)
	return work, cancel, err
}

// SetEnabled preserves the saved cloud profile. Disabling also cancels all
// captured operations synchronously, without clearing their last sync failure.
func (c *Client) SetEnabled(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl := c.cfg.Get().Cloud
	cl.Enabled = enabled
	if err := c.cfg.SetCloud(cl); err != nil {
		return err
	}
	if !enabled {
		c.invalidateSessionLocked(false)
	}
	return nil
}

func (c *Client) updateSession(ctx context.Context, update func(*config.Cloud)) error {
	return c.updateSessionTransition(ctx, update, false)
}

func (c *Client) updateSessionTransition(ctx context.Context, update func(*config.Cloud), force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.validateSessionLocked(ctx); err != nil {
		return err
	}
	previous := c.cfg.Get().Cloud
	cl := previous
	update(&cl)
	if err := c.cfg.SetCloud(cl); err != nil {
		return err
	}
	if force || !sameCloudIdentity(previous, cl) {
		c.invalidateSessionLocked(true)
	}
	return nil
}

// SetBaseURL invalidates every old account operation before a new server is used.
func (c *Client) SetBaseURL(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl := c.cfg.Get().Cloud
	cl.BaseURL = strings.TrimRight(url, "/")
	cl.Token, cl.UserID, cl.Email, cl.Name, cl.GithubID, cl.Plan, cl.KdfSalt = "", 0, "", "", "", "", ""
	cl.SyncKey, cl.SyncEnabled = "", false
	if err := c.cfg.SetCloud(cl); err != nil {
		return err
	}
	c.invalidateSessionLocked(true)
	return nil
}

// Register creates a new email/password account. Persists token on success.
// Also derives the AES-256 sync key from the password while we still have
// it in plaintext (Argon2id with the server-issued kdf_salt). The key sits
// in cfg.Cloud.SyncKey across restarts.
func (c *Client) Register(ctx context.Context, name, email, password string) (*User, error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	body := map[string]string{"name": name, "email": email, "password": password}
	var resp AuthResponse
	if err := c.post(ctx, "/api/v1/auth/register", body, &resp); err != nil {
		return nil, err
	}
	if err := c.persistSession(ctx, resp, password); err != nil {
		return nil, err
	}

	return &resp.User, nil
}

// Login swaps email+password for a Sanctum token. Same key-derivation hook
// as Register — we cache the AES key locally so subsequent launches don't
// need the password.
func (c *Client) Login(ctx context.Context, email, password string) (*User, error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	body := map[string]string{"email": email, "password": password}
	var resp AuthResponse
	if err := c.post(ctx, "/api/v1/auth/login", body, &resp); err != nil {
		return nil, err
	}
	if err := c.persistSession(ctx, resp, password); err != nil {
		return nil, err
	}
	return &resp.User, nil
}

// Logout revokes the current token server-side, then clears local session.
// Best-effort: even if the server call fails, we still clear local state.
func (c *Client) Logout(ctx context.Context) error {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	// Logout owns its final local transition even if revocation reports 401.
	// The normal request guard still rejects a changed account before that write.
	captured := *ctx.Value(sessionContextKey{}).(*capturedCloudSession)
	captured.logout = true
	ctx = context.WithValue(ctx, sessionContextKey{}, &captured)
	if c.token() != "" {
		_ = c.post(ctx, "/api/v1/auth/logout", nil, nil)
	}
	return c.updateSessionTransition(ctx, func(cl *config.Cloud) {
		cl.Token, cl.UserID, cl.Email, cl.Name, cl.GithubID, cl.Plan, cl.KdfSalt = "", 0, "", "", "", "", ""
		cl.SyncKey, cl.SyncEnabled = "", false
	}, true)
}

// Me refreshes the local plan/email/name from the server. Called on app
// startup and after billing flow returns. Returns ErrUnauthenticated if the
// stored token has been revoked server-side.
func (c *Client) Me(ctx context.Context) (*MeResponse, error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	var me MeResponse
	if err := c.get(ctx, "/api/v1/me", &me); err != nil {
		return nil, err
	}
	if err := c.updateSession(ctx, func(cl *config.Cloud) {
		cl.UserID = me.User.ID
		cl.Email = me.User.Email
		cl.Name = me.User.Name
		cl.GithubID = me.User.GithubID
		cl.Plan = me.Plan
		cl.KdfSalt = me.KdfSalt
	}); err != nil {
		return nil, err
	}
	return &me, nil
}

// Health pings /health to verify the server URL is reachable. Used by the
// Settings → Account form when the user changes the base URL.
func (c *Client) Health(ctx context.Context) error {
	var out struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
	}
	if err := c.get(ctx, "/api/v1/health", &out); err != nil {
		return err
	}
	if !out.OK || out.Service != "laraenv-server" {
		return i18n.Errorf("errors.cloud.unexpectedServerResponse", map[string]any{"response": fmt.Sprintf("%+v", out)})
	}
	return nil
}

func (c *Client) persistSession(ctx context.Context, resp AuthResponse, password string) error {
	var syncKey string
	if password != "" && resp.User.KdfSalt != "" {
		key, err := DeriveKey(password, resp.User.KdfSalt)
		if err != nil {
			fmt.Printf("[cloud] warn: could not derive sync key: %v\n", err)
		} else {
			syncKey = EncodeKey(key)
		}
	}
	return c.updateSessionTransition(ctx, func(cl *config.Cloud) {
		if cl.UserID != resp.User.ID {
			cl.SyncEnabled = false
		}
		cl.SyncKey = syncKey
		cl.Token = resp.Token
		cl.UserID = resp.User.ID
		cl.Email = resp.User.Email
		cl.Name = resp.User.Name
		cl.GithubID = resp.User.GithubID
		cl.Plan = resp.User.Plan
		cl.KdfSalt = resp.User.KdfSalt
	}, true)
}

func (c *Client) rejectSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.validateSessionLocked(ctx); err != nil {
		return err
	}
	cl := c.cfg.Get().Cloud
	cl.Token, cl.UserID, cl.Email, cl.Name, cl.GithubID, cl.Plan, cl.KdfSalt = "", 0, "", "", "", "", ""
	cl.SyncKey, cl.SyncEnabled = "", false
	if err := c.cfg.SetCloud(cl); err != nil {
		return err
	}
	c.invalidateSessionLocked(true)
	c.lastSyncError, c.syncReachable = ErrUnauthenticated.Error(), false
	return nil
}

// ===== HTTP helpers =====

var ErrUnauthenticated = i18n.Errorf("errors.cloud.notLoggedIn", nil)
var ErrDisabled = i18n.Errorf("errors.cloud.disabled", nil)

// APIError is returned for non-2xx responses. Includes the server's JSON
// message when available so the UI can show a useful error.
type APIError struct {
	Status  int
	Message string
	Body    string
}

// Error renders via i18n.T rather than i18n.Errorf/Wrapf: callers use
// errors.As to read Status/Message/Body back out of a concrete *APIError, so
// it must keep that type (same treatment as UndecryptableError and
// vcruntime's RedistError). e.Message is the server's own response text —
// dynamic and not ours to translate — so only the surrounding "server: ...
// (HTTP ...)" wrapper is authored prose.
func (e *APIError) Error() string {
	if e.Message != "" {
		return i18n.T("backend.cloud.apiErrorWithMessage", map[string]any{"message": e.Message, "status": e.Status})
	}
	return i18n.T("backend.cloud.apiErrorStatus", map[string]any{"status": e.Status})
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	ctx, cancel, snapshot, err := c.BeginSession(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cloudEndpoint(snapshot.BaseURL)+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	authorization := ctx.Value(sessionContextKey{}).(*capturedCloudSession).authorization
	if authorization != "" {
		req.Header.Set("Authorization", "Bearer "+authorization)
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.request(ctx, http.MethodPost, path, body, out)
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodGet, path, nil, out)
}
func (c *Client) put(ctx context.Context, path string, body, out any) error {
	return c.request(ctx, http.MethodPut, path, body, out)
}
func (c *Client) patch(ctx context.Context, path string, body, out any) error {
	return c.request(ctx, http.MethodPatch, path, body, out)
}
func (c *Client) delete(ctx context.Context, path string) error {
	return c.request(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) do(req *http.Request, out any) error {
	ctx := req.Context()
	c.mu.Lock()
	_, err := c.validateSessionLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LaraEnv-Client/0.1.4")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	c.mu.Lock()
	_, err = c.validateSessionLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return err
	}

	captured := ctx.Value(sessionContextKey{}).(*capturedCloudSession)
	if resp.StatusCode == 401 && !captured.oauthProbe && !captured.logout {
		// Server rejected our token — clear the session locally so the UI
		// can redirect to login. Don't recursively call Logout (no token to
		// revoke remotely) — just wipe.
		if err := c.rejectSession(ctx); err != nil {
			return err
		}
		return ErrUnauthenticated
	}
	if resp.StatusCode == 401 {
		return ErrUnauthenticated
	}
	if resp.StatusCode >= 400 {
		var msg struct {
			Message string              `json:"message"`
			Errors  map[string][]string `json:"errors"`
		}
		_ = json.Unmarshal(body, &msg)
		humanMsg := msg.Message
		if len(msg.Errors) > 0 {
			for _, errs := range msg.Errors {
				if len(errs) > 0 {
					humanMsg = errs[0]
					break
				}
			}
		}
		return &APIError{Status: resp.StatusCode, Message: humanMsg, Body: string(body)}
	}
	if out != nil && len(body) > 0 {
		return json.Unmarshal(body, out)
	}
	return nil
}
