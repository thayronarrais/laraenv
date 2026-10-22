package workspacefiles

import (
	"errors"
	"os"
)

// ContextRef names one LaraEnv-owned source. A path supplied by the caller is
// never an authority to browse files.
type ContextRef struct {
	Kind        string `json:"kind"`
	SessionID   string `json:"sessionId"`
	ProjectName string `json:"projectName"`
}

type RootInfo struct {
	RootKey       string `json:"rootKey"`
	CanonicalRoot string `json:"canonicalRoot"`
	Label         string `json:"label"`
	Path          string `json:"path"`
	CanRead       bool   `json:"canRead"`
	CanWrite      bool   `json:"canWrite"`
}

type Snapshot struct {
	CanonicalPath string `json:"canonicalPath"`
	Text          string `json:"text"`
	Revision      string `json:"revision"`
	Size          int64  `json:"size"`
	BOM           bool   `json:"bom"`
	EOL           string `json:"eol"`
	ReadOnly      bool   `json:"readOnly"`
}

type DirectoryEntry struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
	Kind         string `json:"kind"`
}

type DirectoryPage struct {
	Entries    []DirectoryEntry `json:"entries"`
	NextCursor string           `json:"nextCursor"`
}

type Image struct {
	DataURL string `json:"dataUrl"`
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
}

// Result is concrete so Wails can generate stable bindings without generic DTOs.
type Result struct {
	Code      string         `json:"code"`
	Message   string         `json:"message,omitempty"`
	Context   *RootInfo      `json:"context,omitempty"`
	Directory *DirectoryPage `json:"directory,omitempty"`
	Document  *Snapshot      `json:"document,omitempty"`
	Image     *Image         `json:"image,omitempty"`
}

func problem(code string, err error) Result {
	var stale staleDirectoryError
	if errors.As(err, &stale) {
		code = "stale_cursor"
	}
	if code == "unavailable" && os.IsPermission(err) {
		code = "permission_denied"
	}
	if code == "io_error" && os.IsPermission(err) {
		code = "permission_denied"
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	return Result{Code: code, Message: message}
}
