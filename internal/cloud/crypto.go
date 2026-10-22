package cloud

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"lara4ever/internal/i18n"

	"golang.org/x/crypto/argon2"
)

// Human-readable names of the primitives below. Surfaced by SyncStatus so the
// Account page describes exactly what the code does, not a marketing claim.
const (
	CipherName = "AES-256-GCM"
	KDFName    = "Argon2id"
)

// Argon2id parameters. m=64MB, t=3, p=4 is the OWASP-recommended default for
// password hashing. ~500ms on average laptop hardware. The output is 32 bytes
// (= AES-256 key length).
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
)

// DeriveKey runs Argon2id over (password, hex-encoded salt). Returns the raw
// 32 bytes ready for AES-256-GCM. Same inputs always produce the same output —
// safe to call once per session and cache.
func DeriveKey(password, hexSalt string) ([]byte, error) {
	salt, err := hex.DecodeString(hexSalt)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.cloud.kdfSaltInvalid", nil)
	}
	if len(salt) < 16 {
		return nil, i18n.Errorf("errors.cloud.kdfSaltTooShort", map[string]any{"bytes": len(salt)})
	}
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen), nil
}

// Encrypt produces nonce(12) || aes-256-gcm(plaintext). Output is the raw
// bytes — base64 it before sending over JSON.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, i18n.Errorf("errors.cloud.keyMustBe32Bytes", map[string]any{"got": len(key)})
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Seal appends the ciphertext+tag to nonce, giving us a single buffer.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt. Input layout: nonce(12) || ciphertext+tag.
func Decrypt(key, blob []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, i18n.Errorf("errors.cloud.keyMustBe32Bytes", map[string]any{"got": len(key)})
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, i18n.Errorf("errors.cloud.ciphertextTooShort", nil)
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// EncodeKey / DecodeKey convert raw 32-byte keys to/from base64 for storage
// in config.json.
func EncodeKey(k []byte) string         { return base64.StdEncoding.EncodeToString(k) }
func DecodeKey(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
