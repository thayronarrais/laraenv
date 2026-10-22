package mcpbridge

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authTransport struct {
	base       http.RoundTripper
	descriptor Descriptor
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != t.descriptor.URL {
		return nil, errors.New("local MCP transport destination changed")
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.descriptor.Token)
	return t.base.RoundTrip(r)
}

func descriptorClient(d Descriptor) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.MaxIdleConnsPerHost = 8
	return &http.Client{Transport: authTransport{base: transport, descriptor: d}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func newProxy(ctx context.Context, path, version string) (*mcp.Server, func(), error) {
	d, err := ReadDescriptor(path)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	progressHandlers := map[string]func(*mcp.ProgressNotificationParams){}
	client := mcp.NewClient(&mcp.Implementation{Name: "LaraEnv stdio bridge", Version: version}, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
		key, ok := req.Params.ProgressToken.(string)
		if !ok {
			return
		}
		mu.Lock()
		handler := progressHandlers[key]
		mu.Unlock()
		if handler != nil {
			handler(req.Params)
		}
	}})
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	session, err := client.Connect(connectCtx, &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	cancel()
	if err != nil {
		return nil, nil, errors.New("LaraEnv MCP is unavailable; open LaraEnv and enable MCP in Settings")
	}
	proxy := newProxyToolServer(version, func(ctx context.Context, req *mcp.CallToolRequest, group string, args Args) (*mcp.CallToolResult, error) {
		// Forward the validated original object. Re-encoding Args would omit
		// explicit empty version/zero port fields used to clear overrides.
		params := &mcp.CallToolParams{Name: "laraenv_" + group, Arguments: req.Params.Arguments}
		var forwarded atomic.Uint64
		changed := make(chan struct{}, 1)
		if outerToken := req.Params.GetProgressToken(); outerToken != nil {
			key := rand.Text()
			params.SetProgressToken(key)
			mu.Lock()
			progressHandlers[key] = func(p *mcp.ProgressNotificationParams) {
				copy := *p
				copy.ProgressToken = outerToken
				_ = req.Session.NotifyProgress(ctx, &copy)
				forwarded.Add(1)
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			mu.Unlock()
			defer func() { mu.Lock(); delete(progressHandlers, key); mu.Unlock() }()
		}
		result, err := session.CallTool(ctx, params)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("LaraEnv MCP connection ended; reconnect after opening LaraEnv")
		}
		// SDK notification handlers may run after CallTool returns its result.
		// Keep this request's routing alive until its final progress notification
		// is forwarded, instead of racing removal of the progress-token entry.
		if count, ok := result.Meta[progressCountMeta].(float64); ok && count > 0 {
			drainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			for forwarded.Load() < uint64(count) {
				select {
				case <-changed:
				case <-drainCtx.Done():
					return nil, errors.New("LaraEnv MCP progress stream ended before completion")
				}
			}
		}
		return result, nil
	})
	registerContextViews(proxy, func(ctx context.Context, project string) (string, error) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "laraenv_context", Arguments: Args{Action: "summary", Project: project}})
		return contextResultText(result, err)
	})
	return proxy, func() { _ = session.Close() }, nil
}

// RunProxy serves only MCP on stdin/stdout. The application must select this
// CLI mode before constructing NewApp or performing GUI startup work.
func RunProxy(ctx context.Context, descriptorPath, version string) error {
	proxy, closeProxy, err := newProxy(ctx, descriptorPath, version)
	if err != nil {
		return err
	}
	defer closeProxy()
	return proxy.Run(ctx, &mcp.StdioTransport{})
}
