// Package ssh handles SSH host management for the SSH page: keypair
// generation in OpenSSH format and the helpers that build the ssh.exe
// command line used to spawn a session inside our integrated terminal.
package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"lara4ever/internal/i18n"

	xssh "golang.org/x/crypto/ssh"
)

// Supported key kinds. Default in the UI is "ed25519".
const (
	KindED25519  = "ed25519"
	KindRSA4096  = "rsa4096"
	KindECDSA256 = "ecdsa256"
)

// GeneratedKey describes a freshly written keypair.
type GeneratedKey struct {
	PrivatePath string `json:"privatePath"`
	PublicPath  string `json:"publicPath"`
	PublicKey   string `json:"publicKey"` // single-line OpenSSH authorized_keys form
}

// Generate creates a new keypair under dir and writes the OpenSSH-format
// private key (mode 0600) and matching .pub file. comment is appended to the
// public key (typical "user@host"). Refuses to overwrite if private file exists.
func Generate(dir, name, kind, comment string) (*GeneratedKey, error) {
	if name == "" {
		return nil, i18n.Errorf("errors.ssh.keyNameRequired", nil)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	priv := filepath.Join(dir, name)
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err == nil {
		return nil, i18n.Errorf("errors.ssh.keyAlreadyExists", map[string]any{"path": priv})
	}

	signer, pubKey, err := generateKeyPair(kind)
	if err != nil {
		return nil, err
	}

	block, err := xssh.MarshalPrivateKey(signer, comment)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.marshalPrivateKeyFailed", nil)
	}
	if err := os.WriteFile(priv, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, err
	}

	pubLine := xssh.MarshalAuthorizedKey(pubKey)
	if comment != "" {
		// MarshalAuthorizedKey ends in "\n"; insert comment before it.
		pubLine = append(pubLine[:len(pubLine)-1], append([]byte(" "+comment), '\n')...)
	}
	if err := os.WriteFile(pub, pubLine, 0644); err != nil {
		return nil, err
	}

	return &GeneratedKey{PrivatePath: priv, PublicPath: pub, PublicKey: string(pubLine)}, nil
}

func generateKeyPair(kind string) (priv any, pub xssh.PublicKey, err error) {
	switch kind {
	case KindED25519, "":
		pubK, privK, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		sshPub, err := xssh.NewPublicKey(pubK)
		if err != nil {
			return nil, nil, err
		}
		return privK, sshPub, nil
	case KindRSA4096:
		k, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return nil, nil, err
		}
		sshPub, err := xssh.NewPublicKey(&k.PublicKey)
		if err != nil {
			return nil, nil, err
		}
		return k, sshPub, nil
	case KindECDSA256:
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		sshPub, err := xssh.NewPublicKey(&k.PublicKey)
		if err != nil {
			return nil, nil, err
		}
		return k, sshPub, nil
	}
	return nil, nil, i18n.Errorf("errors.ssh.unsupportedKeyKind", map[string]any{"kind": kind})
}

// SaveText writes a pasted private (and optional public) key into dir.
// Validates that privateKey parses as a real SSH private key. If publicKey
// is empty, derives it from the private key. Refuses to overwrite.
func SaveText(dir, name, privateKey, publicKey string) (*GeneratedKey, error) {
	if name == "" {
		return nil, i18n.Errorf("errors.ssh.keyNameRequired", nil)
	}
	if privateKey == "" {
		return nil, i18n.Errorf("errors.ssh.privateKeyContentRequired", nil)
	}
	privBytes := []byte(privateKey)
	signer, err := xssh.ParsePrivateKey(privBytes)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.invalidPrivateKey", nil)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	priv := filepath.Join(dir, name)
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err == nil {
		return nil, i18n.Errorf("errors.ssh.keyNameConflict", map[string]any{"name": name, "dir": dir})
	}
	if err := os.WriteFile(priv, privBytes, 0600); err != nil {
		return nil, err
	}

	pubLine := []byte(publicKey)
	if len(pubLine) == 0 {
		pubLine = xssh.MarshalAuthorizedKey(signer.PublicKey())
	}
	if err := os.WriteFile(pub, pubLine, 0644); err != nil {
		return nil, err
	}
	return &GeneratedKey{PrivatePath: priv, PublicPath: pub, PublicKey: string(pubLine)}, nil
}

// ReadFileText is a backend helper used by the frontend's "Import from file"
// button — reads an arbitrary path picked via OpenFileDialog and returns its
// contents as a string. Capped at 1 MiB to avoid loading silly files.
func ReadFileText(path string) (string, error) {
	if path == "" {
		return "", i18n.Errorf("errors.ssh.pathRequired", nil)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > 1<<20 {
		sizeMB := fmt.Sprintf("%.1f", float64(st.Size())/(1<<20))
		return "", i18n.Errorf("errors.ssh.keyFileTooLarge", map[string]any{"sizeMB": sizeMB})
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Import copies an existing private-key file into dir, validating that it
// parses as a real SSH private key first. If a sibling <src>.pub exists it
// is copied alongside as <name>.pub. Returns metadata for the imported key.
//
// targetName is optional — if empty, the source file's basename is used.
// Refuses to overwrite an existing private file.
func Import(dir, src, targetName string) (*GeneratedKey, error) {
	if src == "" {
		return nil, i18n.Errorf("errors.ssh.sourcePathRequired", nil)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.readSourceFailed", nil)
	}
	// Try parsing as a private key (encrypted keys are rejected here — we
	// don't have a passphrase prompt yet). The user gets a clear error.
	if _, err := xssh.ParsePrivateKey(srcBytes); err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.invalidImportedKey", nil)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	name := targetName
	if name == "" {
		name = filepath.Base(src)
	}
	priv := filepath.Join(dir, name)
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err == nil {
		return nil, i18n.Errorf("errors.ssh.keyNameConflict", map[string]any{"name": name, "dir": dir})
	}

	if err := os.WriteFile(priv, srcBytes, 0600); err != nil {
		return nil, err
	}

	// Copy the .pub sibling if present; otherwise derive it from the parsed
	// key so the UI can still show the public form for copy/paste.
	pubLine, _ := os.ReadFile(src + ".pub")
	if len(pubLine) == 0 {
		signer, perr := xssh.ParsePrivateKey(srcBytes)
		if perr == nil {
			pubLine = xssh.MarshalAuthorizedKey(signer.PublicKey())
		}
	}
	if len(pubLine) > 0 {
		_ = os.WriteFile(pub, pubLine, 0644)
	}

	return &GeneratedKey{PrivatePath: priv, PublicPath: pub, PublicKey: string(pubLine)}, nil
}

// ListKeys returns the basenames of private keys in dir (.pub files excluded).
func ListKeys(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".pub" {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// ReadPublicKey returns the contents of the .pub sibling of name, if present.
func ReadPublicKey(dir, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, name+".pub"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
