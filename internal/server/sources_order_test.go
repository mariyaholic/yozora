//go:build windows

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"uika-resonance/internal/config"
)

func TestSourcesOrderRoundTrip(t *testing.T) {
	h, cfg := fixtureHandlerForOrderTest(t)

	w := request(h, "GET", "/api/sources?t=fixture-token", "", false)
	if w.Code != 200 {
		t.Fatalf("GET status=%d", w.Code)
	}
	var got struct {
		Name    string          `json:"name"`
		Enabled map[string]bool `json:"enabled"`
		Order   []string        `json:"order"`
	}
	if err := jsonUnmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Order == nil || !reflect.DeepEqual(got.Order, canonicalSources) {
		t.Fatalf("default order = %v, want canonical list", got.Order)
	}
	blockedCfg, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	blockedBefore := blockedCfg.Sources.Blocked

	newOrder := []string{"browser", "spotify", "applemusic", "spotifyapi", "youtubemusic", "generic"}
	w = request(h, "POST", "/api/sources", `{"order":["browser","spotify","applemusic","spotifyapi","youtubemusic","generic"]}`, true)
	if w.Code != 200 {
		t.Fatalf("POST order status=%d body=%s", w.Code, w.Body.String())
	}
	if err := jsonUnmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Order, newOrder) {
		t.Fatalf("echoed order = %v, want %v", got.Order, newOrder)
	}
	loaded, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Sources.Order, newOrder) {
		t.Fatalf("saved order = %v, want %v", loaded.Sources.Order, newOrder)
	}

	w = request(h, "POST", "/api/sources", `{"order":["browser","spotify","applemusic","spotifyapi","youtubemusic","generic"]}`, true)
	if w.Code != 200 {
		t.Fatalf("POST order status=%d body=%s", w.Code, w.Body.String())
	}
	loadedAfterOrder, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedAfterOrder.Sources.Blocked, blockedBefore) {
		t.Fatalf("order-only POST changed blocked: %v", loadedAfterOrder.Sources.Blocked)
	}
	w = request(h, "POST", "/api/sources", `{"enabled":{"browser":false}}`, true)
	if w.Code != 200 {
		t.Fatalf("POST enabled status=%d body=%s", w.Code, w.Body.String())
	}
	loadedAfterEnabled, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedAfterEnabled.Sources.Order, newOrder) {
		t.Fatalf("enabled-only POST changed order: %v", loadedAfterEnabled.Sources.Order)
	}
	if !reflect.DeepEqual(loadedAfterEnabled.Sources.Blocked, []string{"browser"}) {
		t.Fatalf("enabled toggle not applied: %v", loadedAfterEnabled.Sources.Blocked)
	}
}

func TestSourcesOrderRejectsInvalidLists(t *testing.T) {
	h, cfg := fixtureHandlerForOrderTest(t)
	before, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"order":[]}`,
		`{"order":["browser","browser"]}`,
		`{"order":["browser","spotify","bogus"]}`,
		`{"order":"browser"}`,
		`{"order":["BROWSER","spotify","applemusic","spotifyapi","youtubemusic","generic"]}`,
		`{"order":null}`,
		`{"order":["applemusic","spotify","spotifyapi","youtubemusic","browser","youtubemusic"]}`,
		`{"order":["applemusic","spotify","spotifyapi","youtubemusic","browser","bogus"]}`,
	} {
		w := request(h, "POST", "/api/sources", body, true)
		if w.Code != 400 {
			t.Errorf("accepted %q: %d", body, w.Code)
			continue
		}
		loaded, err := config.Load(cfg.Path())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded.Sources.Order, before.Sources.Order) {
			t.Fatalf("rejected request changed saved order: %v -> %v", before.Sources.Order, loaded.Sources.Order)
		}
	}
	fresh, _ := config.Load(cfg.Path())
	if !reflect.DeepEqual(fresh.Sources.Order, before.Sources.Order) {
		t.Fatal("holder mutated by rejected request")
	}
}

func fixtureHandlerForOrderTest(t *testing.T) (http.Handler, *config.Holder) {
	t.Helper()
	path := t.TempDir() + "/order.toml"
	c := config.Defaults()
	c.Sources.Blocked = nil
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	h, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	return Handler(Deps{Cfg: h, CfgPath: path, Token: "fixture-token"}), h
}

func TestYouTubeMusicToggleAndReservedOrder(t *testing.T) {
	h, cfg := fixtureHandlerForOrderTest(t)
	w := request(h, "POST", "/api/sources", `{"enabled":{"youtubemusic":false}}`, true)
	if w.Code != 200 {
		t.Fatalf("music toggle: %d %s", w.Code, w.Body.String())
	}
	loaded, err := config.Load(cfg.Path())
	if err != nil || !reflect.DeepEqual(loaded.Sources.Blocked, []string{"youtubemusic"}) {
		t.Fatalf("saved block: %v %v", loaded, err)
	}
	w = request(h, "POST", "/api/sources", `{"enabled":{"youtubemusic":true}}`, true)
	if w.Code != 200 {
		t.Fatalf("enable: %d", w.Code)
	}
	for _, key := range []string{"youtube", "youtube_tabs"} {
		w = request(h, "POST", "/api/sources", `{"order":["applemusic","spotify","spotifyapi","youtubemusic","browser","generic","`+key+`"]}`, true)
		if w.Code != 400 {
			t.Errorf("accepted %s in order", key)
		}
		w = request(h, "POST", "/api/sources", `{"order":["applemusic","spotify","spotifyapi","youtubemusic","browser","`+key+`"]}`, true)
		if w.Code != 400 {
			t.Errorf("accepted %s in place of a canonical source", key)
		}
	}
}

func TestMigratedSourceOrderStillValid(t *testing.T) {
	for _, known := range []string{
		`["applemusic","spotify","spotifyapi","browser","generic"]`,
		`["applemusic","spotify","spotifyapi","youtubemusic","youtubemusic","browser","generic"]`,
	} {
		path := t.TempDir() + "/legacy.toml"
		fixture := "[sources]\norder = [\"generic\",\"spotifyapi\",\"browser\",\"spotify\",\"applemusic\"]\nblocked = [\"browser\",\"browser\"]\nknown = " + known + "\n"
		if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSourceOrder(c.Sources.Order, canonicalSources); err != nil {
			t.Fatalf("migrated order %v: %v", c.Sources.Order, err)
		}
	}
}

func TestLegacySourceOrderDashboardApply(t *testing.T) {
	path := t.TempDir() + "/legacy.toml"
	fixture := `[sources]
order = ["generic", "spotifyapi", "browser", "spotify", "applemusic", "spotify", "custom"]
blocked = []
`
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Deps{Cfg: cfg, CfgPath: path, Token: "fixture-token"})
	w := request(h, "GET", "/api/sources?t=fixture-token", "", false)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var loaded struct {
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceOrder(loaded.Order, canonicalSources); err != nil {
		t.Fatalf("loaded order %v: %v", loaded.Order, err)
	}
	body, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", "/api/sources", string(body), true)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard Apply status=%d body=%s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(cfg.Get().Sources.Order, loaded.Order) {
		t.Fatalf("saved order = %v, want %v", cfg.Get().Sources.Order, loaded.Order)
	}
}
