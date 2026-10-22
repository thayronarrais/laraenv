package workspacefiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

func decodeText(data []byte, canonical string) (*Snapshot, error) {
	sum := sha256.Sum256(data)
	snapshot := &Snapshot{CanonicalPath: canonical, Revision: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		snapshot.BOM = true
		data = data[3:]
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("file is not UTF-8 text")
	}
	text := string(data)
	crlf := strings.Count(text, "\r\n")
	withoutCRLF := strings.ReplaceAll(text, "\r\n", "")
	lf := strings.Count(withoutCRLF, "\n")
	if strings.Contains(withoutCRLF, "\r") {
		return nil, errors.New("unsupported line ending")
	}
	switch {
	case crlf > 0 && lf > 0:
		snapshot.EOL = "mixed"
		snapshot.ReadOnly = true
	case crlf > 0:
		snapshot.EOL = "crlf"
	default:
		snapshot.EOL = "lf"
	}
	snapshot.Text = strings.ReplaceAll(text, "\r\n", "\n")
	return snapshot, nil
}
