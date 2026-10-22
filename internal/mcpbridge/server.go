package mcpbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRequestBytes = 256 << 10

const progressCountMeta = "io.laraenv/progressCount"

type Server struct {
	mu         sync.Mutex
	opts       Options
	sdk        *mcp.Server
	http       *http.Server
	descriptor Descriptor
	runCtx     context.Context
	cancel     context.CancelFunc
	closed     bool
	requests   chan struct{}
}

func NewServer(opts Options) (*Server, error) {
	if opts.Backend == nil {
		return nil, errors.New("MCP backend is required")
	}
	if strings.TrimSpace(opts.DescriptorPath) == "" {
		return nil, errors.New("MCP connection path is required")
	}
	path, err := filepath.Abs(opts.DescriptorPath)
	if err != nil {
		return nil, errors.New("invalid MCP connection path")
	}
	opts.DescriptorPath = path
	if opts.Version == "" {
		opts.Version = "development"
	}
	s := &Server{opts: opts, requests: make(chan struct{}, 16)}
	s.sdk = newToolServer(opts.Version, s.call)
	s.sdk.AddReceivingMiddleware(s.auditMiddleware)
	return s, nil
}

type auditContextKey struct{}

func auditLabel(value string, maximum int) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}

func (s *Server) auditMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		started := time.Now()
		event := AuditEvent{ID: rand.Text(), Group: "unknown", Action: "invalid", StartedAt: started.UTC()}
		if call, ok := req.(*mcp.CallToolRequest); ok {
			var args struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(call.Params.Arguments, &args)
			for _, spec := range toolSpecs {
				if call.Params.Name == "laraenv_"+spec.group {
					event.Group = spec.group
					for _, action := range spec.actions {
						if args.Action == action {
							event.Action = action
							break
						}
					}
					break
				}
			}
		}
		ctx = context.WithValue(ctx, auditContextKey{}, &event)
		defer func() {
			event.DurationMS = time.Since(started).Milliseconds()
			event.Success = err == nil
			if res, ok := result.(*mcp.CallToolResult); ok && res.IsError {
				event.Success = false
			}
			event.Cancelled = event.Cancelled || ctx.Err() != nil
			if s.opts.Audit != nil {
				func() { defer func() { _ = recover() }(); s.opts.Audit(event) }()
			}
		}()
		return next(ctx, method, req)
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("MCP server is closed")
	}
	if s.http != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errors.New("cannot listen for local MCP clients")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		listener.Close()
		return errors.New("cannot create MCP credential")
	}
	s.descriptor = Descriptor{URL: "http://" + listener.Addr().String() + "/mcp", Token: base64.RawURLEncoding.EncodeToString(secret), PID: os.Getpid()}
	s.runCtx, s.cancel = context.WithCancel(ctx)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.sdk }, &mcp.StreamableHTTPOptions{MaxRequestBodyBytes: maxRequestBytes, SessionTimeout: 5 * time.Minute, PropagateRequestCancellation: true})
	s.http = &http.Server{Handler: s.guard(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return s.runCtx }}
	if err := writeDescriptor(s.opts.DescriptorPath, s.descriptor); err != nil {
		s.cancel()
		listener.Close()
		s.http = nil
		return err
	}
	go func() { _ = s.http.Serve(listener) }()
	go func() { <-s.runCtx.Done(); _ = s.Close() }()
	return nil
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Host != strings.TrimPrefix(strings.TrimSuffix(s.descriptor.URL, "/mcp"), "http://") {
			http.Error(w, "invalid local host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != strings.TrimSuffix(s.descriptor.URL, "/mcp") {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		values := r.Header.Values("Authorization")
		if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+s.descriptor.Token)) != 1 {
			http.Error(w, "local MCP authorization required", http.StatusUnauthorized)
			return
		}
		if r.ContentLength > maxRequestBytes {
			http.Error(w, "MCP request too large", http.StatusRequestEntityTooLarge)
			return
		}
		select {
		case s.requests <- struct{}{}:
			defer func() { <-s.requests }()
		default:
			http.Error(w, "too many MCP requests", http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("Mcp-Session-Id") == "" {
			count := 0
			for range s.sdk.Sessions() {
				count++
			}
			if count >= 32 {
				http.Error(w, "too many MCP sessions", http.StatusTooManyRequests)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) call(ctx context.Context, req *mcp.CallToolRequest, group string, args Args) (res *mcp.CallToolResult, err error) {
	event, _ := ctx.Value(auditContextKey{}).(*AuditEvent)
	if event != nil {
		event.Project = auditLabel(args.Project, 255)
		event.Service = auditLabel(args.Service, 128)
		event.Kind = auditLabel(args.Kind, 32)
		if group == "db" {
			event.Database = auditLabel(args.Database, 128)
			event.Engine = auditLabel(args.Engine, 16)
		}
		if args.Kind == "cron" && (group == "worker" || group == "logs") {
			event.JobID = auditLabel(args.ID, 128)
		}
	}
	timeout := 60 * time.Second
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s.mu.Lock()
	runCtx := s.runCtx
	s.mu.Unlock()
	if runCtx != nil {
		stop := context.AfterFunc(runCtx, cancel)
		defer stop()
	}
	defer func() {
		if recover() != nil {
			err = errors.New("LaraEnv MCP operation failed")
			res = nil
		}
		if event != nil {
			event.Cancelled = ctx.Err() != nil
		}
	}()
	var progressMu sync.Mutex
	var step uint64
	progress := func(message string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		if token := req.Params.GetProgressToken(); token != nil && ctx.Err() == nil {
			if len(message) > 4096 {
				message = message[:4096]
			}
			step++
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: float64(step), Message: message})
		}
	}
	value, backendErr := s.opts.Backend.Call(ctx, group, args, progress)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res, err = resultFor(value)
	if err == nil && backendErr != nil {
		res.IsError = true
		res.Content = append(res.Content, &mcp.TextContent{Text: backendErr.Error()})
	}
	if err == nil && req.Params.GetProgressToken() != nil {
		progressMu.Lock()
		res.Meta = mcp.Meta{progressCountMeta: step}
		progressMu.Unlock()
	}
	return res, err
}

func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Status{Running: s.http != nil && !s.closed, DescriptorPath: s.opts.DescriptorPath}
	if status.Running {
		status.URL = s.descriptor.URL
	}
	return status
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	server := s.http
	cancel := s.cancel
	descriptor := s.descriptor
	path := s.opts.DescriptorPath
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for session := range s.sdk.Sessions() {
		_ = session.Close()
	}
	var err error
	if server != nil {
		err = server.Close()
	}
	if current, readErr := ReadDescriptor(path); readErr == nil && current.Token == descriptor.Token && current.URL == descriptor.URL {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			err = fmt.Errorf("cannot remove owned MCP connection")
		}
	}
	return err
}
