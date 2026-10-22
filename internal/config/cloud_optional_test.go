package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Removing the opt-in default or restoring the endpoint migration would
// silently reconnect an existing profile and discard its stored credentials.
func TestLegacyCloudProfileRemainsDisabledAndPreserved(t *testing.T) {
	for _, endpoint := range []string{"http://laraenv-server.test", "", "https://saved.invalid"} {
		t.Run(endpoint, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			cloud := map[string]any{"baseUrl": endpoint, "token": "saved-token", "syncKey": "saved-key", "syncEnabled": true, "userId": 7, "email": "saved@example.invalid", "name": "Saved", "githubId": "123", "plan": "pro", "kdfSalt": "saved-salt"}
			raw, _ := json.Marshal(map[string]any{"cloud": cloud})
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			gotRaw, _ := json.Marshal(store.Get().Cloud)
			var got map[string]any
			_ = json.Unmarshal(gotRaw, &got)
			if got["enabled"] != false {
				t.Fatalf("legacy cloud must explicitly default disabled: %s", gotRaw)
			}
			for key, want := range cloud {
				if key == "userId" {
					want = float64(7)
				}
				if got[key] != want {
					t.Errorf("%s = %#v, want %#v", key, got[key], want)
				}
			}
		})
	}
}

func TestFailedCloudWritePreservesSavedProfile(t *testing.T) {
	store := newTestStore(t)
	before := store.Get().Cloud
	before.Token, before.SyncKey = "saved-token", "saved-key"
	if err := store.SetCloud(before); err != nil {
		t.Fatal(err)
	}
	store.path = t.TempDir() // a directory cannot be replaced by the config file
	next := before
	next.Enabled, next.Token, next.SyncKey = true, "replacement", "replacement"
	if err := store.SetCloud(next); err == nil {
		t.Fatal("write to a directory unexpectedly succeeded")
	}
	if got := store.Get().Cloud; !reflect.DeepEqual(got, before) {
		t.Fatalf("failed write changed cloud profile: %+v", got)
	}
}
