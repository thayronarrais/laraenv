package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"lara4ever/internal/config"
)

type sessionRoundTrip func(*http.Request) (*http.Response, error)

func (f sessionRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func sessionResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func TestDelayedOldAccountResponseCannotChangeNewLogin(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, store := optionalClient(t, "https://old.invalid")
			setOptionalEnabled(t, client, true)
			started, release := make(chan struct{}), make(chan struct{})
			client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/v1/auth/login" {
					return sessionResponse(200, `{"token":"new-token","user":{"id":99,"email":"new@example.invalid","plan":"pro","kdf_salt":"`+strings.Repeat("ab", 32)+`"}}`), nil
				}
				close(started)
				<-release // Models a response already received when the account changes.
				return sessionResponse(status, `{"user":{"id":7,"email":"old@example.invalid"},"plan":"free"}`), nil
			})
			done := make(chan error, 1)
			go func() { _, err := client.Me(context.Background()); done <- err }()
			<-started
			if err := client.SetBaseURL("https://new.invalid"); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Login(context.Background(), "new@example.invalid", "password"); err != nil {
				t.Fatal(err)
			}
			before := store.Get().Cloud
			close(release)
			if err := <-done; err == nil {
				t.Error("stale account response was accepted")
			}
			if got := store.Get().Cloud; got != before {
				t.Fatalf("stale response changed new session: before=%+v after=%+v", before, got)
			}
			if before.SyncKey == "" {
				t.Fatal("new login did not prime its own sync key")
			}
		})
	}
}

func capturedSession(t *testing.T, client *Client) (context.Context, context.CancelFunc, config.Cloud) {
	t.Helper()
	starter, ok := any(client).(interface {
		BeginSession(context.Context) (context.Context, context.CancelFunc, config.Cloud, error)
	})
	if !ok {
		t.Fatal("cloud must provide an immutable session capture")
	}
	ctx, cancel, snapshot, err := starter.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ctx, cancel, snapshot
}

func TestCapturedCloudSessionCannotFollowIdentitySwitch(t *testing.T) {
	for _, field := range []string{"server", "token", "account", "key", "logout", "disable"} {
		t.Run(field, func(t *testing.T) {
			client, store := optionalClient(t, "https://old.invalid")
			setOptionalEnabled(t, client, true)
			ctx, cancel, snapshot := capturedSession(t, client)
			defer cancel()
			var requests atomic.Int32
			client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				return sessionResponse(200, `{}`), nil
			})
			changed := store.Get().Cloud
			switch field {
			case "server":
				changed.BaseURL = "https://new.invalid"
			case "token":
				changed.Token = "new-token"
			case "account":
				changed.UserID = 99
			case "key":
				changed.SyncKey = EncodeKey([]byte(strings.Repeat("x", 32)))
			case "logout":
				if err := client.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
				changed = store.Get().Cloud
				requests.Store(0)
			case "disable":
				setOptionalEnabled(t, client, false)
				setOptionalEnabled(t, client, true)
				changed = store.Get().Cloud
			}
			if err := store.SetCloud(changed); err != nil {
				t.Fatal(err)
			}
			for name, call := range map[string]func() error{
				"store run": func() error { return client.StoreDeploymentRun(ctx, 7, "deploy", "success", "", "", "", "") },
				"delete":    func() error { return client.DeleteDeployment(ctx, 7) },
				"sync upload": func() error {
					return client.PutResourceJSON(ctx, KindSSHHost, "host", map[string]string{"password": "secret"}, 0)
				},
			} {
				if err := call(); err == nil {
					t.Errorf("%s used a stale session", name)
				}
			}
			if requests.Load() != 0 {
				t.Fatalf("queued old work sent %d requests under changed identity", requests.Load())
			}
			if snapshot.Token != "saved-token" || snapshot.BaseURL != "https://old.invalid" || snapshot.UserID != 7 {
				t.Fatalf("capture changed: %+v", snapshot)
			}
		})
	}
}

func TestCapturedSyncKeyAndRequestIdentityAreUsedTogether(t *testing.T) {
	client, _ := optionalClient(t, "https://old.invalid")
	setOptionalEnabled(t, client, true)
	ctx, cancel, snapshot := capturedSession(t, client)
	defer cancel()
	client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "old.invalid" || req.Header.Get("Authorization") != "Bearer saved-token" {
			t.Errorf("mixed request identity: %s %q", req.URL, req.Header.Get("Authorization"))
		}
		var resource remoteResource
		if err := json.NewDecoder(req.Body).Decode(&resource); err != nil {
			t.Fatal(err)
		}
		blob, err := base64.StdEncoding.DecodeString(resource.Ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := DecodeKey(snapshot.SyncKey)
		plain, err := Decrypt(key, blob)
		if err != nil || string(plain) != `{"password":"secret"}` {
			t.Fatalf("payload was not encrypted with the captured key: %s %v", plain, err)
		}
		return sessionResponse(200, `{}`), nil
	})
	if snapshot.SyncKey != EncodeKey(make([]byte, 32)) {
		t.Fatal("wrong captured key")
	}
	if err := client.PutResourceJSON(ctx, KindSSHHost, "host", map[string]string{"password": "secret"}, 0); err != nil {
		t.Fatal(err)
	}
}

func TestNewCloudAccountDoesNotInheritSyncOptIn(t *testing.T) {
	client, store := optionalClient(t, "https://old.invalid")
	setOptionalEnabled(t, client, true)
	client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
		return sessionResponse(200, `{"token":"new-token","user":{"id":99,"email":"new@example.invalid","plan":"pro"}}`), nil
	})
	if _, err := client.Login(context.Background(), "new@example.invalid", "password"); err != nil {
		t.Fatal(err)
	}
	if store.Get().Cloud.SyncEnabled {
		t.Fatal("new cloud account inherited the prior account's automatic sync opt-in")
	}
}

func TestSyncFailureIsCachedWithoutStatusHTTP(t *testing.T) {
	for _, operation := range []string{"upload", "delete", "pull"} {
		t.Run(operation, func(t *testing.T) {
			client, _ := optionalClient(t, "https://old.invalid")
			setOptionalEnabled(t, client, true)
			var requests atomic.Int32
			failing := true
			client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				if failing {
					return sessionResponse(503, `{"message":"temporarily unavailable"}`), nil
				}
				return sessionResponse(200, `{"resources":[]}`), nil
			})
			call := func() error {
				switch operation {
				case "upload":
					return client.PutResourceWithRetry(context.Background(), KindSSHHost, "host", []byte(`{}`))
				case "delete":
					return client.DeleteResource(context.Background(), KindSSHHost, "host")
				default:
					_, err := client.PullResources(context.Background(), "")
					return err
				}
			}
			if err := call(); err == nil {
				t.Fatal("fixture sync failure was accepted")
			}
			failed := client.SyncStatusSnapshot()
			if failed.LastError == "" || failed.Reachable {
				t.Fatalf("sync failure was not cached: %+v", failed)
			}
			setOptionalEnabled(t, client, false)
			if got := client.SyncStatusSnapshot(); got.LastError != failed.LastError || got.Enabled {
				t.Fatalf("disable discarded the cached failure: %+v", got)
			}
			if requests.Load() != 1 {
				t.Fatal("status read performed HTTP")
			}
			setOptionalEnabled(t, client, true)
			failing = false
			if err := call(); err != nil {
				t.Fatal(err)
			}
			if got := client.SyncStatusSnapshot(); got.LastError != "" || !got.Reachable {
				t.Fatalf("successful retry did not update cached status: %+v", got)
			}
		})
	}
}

func TestLogoutSucceedsWhenServerAlreadyRejectedToken(t *testing.T) {
	client, store := optionalClient(t, "https://old.invalid")
	setOptionalEnabled(t, client, true)
	client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) { return sessionResponse(401, `{}`), nil })
	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("best-effort logout failed: %v", err)
	}
	if got := store.Get().Cloud; got.Token != "" || got.UserID != 0 || got.SyncKey != "" || got.SyncEnabled {
		t.Fatalf("logout retained session: %+v", got)
	}
}

func TestSuccessfulAuthenticationRotatesExistingLifetime(t *testing.T) {
	for _, method := range []string{"login", "register"} {
		t.Run(method, func(t *testing.T) {
			client, _ := optionalClient(t, "https://old.invalid")
			setOptionalEnabled(t, client, true)
			client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
				return sessionResponse(200, `{"token":"same-token","user":{"id":99,"email":"same@example.invalid","kdf_salt":"`+strings.Repeat("ab", 32)+`"}}`), nil
			})
			authenticate := func() error {
				if method == "register" {
					_, err := client.Register(context.Background(), "Same", "same@example.invalid", "password")
					return err
				}
				_, err := client.Login(context.Background(), "same@example.invalid", "password")
				return err
			}
			if err := authenticate(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel, _ := capturedSession(t, client)
			defer cancel()
			if err := authenticate(); err != nil {
				t.Fatal(err)
			}
			if ctx.Err() == nil {
				t.Fatal("successful authentication retained the prior lifetime")
			}
		})
	}
}

type identitySwitchingBody struct{ change func() error }

func (body identitySwitchingBody) MarshalJSON() ([]byte, error) { return []byte(`{}`), body.change() }

func TestRequestCannotAdoptIdentityChangedWhilePreparingBody(t *testing.T) {
	client, _ := optionalClient(t, "https://old.invalid")
	setOptionalEnabled(t, client, true)
	var requests atomic.Int32
	client.http.Transport = sessionRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return sessionResponse(200, `{}`), nil
	})
	err := client.post(context.Background(), "/api/v1/deployments", identitySwitchingBody{change: func() error { return client.SetBaseURL("https://new.invalid") }}, nil)
	if err == nil || requests.Load() != 0 {
		t.Fatalf("request adopted the changed endpoint or token: err=%v requests=%d", err, requests.Load())
	}
}
