package ssh

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	xssh "golang.org/x/crypto/ssh"

	"lara4ever/internal/i18n"
)

// SFTPEntry is the shape both local and remote directory listings serialize as.
type SFTPEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix millis
	IsDir   bool   `json:"isDir"`
	Mode    string `json:"mode"`
}

// SFTPProgress is the payload emitted on the "sftp:progress:<opId>" channel
// throughout a transfer. Files: cumulative byte counters + per-file path.
// Done/Error are only set on the terminal event of an op.
type SFTPProgress struct {
	OpID    string `json:"opId"`
	Bytes   int64  `json:"bytes"`   // bytes copied so far across all files
	Total   int64  `json:"total"`   // total bytes for the whole op (-1 if unknown up front)
	Current string `json:"current"` // path currently being transferred (POSIX/local)
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

// SFTPRegistry tracks one SFTP client per terminal id (lazy) and tracks
// in-flight transfer ops so they can be cancelled.
type SFTPRegistry struct {
	mu      sync.Mutex
	clients map[string]*sftp.Client     // termId -> live SFTP client
	cancels map[string]*atomic.Bool     // opId -> cancel flag
	watches map[string]*atomic.Bool     // sessionId -> stop flag for temp-file mtime poller

	// emit is called on every progress tick. Wired by the App layer to
	// wruntime.EventsEmit so this package stays UI-agnostic.
	emit func(event string, payload SFTPProgress)

	// emitRaw is used for non-progress events (external-edit save notifications).
	emitRaw func(event string, data any)
}

func NewSFTPRegistry(emit func(event string, payload SFTPProgress)) *SFTPRegistry {
	return &SFTPRegistry{
		clients: make(map[string]*sftp.Client),
		cancels: make(map[string]*atomic.Bool),
		watches: make(map[string]*atomic.Bool),
		emit:    emit,
	}
}

// SetRawEmitter wires a generic event emitter used for non-progress events
// such as external-editor mtime-change notifications.
func (r *SFTPRegistry) SetRawEmitter(fn func(event string, data any)) {
	r.emitRaw = fn
}

// Open returns the SFTP client for the given terminal id, creating it from
// the supplied SSH client if needed. resolveClient is called only on cache
// miss; it should look up the *xssh.Client owned by the terminal session.
func (r *SFTPRegistry) Open(termID string, resolveClient func() (*xssh.Client, error)) (*sftp.Client, error) {
	r.mu.Lock()
	if c, ok := r.clients[termID]; ok {
		r.mu.Unlock()
		return c, nil
	}
	r.mu.Unlock()

	sshC, err := resolveClient()
	if err != nil {
		return nil, err
	}
	if sshC == nil {
		return nil, i18n.Errorf("errors.ssh.noLiveConnection", map[string]any{"termID": termID})
	}
	cli, err := sftp.NewClient(sshC)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.openSftpSubsystemFailed", nil)
	}
	r.mu.Lock()
	// Race: another caller may have created one between our checks.
	if existing, ok := r.clients[termID]; ok {
		r.mu.Unlock()
		_ = cli.Close()
		return existing, nil
	}
	r.clients[termID] = cli
	r.mu.Unlock()
	return cli, nil
}

// Get returns the cached SFTP client without creating one.
func (r *SFTPRegistry) Get(termID string) (*sftp.Client, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[termID]
	return c, ok
}

// Close tears down the SFTP client for a given terminal (no-op if absent).
func (r *SFTPRegistry) Close(termID string) {
	r.mu.Lock()
	c, ok := r.clients[termID]
	delete(r.clients, termID)
	r.mu.Unlock()
	if ok && c != nil {
		_ = c.Close()
	}
}

// Cancel marks the op as cancelled. The transfer goroutine checks this
// between writes and aborts at the next opportunity.
func (r *SFTPRegistry) Cancel(opID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if flag, ok := r.cancels[opID]; ok {
		flag.Store(true)
	}
}

func (r *SFTPRegistry) registerOp(opID string) *atomic.Bool {
	flag := &atomic.Bool{}
	r.mu.Lock()
	r.cancels[opID] = flag
	r.mu.Unlock()
	return flag
}

func (r *SFTPRegistry) unregisterOp(opID string) {
	r.mu.Lock()
	delete(r.cancels, opID)
	r.mu.Unlock()
}

// ===== Listings =====

// ListLocal reads the local filesystem at p (OS-native path).
func ListLocal(p string) ([]SFTPEntry, error) {
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		p = home
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]SFTPEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, SFTPEntry{
			Name:    e.Name(),
			Path:    filepath.Join(p, e.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
			IsDir:   e.IsDir(),
			Mode:    info.Mode().String(),
		})
	}
	sortEntries(out)
	return out, nil
}

// ListRemote reads the remote filesystem over SFTP. Always POSIX paths.
func (r *SFTPRegistry) ListRemote(cli *sftp.Client, p string) ([]SFTPEntry, error) {
	if p == "" {
		wd, err := cli.Getwd()
		if err != nil {
			return nil, err
		}
		p = wd
	}
	infos, err := cli.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]SFTPEntry, 0, len(infos))
	for _, info := range infos {
		out = append(out, SFTPEntry{
			Name:    info.Name(),
			Path:    path.Join(p, info.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
			IsDir:   info.IsDir(),
			Mode:    info.Mode().String(),
		})
	}
	sortEntries(out)
	return out, nil
}

func sortEntries(s []SFTPEntry) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].IsDir != s[j].IsDir {
			return s[i].IsDir // dirs first
		}
		return s[i].Name < s[j].Name
	})
}

// ===== Remote management =====

func MkdirRemote(cli *sftp.Client, p string) error    { return cli.MkdirAll(p) }
func RenameRemote(cli *sftp.Client, from, to string) error { return cli.Rename(from, to) }

// RemoveRemote deletes a file or recursively a directory.
func RemoveRemote(cli *sftp.Client, p string) error {
	info, err := cli.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return cli.Remove(p)
	}
	w := cli.Walk(p)
	var dirs []string
	for w.Step() {
		if w.Err() != nil {
			continue
		}
		if w.Stat().IsDir() {
			dirs = append(dirs, w.Path())
			continue
		}
		if err := cli.Remove(w.Path()); err != nil {
			return err
		}
	}
	// Remove dirs deepest-first.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if err := cli.RemoveDirectory(d); err != nil {
			return err
		}
	}
	return nil
}

// ===== Transfers =====

const xferBuf = 64 * 1024

// Upload streams localPath -> remotePath. If localPath is a directory, the
// whole tree is recursively copied: contents land *inside* remotePath
// (which is created if missing). Single files copy local file -> remote
// file path; if remotePath is an existing directory, the basename is appended.
func (r *SFTPRegistry) Upload(cli *sftp.Client, opID, localPath, remotePath string) {
	cancel := r.registerOp(opID)
	defer r.unregisterOp(opID)

	emit := func(p SFTPProgress) {
		if r.emit != nil {
			p.OpID = opID
			r.emit("sftp:progress:"+opID, p)
		}
	}

	info, err := os.Stat(localPath)
	if err != nil {
		emit(SFTPProgress{Done: true, Error: err.Error()})
		return
	}

	// Compute total bytes (single file or recursive walk).
	var total int64
	if info.IsDir() {
		_ = filepath.Walk(localPath, func(_ string, fi os.FileInfo, werr error) error {
			if werr == nil && !fi.IsDir() {
				total += fi.Size()
			}
			return nil
		})
	} else {
		total = info.Size()
		// If remotePath is an existing directory, copy into it.
		if rinfo, rerr := cli.Stat(remotePath); rerr == nil && rinfo.IsDir() {
			remotePath = path.Join(remotePath, filepath.Base(localPath))
		}
	}

	var copied int64
	tick := time.Now()
	report := func(curr string, force bool) {
		if force || time.Since(tick) > 100*time.Millisecond {
			emit(SFTPProgress{Bytes: copied, Total: total, Current: curr})
			tick = time.Now()
		}
	}

	copyFile := func(local, remote string) error {
		src, err := os.Open(local)
		if err != nil {
			return err
		}
		defer src.Close()
		// Ensure parent dir.
		if parent := path.Dir(remote); parent != "" && parent != "." {
			_ = cli.MkdirAll(parent)
		}
		dst, err := cli.Create(remote)
		if err != nil {
			return err
		}
		defer dst.Close()
		buf := make([]byte, xferBuf)
		for {
			if cancel.Load() {
				return i18n.Errorf("errors.ssh.transferCancelled", nil)
			}
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return werr
				}
				copied += int64(n)
				report(remote, false)
			}
			if rerr == io.EOF {
				return nil
			}
			if rerr != nil {
				return rerr
			}
		}
	}

	if !info.IsDir() {
		if err := copyFile(localPath, remotePath); err != nil {
			emit(SFTPProgress{Bytes: copied, Total: total, Current: remotePath, Done: true, Error: err.Error()})
			return
		}
		emit(SFTPProgress{Bytes: copied, Total: total, Current: remotePath, Done: true})
		return
	}

	// Directory upload — mirror the tree under remotePath.
	if err := cli.MkdirAll(remotePath); err != nil {
		emit(SFTPProgress{Done: true, Error: err.Error()})
		return
	}
	werr := filepath.Walk(localPath, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if cancel.Load() {
			return i18n.Errorf("errors.ssh.transferCancelled", nil)
		}
		rel, err := filepath.Rel(localPath, p)
		if err != nil {
			return err
		}
		// Convert to POSIX for the remote side.
		relPosix := filepath.ToSlash(rel)
		target := remotePath
		if relPosix != "." {
			target = path.Join(remotePath, relPosix)
		}
		if fi.IsDir() {
			return cli.MkdirAll(target)
		}
		return copyFile(p, target)
	})
	if werr != nil {
		emit(SFTPProgress{Bytes: copied, Total: total, Done: true, Error: werr.Error()})
		return
	}
	emit(SFTPProgress{Bytes: copied, Total: total, Done: true})
}

// Download streams remotePath -> localPath. Mirrors Upload's semantics.
func (r *SFTPRegistry) Download(cli *sftp.Client, opID, remotePath, localPath string) {
	cancel := r.registerOp(opID)
	defer r.unregisterOp(opID)

	emit := func(p SFTPProgress) {
		if r.emit != nil {
			p.OpID = opID
			r.emit("sftp:progress:"+opID, p)
		}
	}

	rinfo, err := cli.Stat(remotePath)
	if err != nil {
		emit(SFTPProgress{Done: true, Error: err.Error()})
		return
	}

	var total int64
	if rinfo.IsDir() {
		w := cli.Walk(remotePath)
		for w.Step() {
			if w.Err() != nil {
				continue
			}
			if !w.Stat().IsDir() {
				total += w.Stat().Size()
			}
		}
	} else {
		total = rinfo.Size()
		if linfo, lerr := os.Stat(localPath); lerr == nil && linfo.IsDir() {
			localPath = filepath.Join(localPath, path.Base(remotePath))
		}
	}

	var copied int64
	tick := time.Now()
	report := func(curr string, force bool) {
		if force || time.Since(tick) > 100*time.Millisecond {
			emit(SFTPProgress{Bytes: copied, Total: total, Current: curr})
			tick = time.Now()
		}
	}

	copyFile := func(remote, local string) error {
		src, err := cli.Open(remote)
		if err != nil {
			return err
		}
		defer src.Close()
		if parent := filepath.Dir(local); parent != "" {
			_ = os.MkdirAll(parent, 0o755)
		}
		dst, err := os.Create(local)
		if err != nil {
			return err
		}
		defer dst.Close()
		buf := make([]byte, xferBuf)
		for {
			if cancel.Load() {
				return i18n.Errorf("errors.ssh.transferCancelled", nil)
			}
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return werr
				}
				copied += int64(n)
				report(local, false)
			}
			if rerr == io.EOF {
				return nil
			}
			if rerr != nil {
				return rerr
			}
		}
	}

	if !rinfo.IsDir() {
		if err := copyFile(remotePath, localPath); err != nil {
			emit(SFTPProgress{Bytes: copied, Total: total, Current: localPath, Done: true, Error: err.Error()})
			return
		}
		emit(SFTPProgress{Bytes: copied, Total: total, Current: localPath, Done: true})
		return
	}

	if err := os.MkdirAll(localPath, 0o755); err != nil {
		emit(SFTPProgress{Done: true, Error: err.Error()})
		return
	}
	w := cli.Walk(remotePath)
	for w.Step() {
		if w.Err() != nil {
			continue
		}
		if cancel.Load() {
			// Reuses errors.ssh.transferCancelled's text via i18n.T (this is a
			// plain string field, not an error return) so this matches the
			// other three cancellation sites in this file instead of being the
			// one that renders "cancelled" untranslated next to them.
			emit(SFTPProgress{Bytes: copied, Total: total, Done: true, Error: i18n.T("errors.ssh.transferCancelled", nil)})
			return
		}
		rel, err := relPosix(remotePath, w.Path())
		if err != nil {
			emit(SFTPProgress{Done: true, Error: err.Error()})
			return
		}
		target := localPath
		if rel != "." && rel != "" {
			target = filepath.Join(localPath, filepath.FromSlash(rel))
		}
		if w.Stat().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				emit(SFTPProgress{Done: true, Error: err.Error()})
				return
			}
			continue
		}
		if err := copyFile(w.Path(), target); err != nil {
			emit(SFTPProgress{Bytes: copied, Total: total, Current: target, Done: true, Error: err.Error()})
			return
		}
	}
	emit(SFTPProgress{Bytes: copied, Total: total, Done: true})
}

// ===== File read/write =====

// ErrFileTooLarge is returned by ReadRemoteFile when the file exceeds maxBytes.
// The frontend matches on the string prefix to offer the "open with system editor"
// fallback, so do not reword without updating the React side. Both
// errors.ssh.fileTooLarge and errors.ssh.binaryFile are listed in
// langs/.do-not-translate.json — the value must keep the exact English
// substring ("file too large" / "binary file" respectively) in every locale,
// which tools/langs/check.mjs enforces; everything else in the string is
// free to translate.
var (
	errFileTooLargePrefix = "file too large"
	errBinaryFilePrefix   = "binary file"
)

// ReadRemoteFile reads the entire remote file into memory, refusing files
// above maxBytes or files that look binary (NUL byte in the first 8 KiB).
func ReadRemoteFile(cli *sftp.Client, p string, maxBytes int64) ([]byte, error) {
	info, err := cli.Stat(p)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, i18n.Errorf("errors.ssh.pathIsDirectory", nil)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		// The catalogue value must keep the literal English prefix
		// errFileTooLargePrefix ("file too large") in every locale: the
		// frontend (SFTPFileEditorModal.tsx) matches on that exact substring
		// to offer the "open with system editor" fallback, regardless of the
		// active UI language.
		return nil, i18n.Errorf("errors.ssh.fileTooLarge", map[string]any{"size": info.Size(), "limit": maxBytes})
	}
	f, err := cli.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Binary sniff: NUL byte in the first 8 KiB.
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	for _, b := range probe {
		if b == 0 {
			// Same constraint as fileTooLarge above: errBinaryFilePrefix
			// ("binary file") must stay literal in every locale for the
			// frontend's substring match.
			return nil, i18n.Errorf("errors.ssh.binaryFile", map[string]any{"path": p})
		}
	}
	return data, nil
}

// WriteRemoteFile writes data to p, preserving the file's existing perms by
// using OpenFile+O_TRUNC instead of Create (which would reset the mode).
func WriteRemoteFile(cli *sftp.Client, p string, data []byte) error {
	if parent := path.Dir(p); parent != "" && parent != "." {
		_ = cli.MkdirAll(parent)
	}
	f, err := cli.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return nil
}

// DownloadRemoteToTemp copies remotePath to a fresh subdir under the OS temp
// dir, preserving the basename so the OS default-app dialog shows the real
// filename. Returns the local path.
func DownloadRemoteToTemp(cli *sftp.Client, remotePath string) (string, error) {
	base := path.Base(remotePath)
	if base == "" || base == "/" {
		base = "remote-file"
	}
	dir, err := os.MkdirTemp("", "lara4ever-sftp-")
	if err != nil {
		return "", err
	}
	local := filepath.Join(dir, base)
	src, err := cli.Open(remotePath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := os.Create(local)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return local, nil
}

// UploadLocalToRemote copies localPath -> remotePath synchronously.
func UploadLocalToRemote(cli *sftp.Client, localPath, remotePath string) error {
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer src.Close()
	if parent := path.Dir(remotePath); parent != "" && parent != "." {
		_ = cli.MkdirAll(parent)
	}
	dst, err := cli.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// CleanupTemp removes the temp dir that holds tempPath. It walks up to the
// first parent under os.TempDir() — never deletes anything outside.
func CleanupTemp(tempPath string) error {
	if tempPath == "" {
		return nil
	}
	abs, err := filepath.Abs(tempPath)
	if err != nil {
		return err
	}
	tmpRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return err
	}
	if !strings.HasPrefix(abs, tmpRoot) {
		return i18n.Errorf("errors.ssh.cleanupOutsideTempDir", map[string]any{"path": abs})
	}
	parent := filepath.Dir(abs)
	if !strings.HasPrefix(parent, tmpRoot) || parent == tmpRoot {
		return os.Remove(abs)
	}
	return os.RemoveAll(parent)
}

// WatchTemp starts a goroutine that polls tempPath's mtime every second and
// emits "sftp:external-saved:<sessionID>" whenever it advances. The poller
// stops when Unwatch(sessionID) is called.
func (r *SFTPRegistry) WatchTemp(sessionID, tempPath string) error {
	info, err := os.Stat(tempPath)
	if err != nil {
		return err
	}
	stop := &atomic.Bool{}
	r.mu.Lock()
	if prev, ok := r.watches[sessionID]; ok {
		prev.Store(true) // stop any previous watcher for this session
	}
	r.watches[sessionID] = stop
	r.mu.Unlock()

	last := info.ModTime()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if stop.Load() {
				return
			}
			fi, err := os.Stat(tempPath)
			if err != nil {
				continue
			}
			if fi.ModTime().After(last) {
				last = fi.ModTime()
				if r.emitRaw != nil {
					r.emitRaw("sftp:external-saved:"+sessionID, map[string]any{
						"sessionId": sessionID,
						"modTime":   fi.ModTime().UnixMilli(),
					})
				}
			}
		}
	}()
	return nil
}

// UnwatchTemp stops the watcher for a session and forgets it.
func (r *SFTPRegistry) UnwatchTemp(sessionID string) {
	r.mu.Lock()
	if flag, ok := r.watches[sessionID]; ok {
		flag.Store(true)
		delete(r.watches, sessionID)
	}
	r.mu.Unlock()
}

// relPosix returns the POSIX-style relative path of full under base.
func relPosix(base, full string) (string, error) {
	if base == full {
		return ".", nil
	}
	b := base
	if b != "/" && len(b) > 0 && b[len(b)-1] != '/' {
		b += "/"
	}
	if len(full) >= len(b) && full[:len(b)] == b {
		return full[len(b):], nil
	}
	return "", i18n.Errorf("errors.ssh.pathNotUnder", map[string]any{"full": full, "base": base})
}
