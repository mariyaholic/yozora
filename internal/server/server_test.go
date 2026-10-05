//go:build windows

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"uika-resonance/internal/config"
)

func fixtureHandler(t *testing.T) (http.Handler, *config.Holder) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.toml")
	c := config.Defaults()
	c.Sources.Blocked = []string{" BrOwSeR ", "custom.app", "SPOTIFY"}
	c.Discord.Apps = map[string]string{"spotify": "synthetic-id"}
	c.Discord.Names = map[string]string{"spotify": "Fixture"}
	c.Template.State = "custom template"
	c.Spotify.Market = "fixture-market"
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	h, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	return Handler(Deps{Cfg: h, CfgPath: path, Token: "fixture-token"}), h
}
func request(h http.Handler, method, path, body string, header bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if header {
		r.Header.Set("X-Uika-Token", "fixture-token")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestSourcesRejectInvalidBodiesWithoutChangingConfig(t *testing.T) {
	h, cfg := fixtureHandler(t)
	before := cfg.Get()
	for _, body := range []string{`{}`, `null`, `{"enabled":null}`, `{"enabled":[]}`, `{"enabled":{"browser":null}}`, `{"enabled":{"browser":1}}`, `{"enabled":{"browser":"false"}}`, `{"enabled":{"youtube":false}}`, `{"enabled":{"Browser":false}}`, `{"enabled":{},"extra":true}`, `{"enabled":{}} {}`, `{"enabled":{}} trailing`, strings.Repeat(" ", 4097) + `{"enabled":{}}`} {
		w := request(h, "POST", "/api/sources", body, true)
		if w.Code != 400 && w.Code != 413 {
			t.Errorf("accepted %q: %d", body[:min(len(body), 80)], w.Code)
		}
		loaded, err := config.Load(cfg.Path())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded, before) || cfg.Get() != before {
			t.Fatal("rejected request changed config")
		}
	}
}
func TestSourcesAuthAndMethods(t *testing.T) {
	h, _ := fixtureHandler(t)
	for _, method := range []string{"GET", "POST"} {
		if w := request(h, method, "/api/sources", `{"enabled":{}}`, false); w.Code != 401 {
			t.Fatalf("unauthenticated %s=%d", method, w.Code)
		}
		for _, header := range []bool{false, true} {
			path := "/api/sources"
			if !header {
				path += "?t=fixture-token"
			}
			if w := request(h, method, path, `{"enabled":{}}`, header); w.Code != 200 {
				t.Fatalf("authenticated %s=%d", method, w.Code)
			}
		}
	}
	if w := request(h, "DELETE", "/api/sources", "", true); w.Code != 405 {
		t.Fatalf("DELETE=%d", w.Code)
	}
}

func TestConcurrentSourceUpdatesDoNotLoseWrites(t *testing.T) {
	h, cfg := fixtureHandler(t)
	var wg sync.WaitGroup
	for _, source := range []string{"applemusic", "spotifyapi", "generic"} {
		wg.Go(func() {
			w := request(h, "POST", "/api/sources", `{"enabled":{"`+source+`":false}}`, true)
			if w.Code != 200 {
				t.Errorf("concurrent save %s: %d", source, w.Code)
			}
		})
	}
	wg.Wait()
	loaded, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"applemusic", "spotifyapi", "generic"} {
		found := false
		for _, block := range loaded.Sources.Blocked {
			if block == source {
				found = true
			}
		}
		if !found {
			t.Errorf("lost update %s: %v", source, loaded.Sources.Blocked)
		}
	}
	if !reflect.DeepEqual(loaded, cfg.Get()) {
		t.Fatal("reload does not match saved config")
	}
}

func TestDashboardSourceControls(t *testing.T) {
	h, _ := fixtureHandler(t)
	w := request(h, "GET", "/?t=fixture-token", "", false)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	page := w.Body.String()
	for _, source := range []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"} {
		if !strings.Contains(page, `id="source-`+source+`"`) || !strings.Contains(page, `for="source-`+source+`"`) {
			t.Errorf("missing accessible checkbox for %s", source)
		}
	}
	for _, marker := range []string{`type="checkbox"`, `id="source-save"`, `role="status"`, `/api/sources`, `method:"POST"`, `JSON.stringify({enabled})`} {
		if !strings.Contains(page, marker) {
			t.Errorf("missing working control %s", marker)
		}
	}
}

func TestDashboardShowsAuthorFooter(t *testing.T) {
	h, _ := fixtureHandler(t)
	w := request(h, "GET", "/?t=fixture-token", "", false)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	page := w.Body.String()
	if !strings.Contains(page, "Made with love by") || !strings.Contains(page, `href="https://github.com/mariyaholic"`) {
		t.Fatal("dashboard is missing the author footer link")
	}
}

func TestSourcesPersistPartialChangesWithoutMutatingSnapshot(t *testing.T) {
	h, cfg := fixtureHandler(t)
	old := cfg.Get()
	w := request(h, "GET", "/api/sources?t=fixture-token", "", false)
	if w.Code != 200 {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Name    string
		Enabled map[string]bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Name != "Yozora" || len(response.Enabled) != 5 || response.Enabled["browser"] || response.Enabled["spotify"] || !response.Enabled["applemusic"] {
		t.Fatalf("state=%+v", response)
	}
	w = request(h, "POST", "/api/sources", `{"enabled":{"browser":true,"generic":false}}`, true)
	if w.Code != 200 {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	want := *old
	want.Sources.Blocked = []string{"custom.app", "SPOTIFY", "generic"}
	if !reflect.DeepEqual(loaded, &want) || !reflect.DeepEqual(cfg.Get(), &want) {
		t.Fatalf("lost unrelated settings: %+v", loaded)
	}
	if !reflect.DeepEqual(old.Sources.Blocked, []string{" BrOwSeR ", "custom.app", "SPOTIFY"}) {
		t.Fatal("published snapshot mutated")
	}
	restarted, err := config.NewHolder(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.Get(), &want) {
		t.Fatal("restart lost source choices")
	}
}
