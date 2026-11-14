//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
)

func settingsRequest(h http.Handler, method, path, body, tag string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Uika-Token", "fixture-token")
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func settingsDocument(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	w := settingsRequest(h, "GET", "/api/settings", "", "")
	var doc map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &doc) != nil {
		t.Fatalf("export: %d", w.Code)
	}
	return doc
}
func settingsBody(t *testing.T, doc map[string]any) string {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func settingsPreview(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := settingsRequest(h, "POST", "/api/settings/preview", body, "")
	if w.Code != 200 || w.Header().Get("ETag") == "" {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	return w
}

func TestSettingsRoundTripPreservesExcludedFields(t *testing.T) {
	h, holder := fixtureHandler(t)
	cfg := mustLoad(t, holder.Path())
	cfg.Discord.ClientID = "private-client"
	cfg.Spotify.ClientID = "private-spotify"
	cfg.Sources.Hidden = []player.HiddenRule{{Artist: "Private artist"}}
	cfg.Server.Port = 3456
	cfg.UI.Onboarded = true
	cfg.Buttons.YozoraURL = "https://example.invalid/custom"
	if err := config.Save(cfg, holder.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Reload(); err != nil {
		t.Fatal(err)
	}

	export := settingsRequest(h, "GET", "/api/settings", "", "")
	if export.Code != 200 || !strings.Contains(export.Header().Get("Content-Disposition"), "attachment") || export.Header().Get("ETag") == "" {
		t.Fatal("export headers")
	}
	for _, secret := range []string{"private-client", "private-spotify", "Private artist", "custom.app", "3456", "example.invalid"} {
		if strings.Contains(export.Body.String(), secret) {
			t.Fatal("export contained excluded value")
		}
	}
	doc := settingsDocument(t, h)
	doc["card"].(map[string]any)["details"] = "{{.Artist}}"
	doc["sources"].(map[string]any)["enabled"].(map[string]any)["spotify"] = true
	doc["sources"].(map[string]any)["order"] = []string{"generic", "browser", "spotify", "spotifyapi", "applemusic", "youtubemusic"}
	doc["appearance"].(map[string]any)["theme"] = "uika"
	body := settingsBody(t, doc)
	before, err := os.ReadFile(holder.Path())
	if err != nil {
		t.Fatal(err)
	}
	beforeHolder := holder.Get()
	preview := settingsPreview(t, h, body)
	var p struct {
		Candidate portableSettings `json:"candidate"`
		Summary   map[string]bool  `json:"summary"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil || !p.Summary["card_changed"] || !p.Summary["sources_changed"] || !p.Summary["appearance_changed"] {
		t.Fatal("preview summary")
	}
	afterPreview, err := os.ReadFile(holder.Path())
	if err != nil || !reflect.DeepEqual(before, afterPreview) || holder.Get() != beforeHolder {
		t.Fatal("preview mutated settings")
	}
	imported := settingsRequest(h, "POST", "/api/settings/import", body, preview.Header().Get("ETag"))
	if imported.Code != 200 {
		t.Fatalf("import: %d %s", imported.Code, imported.Body.String())
	}
	var got portableSettings
	if err := json.Unmarshal(imported.Body.Bytes(), &got); err != nil || !reflect.DeepEqual(got, p.Candidate) {
		t.Fatal("import differed from candidate")
	}
	for _, current := range []*config.Config{mustLoad(t, holder.Path()), holder.Get()} {
		if current.Discord.ClientID != "private-client" || current.Discord.Apps["spotify"] != "synthetic-id" || current.Spotify.ClientID != "private-spotify" ||
			current.Server.Port != 3456 || !current.UI.Onboarded || current.Buttons.YozoraURL != "https://example.invalid/custom" ||
			!reflect.DeepEqual(current.Sources.Hidden, cfg.Sources.Hidden) || !slices.Contains(current.Sources.Blocked, "custom.app") {
			t.Fatal("excluded setting changed")
		}
		if current.Template.Details != "{{.Artist}}" || current.UI.Theme != "uika" || player.IsBlocked(player.Track{Source: "spotify"}, current.Sources.Blocked) {
			t.Fatal("portable setting not applied")
		}
	}
	if settingsRequest(h, "GET", "/api/settings", "", "").Header().Get("ETag") != imported.Header().Get("ETag") {
		t.Fatal("updated tag differs")
	}
}

func TestSettingsRejectInvalidDocuments(t *testing.T) {
	h, holder := fixtureHandler(t)
	original := settingsDocument(t, h)
	valid := settingsBody(t, original)
	modified := func(edit func(map[string]any)) string {
		var copy map[string]any
		if err := json.Unmarshal([]byte(valid), &copy); err != nil {
			t.Fatal(err)
		}
		edit(copy)
		return settingsBody(t, copy)
	}
	cases := map[string]string{
		"null": "null", "array": "[]", "trailing": valid + " {}", "malformed": "{",
		"oversized":          strings.Repeat(" ", settingsMaxBytes+1),
		"version":            modified(func(m map[string]any) { m["version"] = 2 }),
		"format":             modified(func(m map[string]any) { m["format"] = "other" }),
		"unknown":            modified(func(m map[string]any) { m["secret"] = "x" }),
		"missing":            modified(func(m map[string]any) { delete(m, "card") }),
		"null appearance":    modified(func(m map[string]any) { m["appearance"] = nil }),
		"missing card field": modified(func(m map[string]any) { delete(m["card"].(map[string]any), "details") }),
		"card case": modified(func(m map[string]any) {
			c := m["card"].(map[string]any)
			c["Details"] = c["details"]
			delete(c, "details")
		}),
		"invalid template": modified(func(m map[string]any) { m["card"].(map[string]any)["details"] = "{{.Missing}}" }),
		"null switch":      modified(func(m map[string]any) { m["sources"].(map[string]any)["enabled"].(map[string]any)["spotify"] = nil }),
		"missing switch":   modified(func(m map[string]any) { delete(m["sources"].(map[string]any)["enabled"].(map[string]any), "spotify") }),
		"extra switch":     modified(func(m map[string]any) { m["sources"].(map[string]any)["enabled"].(map[string]any)["other"] = true }),
		"duplicate order": modified(func(m map[string]any) {
			m["sources"].(map[string]any)["order"] = []string{"spotify", "spotify", "browser", "generic", "applemusic", "youtubemusic"}
		}),
		"invalid theme": modified(func(m map[string]any) { m["appearance"].(map[string]any)["theme"] = "unknown" }),
		"duplicate":     strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
	}
	before := holder.Get()
	disk := mustLoad(t, holder.Path())
	for name, body := range cases {
		for _, path := range []string{"/api/settings/preview", "/api/settings/import"} {
			w := settingsRequest(h, "POST", path, body, `"fake"`)
			if w.Code != 400 && w.Code != 413 {
				t.Errorf("%s %s: %d", name, path, w.Code)
			}
		}
		if holder.Get() != before || !reflect.DeepEqual(mustLoad(t, holder.Path()), disk) {
			t.Fatalf("%s changed config", name)
		}
	}
}

func TestSettingsPreviewNormalizesCardAliases(t *testing.T) {
	h, _ := fixtureHandler(t)
	doc := settingsDocument(t, h)
	card := doc["card"].(map[string]any)
	card["paused_mode"], card["status_display"] = "clear", "name"
	body := settingsBody(t, doc)
	preview := settingsPreview(t, h, body)
	var p struct {
		Candidate portableSettings `json:"candidate"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil || p.Candidate.Card.PausedMode != "hide" || p.Candidate.Card.StatusDisplay != "app" {
		t.Fatal("aliases not normalized in preview")
	}
	imported := settingsRequest(h, "POST", "/api/settings/import", body, preview.Header().Get("ETag"))
	var got portableSettings
	if imported.Code != 200 || json.Unmarshal(imported.Body.Bytes(), &got) != nil || !reflect.DeepEqual(got, p.Candidate) {
		t.Fatal("import does not match preview")
	}
}

func TestSettingsConflictAndConcurrentUnrelatedChanges(t *testing.T) {
	h, holder := fixtureHandler(t)
	body := settingsBody(t, settingsDocument(t, h))
	preview := settingsPreview(t, h, body)
	if w := settingsRequest(h, "POST", "/api/settings/import", body, ""); w.Code != 428 {
		t.Fatalf("missing tag: %d", w.Code)
	}
	for _, tag := range []string{"*", "W/" + preview.Header().Get("ETag"), `"wrong"`} {
		if w := settingsRequest(h, "POST", "/api/settings/import", body, tag); w.Code != 412 {
			t.Fatalf("invalid tag: %d", w.Code)
		}
	}
	// A persisted portable change, even if the holder hasn't reloaded it, invalidates the tag.
	cfg := mustLoad(t, holder.Path())
	cfg.UI.Theme = "uika"
	if err := config.Save(cfg, holder.Path()); err != nil {
		t.Fatal(err)
	}
	if w := settingsRequest(h, "POST", "/api/settings/import", body, preview.Header().Get("ETag")); w.Code != 412 {
		t.Fatalf("stale tag: %d", w.Code)
	}
	preview = settingsPreview(t, h, body)
	cfg = mustLoad(t, holder.Path())
	cfg.Discord.ClientID = "changed-elsewhere"
	if err := config.Save(cfg, holder.Path()); err != nil {
		t.Fatal(err)
	}
	if w := settingsRequest(h, "POST", "/api/settings/import", body, preview.Header().Get("ETag")); w.Code != 200 {
		t.Fatalf("unrelated edit: %d", w.Code)
	}
	if got := mustLoad(t, holder.Path()).Discord.ClientID; got != "changed-elsewhere" {
		t.Fatal("lost unrelated edit")
	}
}

func TestSettingsFailedSaveAndAuthMethods(t *testing.T) {
	h, holder := fixtureHandler(t)
	body := settingsBody(t, settingsDocument(t, h))
	var mu sync.Mutex
	mux := http.NewServeMux()
	registerSettingsWithSave(mux, Deps{Cfg: holder, CfgPath: holder.Path()}, &mu, func(f http.HandlerFunc) http.HandlerFunc { return f }, func(*config.Config, string) error { return errors.New("save failed") })
	before := mustLoad(t, holder.Path())
	preview := settingsPreview(t, mux, body)
	if w := settingsRequest(mux, "POST", "/api/settings/import", body, preview.Header().Get("ETag")); w.Code != 500 {
		t.Fatalf("save error: %d", w.Code)
	}
	if !reflect.DeepEqual(before, mustLoad(t, holder.Path())) || !reflect.DeepEqual(before, holder.Get()) {
		t.Fatal("failed save changed config")
	}
	for _, path := range []string{"/api/settings", "/api/settings/preview", "/api/settings/import"} {
		if w := request(h, "POST", path, body, false); w.Code != 401 {
			t.Errorf("auth %s: %d", path, w.Code)
		}
		if w := request(h, "DELETE", path, "", true); w.Code != 405 {
			t.Errorf("method %s: %d", path, w.Code)
		}
	}
	if w := request(h, "GET", "/api/settings/preview", "", true); w.Code != 405 {
		t.Fatal("preview GET accepted")
	}
	if w := request(h, "GET", "/api/settings/import", "", true); w.Code != 405 {
		t.Fatal("import GET accepted")
	}
}

func TestSettingsExportRejectsUnportableSavedCardAndImportCanRepairIt(t *testing.T) {
	for name, value := range map[string]string{
		"oversized":        strings.Repeat("private", settingsMaxBytes),
		"invalid template": "private-{{.NotAField}}",
	} {
		t.Run(name, func(t *testing.T) {
			h, holder := fixtureHandler(t)
			valid := settingsBody(t, settingsDocument(t, h))
			cfg := mustLoad(t, holder.Path())
			cfg.Template.Details = value
			if err := config.Save(cfg, holder.Path()); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(holder.Path())
			if err != nil {
				t.Fatal(err)
			}
			w := settingsRequest(h, "GET", "/api/settings", "", "")
			if w.Code != http.StatusUnprocessableEntity || strings.Contains(w.Body.String(), "private") || w.Header().Get("Content-Disposition") != "" {
				t.Fatalf("unportable export: %d", w.Code)
			}
			preview := settingsPreview(t, h, valid)
			if strings.Contains(preview.Body.String(), "private") {
				t.Fatal("preview leaked saved card")
			}
			after, err := os.ReadFile(holder.Path())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("export or preview changed disk")
			}
			imported := settingsRequest(h, "POST", "/api/settings/import", valid, preview.Header().Get("ETag"))
			if imported.Code != 200 || mustLoad(t, holder.Path()).Template.Details == value {
				t.Fatalf("repair import: %d", imported.Code)
			}
		})
	}
}
