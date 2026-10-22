package workspacefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

type ResolveSource func(ContextRef) (string, string, error)

type Service struct{ resolve ResolveSource }

func NewService(resolve ResolveSource) *Service { return &Service{resolve: resolve} }

// OpenContext retains the authoritative directory until the caller closes it.
func (s *Service) OpenContext(ref ContextRef, expectedRootKey string) (*os.Root, *RootInfo, Result) {
	return s.openContext(ref, expectedRootKey)
}

func validContext(ref ContextRef) bool {
	switch ref.Kind {
	case "agent":
		return ref.SessionID != "" && ref.ProjectName == ""
	case "project":
		return ref.ProjectName != "" && ref.SessionID == ""
	default:
		return false
	}
}

func (s *Service) openContext(ref ContextRef, expected string) (*os.Root, *RootInfo, Result) {
	if !validContext(ref) {
		return nil, nil, problem("invalid_path", errors.New("invalid workspace context"))
	}
	if s == nil || s.resolve == nil {
		return nil, nil, problem("unavailable", errors.New("workspace source unavailable"))
	}
	path, label, err := s.resolve(ref)
	if err != nil || path == "" || !filepath.IsAbs(path) {
		return nil, nil, problem("unavailable", errors.New("workspace source unavailable"))
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, problem("unavailable", err)
	}
	canonical, id, err := directoryIdentity(root, path)
	if err != nil {
		root.Close()
		return nil, nil, problem("unavailable", err)
	}
	keyBytes := sha256.Sum256([]byte(canonical + "\x00" + id))
	info := &RootInfo{RootKey: hex.EncodeToString(keyBytes[:]), CanonicalRoot: canonical, Label: label, Path: path, CanRead: true, CanWrite: true}
	if expected != "" && expected != info.RootKey {
		root.Close()
		return nil, nil, problem("unavailable", errors.New("workspace root changed"))
	}
	return root, info, Result{}
}

func (s *Service) Context(ref ContextRef) Result {
	root, info, issue := s.openContext(ref, "")
	if issue.Code != "" {
		return issue
	}
	defer root.Close()
	return Result{Code: "ok", Context: info}
}
