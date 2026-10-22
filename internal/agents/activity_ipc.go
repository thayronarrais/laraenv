package agents

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

const activityIPCMaxRequest = 64 << 10
const activityIPCMaxResponse = 2 << 20

type ActivityBackend interface {
	Get(context.Context, string) (ActivitySnapshot, error)
	SetInterest(context.Context, ActivityInterest) error
	RemoveInterest(context.Context, string, string) error
	Refresh(context.Context, string) (ActivitySnapshot, error)
	Wait(context.Context, string, uint64) (ActivitySnapshot, error)
}

type ActivityOwnerServer struct {
	registry   *ActivityOwnerRegistry
	backend    ActivityBackend
	descriptor ActivityOwnerDescriptor
	http       *http.Server
	cancel     context.CancelFunc
	once       sync.Once
	closeErr   error
	requests   chan struct{}
}

func validActivityEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() != "127.0.0.1" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535
}

func StartActivityOwner(ctx context.Context, registry *ActivityOwnerRegistry, backend ActivityBackend) (*ActivityOwnerServer, error) {
	identity, err := NewActivityOwnerIdentity()
	if err != nil {
		return nil, err
	}
	return StartActivityOwnerWithIdentity(ctx, registry, backend, identity)
}

// NewActivityOwnerIdentity creates one process-lifetime identity. Reuse it
// across feature toggles so already spawned PTYs keep a valid owner fence.
func NewActivityOwnerIdentity() (ActivityOwnerIdentity, error) {
	created, err := activityProcessCreation(os.Getpid())
	if err != nil {
		return ActivityOwnerIdentity{}, err
	}
	return ActivityOwnerIdentity{ID: uuid.NewString(), PID: os.Getpid(), CreatedAt: created}, nil
}

func StartActivityOwnerWithIdentity(ctx context.Context, registry *ActivityOwnerRegistry, backend ActivityBackend, identity ActivityOwnerIdentity) (*ActivityOwnerServer, error) {
	if registry == nil || backend == nil {
		return nil, errors.New("activity registry and backend required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	created, err := activityProcessCreation(os.Getpid())
	if err != nil {
		return nil, err
	}
	if !validActivityOwnerID(identity.ID) || identity.PID != os.Getpid() || !identity.CreatedAt.Equal(created) {
		return nil, ErrActivityOwnerUnavailable
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot listen for activity clients")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s := &ActivityOwnerServer{registry: registry, backend: backend, cancel: cancel, requests: make(chan struct{}, 32), descriptor: ActivityOwnerDescriptor{Identity: identity, Endpoint: "http://" + listener.Addr().String(), Token: rand.Text() + rand.Text()}}
	s.http = &http.Server{Handler: http.HandlerFunc(s.serve), BaseContext: func(net.Listener) context.Context { return runCtx }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 8 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10}
	if err := registry.Register(s.descriptor); err != nil {
		cancel()
		listener.Close()
		return nil, err
	}
	go func() { _ = s.http.Serve(listener) }()
	go func() { <-runCtx.Done(); _ = s.Close() }()
	return s, nil
}
func (s *ActivityOwnerServer) Descriptor() ActivityOwnerDescriptor { return s.descriptor }
func (s *ActivityOwnerServer) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.closeErr = errors.Join(s.http.Close(), s.registry.removeOwner(s.descriptor.Identity.ID))
	})
	return s.closeErr
}

type activityIPCRequest struct {
	SessionID string            `json:"sessionId,omitempty"`
	Interest  *ActivityInterest `json:"interest,omitempty"`
	WindowID  string            `json:"windowId,omitempty"`
	Purpose   string            `json:"purpose,omitempty"`
	Revision  uint64            `json:"revision,omitempty"`
}

func (s *ActivityOwnerServer) serve(w http.ResponseWriter, r *http.Request) {
	fail := func(code int) { http.Error(w, "activity request unavailable", code) }
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		fail(http.StatusMethodNotAllowed)
		return
	}
	if r.Host != s.descriptor.Endpoint[len("http://"):] || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.descriptor.Endpoint) {
		fail(http.StatusForbidden)
		return
	}
	auth := r.Header.Values("Authorization")
	if len(auth) != 1 || subtle.ConstantTimeCompare([]byte(auth[0]), []byte("Bearer "+s.descriptor.Token)) != 1 {
		fail(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/get", "/interest", "/remove-interest", "/refresh", "/wait":
	default:
		fail(http.StatusNotFound)
		return
	}
	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	default:
		fail(http.StatusTooManyRequests)
		return
	}
	if _, ok, err := s.registry.ResolveOwner(s.descriptor.Identity.ID); err != nil || !ok {
		fail(http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, activityIPCMaxRequest)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req activityIPCRequest
	if err := decoder.Decode(&req); err != nil {
		fail(http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail(http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/interest" {
		if req.Interest == nil || req.Interest.WindowID == "" || (req.Interest.Purpose != ActivityInterestCounter && req.Interest.Purpose != ActivityInterestDialog) {
			fail(http.StatusBadRequest)
			return
		}
		req.SessionID = req.Interest.SessionID
	}
	var authorizedBinding ActivityBinding
	if r.URL.Path == "/remove-interest" {
		if req.WindowID == "" || (req.Purpose != "counter" && req.Purpose != "dialog") {
			fail(http.StatusBadRequest)
			return
		}
	} else {
		binding, ok, err := s.registry.ResolveBinding(req.SessionID)
		if err != nil || !ok || binding.OwnerID != s.descriptor.Identity.ID {
			fail(http.StatusForbidden)
			return
		}
		authorizedBinding = binding
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var snapshot ActivitySnapshot
	var err error
	switch r.URL.Path {
	case "/get":
		snapshot, err = s.backend.Get(ctx, req.SessionID)
	case "/refresh":
		snapshot, err = s.backend.Refresh(ctx, req.SessionID)
	case "/wait":
		snapshot, err = s.backend.Wait(ctx, req.SessionID, req.Revision)
	case "/interest":
		err = s.backend.SetInterest(ctx, *req.Interest)
	case "/remove-interest":
		err = s.backend.RemoveInterest(ctx, req.WindowID, req.Purpose)
	}
	if err != nil {
		fail(http.StatusServiceUnavailable)
		return
	}
	// Revalidate after an asynchronous backend call: an old owner cannot answer
	// for a session which another process has since resumed.
	if req.SessionID != "" {
		binding, ok, err := s.registry.ResolveBinding(req.SessionID)
		if err != nil || !ok || binding != authorizedBinding {
			fail(http.StatusConflict)
			return
		}
	}
	if r.URL.Path == "/interest" || r.URL.Path == "/remove-interest" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > activityIPCMaxResponse {
		fail(http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

type ActivityClient struct {
	registry   *ActivityOwnerRegistry
	descriptor ActivityOwnerDescriptor
	http       *http.Client
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewActivityClient takes an owner ID, never a caller-supplied URL or token.
// It resolves and revalidates the registry's process identity on every request.
func NewActivityClient(registry *ActivityOwnerRegistry, ownerID string) (*ActivityClient, error) {
	if registry == nil {
		return nil, ErrActivityOwnerUnavailable
	}
	d, ok, err := registry.ResolveOwner(ownerID)
	if err != nil {
		return nil, err
	}
	if !ok || !validActivityEndpoint(d.Endpoint) {
		return nil, ErrActivityOwnerUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ActivityClient{registry: registry, descriptor: d, ctx: ctx, cancel: cancel, http: &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, MaxIdleConnsPerHost: 4, IdleConnTimeout: 15 * time.Second}, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("activity redirect refused") }}}, nil
}
func (c *ActivityClient) Close() error { c.cancel(); c.http.CloseIdleConnections(); return nil }
func (c *ActivityClient) call(ctx context.Context, operation string, input activityIPCRequest) (ActivitySnapshot, error) {
	var out ActivitySnapshot
	d, ok, err := c.registry.ResolveOwner(c.descriptor.Identity.ID)
	if err != nil || !ok || d != c.descriptor {
		return out, ErrActivityOwnerUnavailable
	}
	if err := c.ctx.Err(); err != nil {
		return out, err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return out, err
	}
	if len(data) > activityIPCMaxRequest {
		return out, errors.New("activity request too large")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint+"/"+operation, bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, ErrActivityOwnerUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return out, nil
	}
	if response.StatusCode != http.StatusOK {
		return out, ErrActivityOwnerUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, activityIPCMaxResponse+1))
	if err != nil {
		return out, err
	}
	if len(body) > activityIPCMaxResponse {
		return out, errors.New("activity response too large")
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, errors.New("invalid activity response")
	}
	return out, nil
}
func (c *ActivityClient) Get(ctx context.Context, id string) (ActivitySnapshot, error) {
	return c.call(ctx, "get", activityIPCRequest{SessionID: id})
}
func (c *ActivityClient) Refresh(ctx context.Context, id string) (ActivitySnapshot, error) {
	return c.call(ctx, "refresh", activityIPCRequest{SessionID: id})
}
func (c *ActivityClient) Wait(ctx context.Context, id string, revision uint64) (ActivitySnapshot, error) {
	return c.call(ctx, "wait", activityIPCRequest{SessionID: id, Revision: revision})
}
func (c *ActivityClient) SetInterest(ctx context.Context, interest ActivityInterest) error {
	_, err := c.call(ctx, "interest", activityIPCRequest{Interest: &interest})
	return err
}
func (c *ActivityClient) RemoveInterest(ctx context.Context, windowID, purpose string) error {
	_, err := c.call(ctx, "remove-interest", activityIPCRequest{WindowID: windowID, Purpose: purpose})
	return err
}

var _ ActivityBackend = (*ActivityClient)(nil)
