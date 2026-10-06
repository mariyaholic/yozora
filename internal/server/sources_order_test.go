//go:build windows

package server

import (
	"net/http"
	"reflect"
	"testing"

	"uika-resonance/internal/config"
)

// Maria's hierarchy request: the dashboard must expose the saved source
// priority (drag order) just like the on/off switches, with identical
// strictness. Default saves the canonical order explicitly so future
// reshuffles of Defaults() never silently rewrite a user's list.
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

	// Reorder: browser to the front, then save.
	newOrder := []string{"browser", "spotify", "applemusic", "spotifyapi", "generic"}
	w = request(h, "POST", "/api/sources", `{"order":["browser","spotify","applemusic","spotifyapi","generic"]}`, true)
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
}

func TestSourcesOrderRejectsInvalidLists(t *testing.T) {
	h, cfg := fixtureHandlerForOrderTest(t)
	before, err := config.Load(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"order":[]}`,
		`{"order":["browser","browser"]}`,         // duplicates
		`{"order":["browser","spotify","bogus"]}`, // unknown source
		`{"order":"browser"}`,                     // wrong type
		`{"order":["BROWSER","spotify","applemusic","spotifyapi","generic"]}`, // canonical spelling required
		`{"order":null}`,
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

// fixtureHandlerForOrderTest mirrors fixtureHandler but with every source
// enabled so order assertions are independent of block state.
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
