//go:build windows

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"uika-resonance/internal/config"
)

const fixtureDashboardToken = "0123456789abcdef0123456789abcdef"

func TestEnsureDashboardConfigPreservesUserSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	cfg := config.Defaults()
	cfg.Discord.AppName = "fixture custom name"
	cfg.Buttons.YozoraURL = "https://example.test/custom"
	cfg.Sources.Blocked = []string{"browser", "fixture-player"}
	cfg.Presence.MinUpdateSecs = 20
	cfg.Server.Enabled = false
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	if err := ensureDashboardConfig(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Enabled = true
	if !reflect.DeepEqual(loaded, cfg) {
		t.Fatal("opening settings changed unrelated user preferences")
	}
}

func TestProbeDashboardDoesNotForwardTokenOnRedirect(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	if probeDashboard(context.Background(), dashboardEndpoint{BaseURL: redirect.URL, Token: fixtureDashboardToken, PID: 1}) {
		t.Fatal("redirect was accepted as healthy Yozora")
	}
	if forwarded.Load() != 0 {
		t.Fatal("session probe followed redirect and forwarded authentication")
	}
}

func TestDashboardEndpointRoundTripAndAuthenticatedProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sources" || r.Header.Get("X-Uika-Token") != fixtureDashboardToken || r.URL.RawQuery != "" {
			t.Error("probe did not use local header-authenticated source endpoint")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Yozora","enabled":{"applemusic":true,"spotify":true,"spotifyapi":true,"browser":false,"generic":false}}`))
	}))
	defer server.Close()
	endpoint := dashboardEndpoint{BaseURL: server.URL, Token: fixtureDashboardToken, PID: 1}
	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := writeDashboardEndpoint(path, endpoint); err != nil {
		t.Fatal(err)
	}
	got, err := readDashboardEndpoint(path)
	if err != nil || got != endpoint {
		t.Fatalf("endpoint roundtrip failed: error=%v", err)
	}
	if !probeDashboard(context.Background(), got) {
		t.Fatal("authenticated source-controls endpoint was not recognized")
	}
	ready, err := waitDashboard(context.Background(), path)
	if err != nil || ready != endpoint {
		t.Fatalf("ready endpoint was not discovered: error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitDashboard(ctx, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("canceled launch kept waiting for an endpoint")
	}
}

func TestDashboardURLRejectsUnsafeEndpoints(t *testing.T) {
	for _, base := range []string{
		"https://example.test", "http://example.test:8756", "http://localhost:8756",
		"http://127.0.0.1:8756/settings", "http://127.0.0.1:8756?t=wrong",
		"http://user@127.0.0.1:8756", "http://127.0.0.1:8756#fragment", "http://127.0.0.1",
	} {
		t.Run(base, func(t *testing.T) {
			if _, err := dashboardURL(dashboardEndpoint{BaseURL: base, Token: fixtureDashboardToken, PID: 1}); err == nil {
				t.Fatal("accepted unsafe settings endpoint")
			}
		})
	}
	endpoint := dashboardEndpoint{BaseURL: "http://127.0.0.1:8756", Token: fixtureDashboardToken, PID: 1}
	u, err := dashboardURL(endpoint)
	if err != nil || u.Host != "127.0.0.1:8756" || u.Query().Get("t") != fixtureDashboardToken {
		t.Fatalf("valid endpoint failed: error=%v", err)
	}
	endpoint.Token = "not-a-session-token"
	if _, err := dashboardURL(endpoint); err == nil {
		t.Fatal("accepted malformed session token")
	}
}
