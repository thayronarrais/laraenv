package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
)

// Resource kinds. Mirrors the server-side enum in ResourceController.
const (
	KindSSHHost      = "ssh_host"
	KindCron         = "cron"
	KindThemePref    = "theme_pref"
	KindTerminalPref = "terminal_pref"
	KindDeployPreset = "deploy_preset"
)

// remoteResource is what the server returns. Ciphertext arrives as base64.
type remoteResource struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Ciphertext string `json:"ciphertext"`
	Version    int    `json:"version"`
	UpdatedAt  string `json:"updated_at"`
}

type listResponse struct {
	Resources []remoteResource `json:"resources"`
}

// SyncStatus is the snapshot the UI uses for the Sync section.
type SyncStatus struct {
	Enabled       bool           `json:"enabled"`
	HasKey        bool           `json:"hasKey"`
	Reachable     bool           `json:"reachable"`
	IsPro         bool           `json:"isPro"`
	LastError     string         `json:"lastError,omitempty"`
	ResourceCount int            `json:"resourceCount"`
	Encryption    SyncEncryption `json:"encryption"`
}

// SyncEncryption describes how synced data is protected. Every field mirrors
// the implementation in crypto.go / PutResource: payloads are encrypted on
// this machine before upload, the key is derived locally from the user's
// password or sync passphrase and the server only ever stores ciphertext.
type SyncEncryption struct {
	Cipher      string `json:"cipher"`      // e.g. "AES-256-GCM"
	KDF         string `json:"kdf"`         // e.g. "Argon2id"
	KeyOnDevice bool   `json:"keyOnDevice"` // a derived key is present locally
}

// SetSyncEnabled flips the local toggle. Doesn't actually push/pull —
// caller should call SyncNow afterwards.
func (c *Client) SetSyncEnabled(enabled bool) error {
	ctx, cancel, err := c.cloudContext(context.Background())
	if err != nil {
		return err
	}
	defer cancel()
	return c.updateSession(ctx, func(cl *config.Cloud) { cl.SyncEnabled = enabled })
}

// HaveKey returns true if a sync key is cached locally (i.e. the user
// signed in with email/password and we derived one). GitHub-only users
// don't have a key yet — they need to set a sync passphrase first
// (out of scope for this phase).
func (c *Client) HaveKey() bool {
	return c.cfg.Get().Cloud.SyncKey != ""
}

// PrimeKey derives the AES-256 key from the user's password and persists
// it locally (base64). Called from Login/Register where we still have the
// plaintext password.
func (c *Client) PrimeKey(password string) error {
	ctx, cancel, err := c.cloudContext(context.Background())
	if err != nil {
		return err
	}
	defer cancel()
	return c.primeKey(ctx, password)
}

func (c *Client) primeKey(ctx context.Context, password string) error {
	ctx, cancel, cl, err := c.BeginSession(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if cl.KdfSalt == "" {
		return i18n.Errorf("errors.cloud.kdfSaltMissing", nil)
	}
	key, err := DeriveKey(password, cl.KdfSalt)
	if err != nil {
		return err
	}
	return c.updateSession(ctx, func(cl *config.Cloud) { cl.SyncKey = EncodeKey(key) })
}

// loadKey returns the raw 32-byte key from cfg, or an error if absent.
func (c *Client) loadKey() ([]byte, error) {
	return c.loadSessionKey(context.Background())
}

func (c *Client) loadSessionKey(ctx context.Context) ([]byte, error) {
	_, cancel, snapshot, err := c.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	enc := snapshot.SyncKey
	if enc == "" {
		return nil, i18n.Errorf("errors.cloud.syncKeyMissing", nil)
	}
	return DecodeKey(enc)
}

// PutResource encrypts `payload` (the marshaled JSON of an SSH host, cron job,
// etc.), bumps the version, and PUTs to /resources/{kind}/{externalID}.
//
// Returns ErrUnauthenticated if the token is gone, ErrNotPro if the server
// returned 402 (out-of-sync entitlement), or any other server error.
func (c *Client) PutResource(ctx context.Context, kind, externalID string, payload []byte, currentVersion int) (resultErr error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { c.recordSyncResult(ctx, resultErr) }()
	if c.token() == "" {
		return ErrUnauthenticated
	}
	key, err := c.loadSessionKey(ctx)
	if err != nil {
		return err
	}
	blob, err := Encrypt(key, payload)
	if err != nil {
		return i18n.Wrapf(err, "errors.cloud.encryptFailed", nil)
	}
	body := map[string]any{
		"ciphertext": base64.StdEncoding.EncodeToString(blob),
		"version":    currentVersion + 1,
	}
	path := fmt.Sprintf("/api/v1/resources/%s/%s", kind, externalID)
	var out remoteResource
	if err := c.put(ctx, path, body, &out); err != nil {
		return err
	}
	return nil
}

// DeleteResource removes a synced resource by kind+external_id.
func (c *Client) DeleteResource(ctx context.Context, kind, externalID string) (resultErr error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { c.recordSyncResult(ctx, resultErr) }()
	if c.token() == "" {
		return ErrUnauthenticated
	}
	path := fmt.Sprintf("/api/v1/resources/%s/%s", kind, externalID)
	return c.delete(ctx, path)
}

// UndecryptableError signals that PullResources skipped one or more resources
// it couldn't decrypt with the local key — almost always a passphrase mismatch
// across machines (e.g. data encrypted under an old GitHub-era passphrase while
// this machine logged in with email/password). The caller still receives every
// resource that DID decrypt; this is non-fatal and actionable.
type UndecryptableError struct {
	Skipped int      // count of resources that failed to decrypt
	Kinds   []string // distinct kinds affected (e.g. ["ssh_host"])
}

// Error renders via i18n.T rather than i18n.Errorf/Wrapf: this must stay a
// concrete *UndecryptableError so IsUndecryptable's errors.As keeps working
// (an i18n.Errorf-built error would lose the Skipped/Kinds fields), the same
// treatment internal/vcruntime's RedistError uses for the same reason.
func (e *UndecryptableError) Error() string {
	return i18n.T("backend.cloud.undecryptableSkipped", map[string]any{"count": e.Skipped})
}

// IsUndecryptable reports whether err is (or wraps) an UndecryptableError.
func IsUndecryptable(err error) bool {
	var u *UndecryptableError
	return errors.As(err, &u)
}

// listRaw fetches the raw (still-encrypted) resources of the given kind.
// `kind` empty fetches all kinds.
func (c *Client) listRaw(ctx context.Context, kind string) ([]remoteResource, error) {
	path := "/api/v1/resources"
	if kind != "" {
		path += "?kind=" + kind
	}
	var resp listResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Resources, nil
}

// PullResources fetches all resources of the given kind, decrypts them, and
// returns the decoded payloads keyed by external_id. Sorted by external_id
// for deterministic ordering.
//
// `kind` empty fetches all kinds. Caller must dispatch them by kind.
//
// Resources that fail to decrypt with the local key are SKIPPED (not fatal):
// the successfully decrypted ones are still returned, alongside a non-nil
// *UndecryptableError so the caller can surface an actionable message. A nil
// error means everything decrypted cleanly. Network/auth failures still return
// a nil slice + hard error.
func (c *Client) PullResources(ctx context.Context, kind string) (result []DecryptedResource, resultErr error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer func() { c.recordSyncResult(ctx, resultErr) }()
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	key, err := c.loadSessionKey(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := c.listRaw(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]DecryptedResource, 0, len(resources))
	skipped := 0
	badKinds := map[string]struct{}{}
	for _, r := range resources {
		blob, err := base64.StdEncoding.DecodeString(r.Ciphertext)
		if err != nil {
			skipped++
			badKinds[r.Kind] = struct{}{}
			fmt.Printf("[cloud] skip %s/%s: base64 decode: %v\n", r.Kind, r.ExternalID, err)
			continue
		}
		plaintext, err := Decrypt(key, blob)
		if err != nil {
			skipped++
			badKinds[r.Kind] = struct{}{}
			fmt.Printf("[cloud] skip %s/%s: decrypt: %v (wrong passphrase?)\n", r.Kind, r.ExternalID, err)
			continue
		}
		out = append(out, DecryptedResource{
			Kind:       r.Kind,
			ExternalID: r.ExternalID,
			Payload:    plaintext,
			Version:    r.Version,
			UpdatedAt:  r.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ExternalID < out[j].ExternalID
	})
	if skipped > 0 {
		kinds := make([]string, 0, len(badKinds))
		for k := range badKinds {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		return out, &UndecryptableError{Skipped: skipped, Kinds: kinds}
	}
	return out, nil
}

// RekeyResources re-encrypts every synced resource under newKey. It decrypts
// each blob with the current local key, re-encrypts with newKey, and PUTs it
// back (bumping the version, with one conflict retry). On success it persists
// newKey as the local SyncKey so this machine keeps working afterwards.
//
// Run this on the machine that can still decrypt (it holds the old key). After
// re-keying, any other machine whose key already equals newKey decrypts
// everything with no further action.
//
// Returns (rekeyed, skipped, err). Resources that won't decrypt with the
// current key are skipped. If NOTHING decrypts (rekeyed == 0 but resources
// existed), the current key is wrong for this data — returns an error WITHOUT
// clobbering the local SyncKey.
func (c *Client) RekeyResources(ctx context.Context, newKey []byte) (rekeyed int, skipped int, err error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer cancel()
	if c.token() == "" {
		return 0, 0, ErrUnauthenticated
	}
	if len(newKey) != 32 {
		return 0, 0, i18n.Errorf("errors.cloud.newKeyMustBe32Bytes", map[string]any{"got": len(newKey)})
	}
	oldKey, err := c.loadSessionKey(ctx)
	if err != nil {
		return 0, 0, err
	}
	resources, err := c.listRaw(ctx, "")
	if err != nil {
		return 0, 0, err
	}
	for _, r := range resources {
		blob, derr := base64.StdEncoding.DecodeString(r.Ciphertext)
		if derr != nil {
			skipped++
			continue
		}
		plain, derr := Decrypt(oldKey, blob)
		if derr != nil {
			skipped++
			continue
		}
		nb, eerr := Encrypt(newKey, plain)
		if eerr != nil {
			return rekeyed, skipped, i18n.Wrapf(eerr, "errors.cloud.reencryptResourceFailed", map[string]any{"kind": r.Kind, "externalId": r.ExternalID})
		}
		path := fmt.Sprintf("/api/v1/resources/%s/%s", r.Kind, r.ExternalID)
		body := map[string]any{
			"ciphertext": base64.StdEncoding.EncodeToString(nb),
			"version":    r.Version + 1,
		}
		perr := c.put(ctx, path, body, &remoteResource{})
		if perr != nil && IsConflict(perr) {
			// Someone bumped the version between our list and PUT — refetch and retry once.
			if cur, ferr := c.fetchOne(ctx, r.Kind, r.ExternalID); ferr == nil {
				body["version"] = cur.Version + 1
				perr = c.put(ctx, path, body, &remoteResource{})
			}
		}
		if perr != nil {
			return rekeyed, skipped, i18n.Wrapf(perr, "errors.cloud.rekeyResourceFailed", map[string]any{"kind": r.Kind, "externalId": r.ExternalID})
		}
		rekeyed++
	}
	if rekeyed == 0 && len(resources) > 0 {
		return 0, skipped, i18n.Errorf("errors.cloud.rekeyNothingDecrypted", map[string]any{"count": len(resources)})
	}
	if serr := c.updateSession(ctx, func(cl *config.Cloud) { cl.SyncKey = EncodeKey(newKey) }); serr != nil {
		return rekeyed, skipped, serr
	}
	return rekeyed, skipped, nil
}

// DecryptedResource is a server resource after the local AES-GCM open.
type DecryptedResource struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"externalId"`
	Payload    []byte `json:"-"`       // raw plaintext bytes (typically JSON)
	PayloadStr string `json:"payload"` // populated for UI inspection only
	Version    int    `json:"version"`
	UpdatedAt  string `json:"updatedAt"`
}

// recordSyncResult never adopts a newer account and never performs HTTP.
func (c *Client) recordSyncResult(ctx context.Context, result error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.validateSessionLocked(ctx); err != nil {
		return
	}
	if result != nil {
		c.lastSyncError, c.syncReachable = result.Error(), false
	} else {
		c.lastSyncError, c.syncReachable = "", true
	}
}

// SyncStatusSnapshot returns a cheap, no-network status for the Sync UI.
func (c *Client) SyncStatusSnapshot() SyncStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cl := c.cfg.Get().Cloud
	return SyncStatus{
		Enabled:    cl.Enabled && cl.SyncEnabled,
		HasKey:     cl.SyncKey != "",
		LastError:  c.lastSyncError,
		Reachable:  c.syncReachable,
		IsPro:      cl.Plan == "pro",
		Encryption: SyncEncryption{Cipher: CipherName, KDF: KDFName, KeyOnDevice: cl.SyncKey != ""},
	}
}

// PutResourceJSON is a convenience: marshals the value to JSON and pushes it.
func (c *Client) PutResourceJSON(ctx context.Context, kind, externalID string, value any, currentVersion int) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.PutResource(ctx, kind, externalID, b, currentVersion)
}

// PutResourceWithRetry performs a PUT, and if the server returns 409 (stale
// version) it parses the current version from the response, bumps it, and
// retries once. The server's 409 body looks like { current: { version, ... }}.
func (c *Client) PutResourceWithRetry(ctx context.Context, kind, externalID string, payload []byte) (resultErr error) {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { c.recordSyncResult(ctx, resultErr) }()
	if err := c.PutResource(ctx, kind, externalID, payload, 0); err == nil {
		return nil
	} else if !IsConflict(err) {
		return err
	}
	// Refetch to get the current server version, then PUT version+1.
	cur, err := c.fetchOne(ctx, kind, externalID)
	if err != nil {
		return err
	}
	return c.PutResource(ctx, kind, externalID, payload, cur.Version)
}

// fetchOne is the GET counterpart to PutResource — fetches a single resource
// (without decrypting; caller decides). Used by the conflict-retry path to
// learn the current server version.
func (c *Client) fetchOne(ctx context.Context, kind, externalID string) (*remoteResource, error) {
	path := fmt.Sprintf("/api/v1/resources/%s/%s", kind, externalID)
	var r remoteResource
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// IsConflict returns true for the 409 last-write-wins conflict response.
// Callers can use this to decide whether to refetch + retry.
func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Status == 409
}

// IsNotPro detects the 402 Payment Required response. UI should show an
// upgrade prompt.
func IsNotPro(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 402 {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message), "pro")
}
