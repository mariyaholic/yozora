//go:build windows

package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
)

func cardBody(t *testing.T, edit func(m map[string]any)) string {
	t.Helper()
	m := map[string]any{"paused_mode": "hide", "type": "watching", "status_display": "details", "buttons_enabled": false,
		"button_label": "Hear it", "spotify_search_fallback": false, "details": "{{.Title}} by {{.Artist}}", "state": "{{upper .Album}}",
		"large_text": "{{.Player}}", "small_text": "", "paused_details": "Paused: {{.Title}}"}
	if edit != nil {
		edit(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCardRoundTripReachesConfigAndHolder(t *testing.T) {
	h, holder := fixtureHandler(t)
	w := request(h, "GET", "/api/card", "", true)
	var got struct {
		Card, Defaults cardSettings
		Hidden         []player.HiddenRule
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("GET = %d %s", w.Code, w.Body)
	}
	if got.Card.State != "custom template" || got.Defaults.Details != "{{.Title}}" || got.Defaults.PausedMode != "show" || got.Hidden == nil {
		t.Fatalf("GET body = %+v", got)
	}
	w = request(h, "POST", "/api/card", cardBody(t, nil), true)
	if w.Code != 200 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	for name, cfg := range map[string]*config.Config{"holder": holder.Get(), "disk": mustLoad(t, holder.Path())} {
		if cfg.Presence.PausedMode != "hide" || cfg.Presence.Type != "watching" || cfg.Presence.StatusDisplay != "details" ||
			cfg.Buttons.Enabled || cfg.Buttons.Label != "Hear it" || cfg.Buttons.SpotifySearch ||
			cfg.Template.Details != "{{.Title}} by {{.Artist}}" || cfg.Template.State != "{{upper .Album}}" ||
			cfg.Template.LargeText != "{{.Player}}" || cfg.Template.SmallText != "" || cfg.Template.Paused != "Paused: {{.Title}}" {
			t.Fatalf("%s did not take the card: %+v %+v %+v", name, cfg.Presence, cfg.Buttons, cfg.Template)
		}
		// Settings the card form does not own survive the save.
		if cfg.Discord.Apps["spotify"] != "synthetic-id" || cfg.Spotify.Market != "fixture-market" || len(cfg.Sources.Blocked) != 3 {
			t.Fatalf("%s lost unrelated settings", name)
		}
	}
}

func mustLoad(t *testing.T, path string) *config.Config {
	t.Helper()
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCardRejectsInvalidBodiesWithoutChangingConfig(t *testing.T) {
	h, holder := fixtureHandler(t)
	before := holder.Get()
	onDisk := mustLoad(t, holder.Path())
	long := strings.Repeat("x", config.MaxTemplate+1)
	bodies := map[string]string{
		"empty object": `{}`, "null": `null`, "array": `[]`, "trailing": cardBody(t, nil) + ` {}`, "garbage": cardBody(t, nil) + `x`,
		"unknown field":             cardBody(t, func(m map[string]any) { m["extra"] = 1 }),
		"missing field":             cardBody(t, func(m map[string]any) { delete(m, "small_text") }),
		"null field":                cardBody(t, func(m map[string]any) { m["state"] = nil }),
		"wrong type":                cardBody(t, func(m map[string]any) { m["buttons_enabled"] = "yes" }),
		"bad paused":                cardBody(t, func(m map[string]any) { m["paused_mode"] = "Show" }),
		"bad type":                  cardBody(t, func(m map[string]any) { m["type"] = "competing" }),
		"bad display":               cardBody(t, func(m map[string]any) { m["status_display"] = "title" }),
		"long label":                cardBody(t, func(m map[string]any) { m["button_label"] = strings.Repeat("l", 33) }),
		"long template":             cardBody(t, func(m map[string]any) { m["details"] = long }),
		"control char":              cardBody(t, func(m map[string]any) { m["large_text"] = "a\x00b" }),
		"newline":                   cardBody(t, func(m map[string]any) { m["button_label"] = "a\nb" }),
		"parse error":               cardBody(t, func(m map[string]any) { m["details"] = "{{.Title" }),
		"unknown field in template": cardBody(t, func(m map[string]any) { m["state"] = "{{.Nope}}" }),
		"unknown func":              cardBody(t, func(m map[string]any) { m["small_text"] = "{{shout .Title}}" }),
		"too large":                 cardBody(t, func(m map[string]any) { m["details"] = strings.Repeat(" ", 9000) }),
	}
	for name, body := range bodies {
		// One bad field among good ones must not save the good ones.
		if w := request(h, "POST", "/api/card", body, true); w.Code != 400 && w.Code != 413 {
			t.Errorf("%s: accepted with %d", name, w.Code)
		}
		if holder.Get() != before || !reflect.DeepEqual(mustLoad(t, holder.Path()), onDisk) {
			t.Fatalf("%s: rejected request changed config", name)
		}
	}
}

func TestCardAuthAndMethods(t *testing.T) {
	h, _ := fixtureHandler(t)
	for _, path := range []string{"/api/card", "/api/card/preview", "/api/hidden"} {
		if w := request(h, "POST", path, cardBody(t, nil), false); w.Code != 401 {
			t.Errorf("unauthenticated POST %s = %d", path, w.Code)
		}
		if w := request(h, "DELETE", path, "", true); w.Code != 405 {
			t.Errorf("DELETE %s = %d", path, w.Code)
		}
	}
	if w := request(h, "GET", "/api/card/preview", "", true); w.Code != 405 {
		t.Errorf("GET preview = %d", w.Code)
	}
}

func TestCardPreviewRendersCandidateWithoutSaving(t *testing.T) {
	h, holder := fixtureHandler(t)
	before := holder.Get()
	onDisk := mustLoad(t, holder.Path())
	w := request(h, "POST", "/api/card/preview", cardBody(t, func(m map[string]any) {
		m["details"] = `{{lower .Title}} / {{.ElapsedFmt}} of {{.DurationFmt}}`
		m["state"] = `{{bar 4 "#" "-"}}`
	}), true)
	var got cardPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	if got.Details != "night walk / 1:22 of 4:03" || got.State != "#---" || got.Large != "Spotify" || got.Small != "" || !got.Sample || !got.Playing {
		t.Fatalf("preview = %+v", got)
	}
	if bad := request(h, "POST", "/api/card/preview", cardBody(t, func(m map[string]any) { m["details"] = "{{" }), true); bad.Code != 400 {
		t.Fatalf("invalid preview = %d", bad.Code)
	}
	if holder.Get() != before || !reflect.DeepEqual(mustLoad(t, holder.Path()), onDisk) {
		t.Fatal("preview changed config")
	}
}

func TestHiddenRulesAddRemoveAndLimits(t *testing.T) {
	h, holder := fixtureHandler(t)
	post := func(body string) (int, []player.HiddenRule) {
		w := request(h, "POST", "/api/hidden", body, true)
		var out struct{ Hidden []player.HiddenRule }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Hidden
	}
	if code, list := post(`{"add":{"artist":"  Mira ","title":""}}`); code != 200 || !reflect.DeepEqual(list, []player.HiddenRule{{Artist: "Mira"}}) {
		t.Fatalf("add = %d %+v", code, list)
	}
	if code, list := post(`{"add":{"artist":"mira"}}`); code != 200 || len(list) != 1 {
		t.Fatalf("duplicate add = %d %+v", code, list)
	}
	if code, list := post(`{"add":{"artist":"Mira","title":"Night *"}}`); code != 200 || len(list) != 2 {
		t.Fatalf("second add = %d %+v", code, list)
	}
	if got := mustLoad(t, holder.Path()).Sources.Hidden; len(got) != 2 || len(holder.Get().Sources.Hidden) != 2 {
		t.Fatalf("not saved: %+v", got)
	}
	if code, list := post(`{"remove":{"artist":" MIRA "}}`); code != 200 || !reflect.DeepEqual(list, []player.HiddenRule{{Artist: "Mira", Title: "Night *"}}) {
		t.Fatalf("remove = %d %+v", code, list)
	}
	if code, list := post(`{"remove":{"artist":"Mira","title":"night *"}}`); code != 200 || len(list) != 0 || mustLoad(t, holder.Path()).Sources.Hidden != nil {
		t.Fatalf("remove last = %d %+v", code, list)
	}
	if w := request(h, "GET", "/api/hidden", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"hidden":[]`) {
		t.Fatalf("empty GET = %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{}`, `{"add":{}}`, `{"add":{"artist":"*"}}`, `{"add":{"title":"` + strings.Repeat("a", config.MaxHiddenField+1) + `"}}`,
		`{"add":{"artist":"a"},"remove":{"artist":"a"}}`, `{"add":{"artist":"a"},"extra":1}`, `{"add":{"artist":"a","genre":"x"}}`, `{"add":{"artist":"a"}} {}`, `[]`, `null`} {
		if code, _ := post(bad); code != 400 {
			t.Errorf("accepted %q: %d", bad, code)
		}
	}
	for i := 0; i < config.MaxHiddenRules; i++ {
		if code, _ := post(fmt.Sprintf(`{"add":{"title":"t%d"}}`, i)); code != 200 {
			t.Fatalf("rule %d refused: %d", i, code)
		}
	}
	if code, _ := post(`{"add":{"title":"one too many"}}`); code != 400 {
		t.Fatalf("rule past the limit accepted: %d", code)
	}
	if n := len(mustLoad(t, holder.Path()).Sources.Hidden); n != config.MaxHiddenRules {
		t.Fatalf("%d rules saved", n)
	}
	if code, _ := post(`{"add":{"title":"t0"}}`); code != 200 {
		t.Fatal("re-adding an existing rule at the limit should be a no-op")
	}
}
