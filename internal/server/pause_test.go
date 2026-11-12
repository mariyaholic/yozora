//go:build windows

package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
)

func pauseFixture(t *testing.T) (http.Handler, *presence.Engine, *config.Holder) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.toml")
	if err := config.Save(config.Defaults(), path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	eng := presence.New(holder, nil, nil, nil, "")
	return Handler(Deps{Engine: eng, Cfg: holder, CfgPath: path, Token: "fixture-token"}), eng, holder
}

func TestPauseEndpointPausesTimedAndResumes(t *testing.T) {
	h, eng, _ := pauseFixture(t)
	before := time.Now()
	w := request(h, "POST", "/api/pause", `{"action":"pause","minutes":60}`, true)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	paused, until := eng.Paused(time.Now())
	if !paused || until.Before(before.Add(59*time.Minute)) || until.After(time.Now().Add(61*time.Minute)) {
		t.Fatalf("paused=%v until=%v", paused, until)
	}
	body := decodeBody(t, w)
	if body["paused"] != true || body["paused_until"] == nil {
		t.Fatalf("body = %v", body)
	}

	w = request(h, "GET", "/api/state?t=fixture-token", "", false)
	state := decodeBody(t, w)
	if state["paused"] != true || state["paused_until"] == nil {
		t.Fatalf("/api/state lacks the pause: %v", state)
	}

	w = request(h, "POST", "/api/pause", `{"action":"resume"}`, true)
	if w.Code != 200 || eng.Status().Paused {
		t.Fatalf("resume: %d paused=%v", w.Code, eng.Status().Paused)
	}
	if body := decodeBody(t, w); body["paused"] != false || body["paused_until"] != nil {
		t.Fatalf("body = %v", body)
	}
	if state := decodeBody(t, request(h, "GET", "/api/state?t=fixture-token", "", false)); state["paused"] != false || state["paused_until"] != nil {
		t.Fatalf("state after resume: %v", state)
	}
}

func TestPauseEndpointUntilResumed(t *testing.T) {
	h, eng, _ := pauseFixture(t)
	if w := request(h, "POST", "/api/pause", `{"action":"pause"}`, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if paused, until := eng.Paused(time.Now()); !paused || !until.IsZero() {
		t.Fatalf("paused=%v until=%v", paused, until)
	}
	if body := decodeBody(t, request(h, "POST", "/api/pause", `{"action":"pause"}`, true)); body["paused_until"] != nil {
		t.Fatalf("an open pause reported an end: %v", body)
	}
}

func TestPauseEndpointRejectsBadRequests(t *testing.T) {
	h, eng, _ := pauseFixture(t)
	for _, body := range []string{``, `{}`, `null`, `[]`, `{"action":""}`, `{"action":"Pause"}`, `{"action":"stop"}`,
		`{"action":"pause","minutes":0}`, `{"action":"pause","minutes":-5}`, `{"action":"pause","minutes":1441}`,
		`{"action":"pause","minutes":1.5}`, `{"action":"pause","minutes":"60"}`, `{"action":"pause","minutes":null,"x":1}`,
		`{"action":"pause","extra":true}`, `{"action":"resume","minutes":5}`, `{"minutes":60}`,
		`{"action":"pause"} {}`, `{"action":"pause"} junk`, `{"action":"pause","note":"` + strings.Repeat("x", 300) + `"}`} {
		w := request(h, "POST", "/api/pause", body, true)
		if w.Code != 400 && w.Code != 413 {
			t.Errorf("accepted %q: %d", body[:min(len(body), 60)], w.Code)
		}
		if eng.Status().Paused {
			t.Fatalf("rejected request %q paused sharing", body[:min(len(body), 60)])
		}
	}
	if w := request(h, "POST", "/api/pause", `{"action":"pause","minutes":1440}`, true); w.Code != 200 {
		t.Errorf("a full day was refused: %d", w.Code)
	}
}

func TestPauseEndpointNeedsTokenAndPost(t *testing.T) {
	h, eng, _ := pauseFixture(t)
	if w := request(h, "POST", "/api/pause", `{"action":"pause"}`, false); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	if eng.Status().Paused {
		t.Fatal("unauthorised request paused sharing")
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if w := request(h, method, "/api/pause", `{"action":"pause"}`, true); w.Code != 405 {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
}

func TestSetSourceEnabledUsesPanelPath(t *testing.T) {
	_, _, holder := pauseFixture(t)
	if err := SetSourceEnabled(holder.Path(), holder, "youtube", true); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceEnabled(holder.Path(), holder, "twitch", false); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(holder.Path())
	if err != nil {
		t.Fatal(err)
	}
	has := func(key string) bool {
		for _, b := range loaded.Sources.Blocked {
			if b == key {
				return true
			}
		}
		return false
	}
	if has("youtube") || !has("twitch") {
		t.Fatalf("blocked = %v", loaded.Sources.Blocked)
	}
	if got := holder.Get().Sources.Blocked; len(got) != len(loaded.Sources.Blocked) {
		t.Fatalf("holder not reloaded: %v vs %v", got, loaded.Sources.Blocked)
	}
	if err := SetSourceEnabled(holder.Path(), holder, "nonsense", false); err == nil {
		t.Fatal("unknown source accepted")
	}
}
