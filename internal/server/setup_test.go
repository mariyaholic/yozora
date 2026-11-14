//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
	"uika-resonance/internal/spotifyapi"
)

const spotifyTestClient = "0123456789abcdef0123456789abcdef"

type setupFixture struct {
	h        http.Handler
	cfg      *config.Holder
	opened   []string
	openErr  error
	stored   []string
	cleared  int
	clearErr error
	exchange []url.Values
	manager  *spotifyapi.Manager
	link     *spotifyapi.Link
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.toml")
	if err := config.Save(config.Defaults(), path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	f := &setupFixture{cfg: holder, manager: spotifyapi.NewManager()}
	t.Cleanup(f.manager.Stop)
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.exchange = append(f.exchange, r.PostForm)
		_, _ = w.Write([]byte(`{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":3600}`))
	}))
	t.Cleanup(token.Close)
	link := &spotifyapi.Link{TokenURL: token.URL, HTTP: token.Client(), Store: func(tok string) error { f.stored = append(f.stored, tok); return nil }}
	f.link = link
	f.h = Handler(Deps{
		Engine: presence.New(holder, nil, nil, nil, ""), Cfg: holder, CfgPath: path, Token: "fixture-token",
		Spotify: f.manager, SpotifyLink: link, RedirectBase: "http://127.0.0.1:50123",
		OpenURL:           func(u string) error { f.opened = append(f.opened, u); return f.openErr },
		ClearSpotifyToken: func() error { f.cleared++; return f.clearErr },
	})
	return f
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return out
}

func TestSetupOnboardedAndDiscordAppID(t *testing.T) {
	f := newSetupFixture(t)
	if w := request(f.h, "GET", "/api/setup", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated setup = %d", w.Code)
	}
	got := decodeBody(t, request(f.h, "GET", "/api/setup", "", true))
	if got["onboarded"] != false || got["default_client_id"] != true {
		t.Fatalf("initial setup = %v", got)
	}
	for _, body := range []string{`{}`, `{"onboarded":"yes"}`, `{"onboarded":true,"x":1}`, `{"onboarded":true} {}`, `{"client_id":"123"}`, `{"client_id":"12345678901234567a"}`, `{"client_id":"123456789012345678901"}`} {
		if w := request(f.h, "POST", "/api/setup", body, true); w.Code != 400 {
			t.Errorf("accepted %s: %d", body, w.Code)
		}
	}
	if f.cfg.Get().UI.Onboarded {
		t.Fatal("a rejected body changed the config")
	}
	got = decodeBody(t, request(f.h, "POST", "/api/setup", `{"onboarded":true}`, true))
	if got["onboarded"] != true || !f.cfg.Get().UI.Onboarded {
		t.Fatalf("onboarded not saved: %v", got)
	}
	loaded, err := config.Load(f.cfg.Path())
	if err != nil || !loaded.UI.Onboarded {
		t.Fatalf("onboarded not in the file: %v", err)
	}
	got = decodeBody(t, request(f.h, "POST", "/api/setup", `{"client_id":" 112233445566778899 "}`, true))
	if got["client_id"] != "112233445566778899" || got["default_client_id"] != false {
		t.Fatalf("custom app ID = %v", got)
	}
	got = decodeBody(t, request(f.h, "POST", "/api/setup", `{"client_id":""}`, true))
	if got["default_client_id"] != true || f.cfg.Get().Discord.ClientID != config.Defaults().Discord.ClientID {
		t.Fatalf("empty app ID did not restore Yozora's: %v", got)
	}
}

func TestTestCardEndpointQueuesOnce(t *testing.T) {
	f := newSetupFixture(t)
	if w := request(f.h, "POST", "/api/test-card", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated test card = %d", w.Code)
	}
	if w := request(f.h, "GET", "/api/test-card", "", true); w.Code != 405 {
		t.Fatalf("GET test card = %d", w.Code)
	}
	if w := request(f.h, "POST", "/api/test-card", "", true); w.Code != http.StatusAccepted {
		t.Fatalf("test card = %d", w.Code)
	}
	if w := request(f.h, "POST", "/api/test-card", "", true); w.Code != http.StatusConflict {
		t.Fatalf("second test card = %d", w.Code)
	}
	state := decodeBody(t, request(f.h, "GET", "/api/state", "", true))
	if card, _ := state["test_card"].(map[string]any); card["state"] != "sending" {
		t.Fatalf("state test_card = %v", state["test_card"])
	}
}

func connect(t *testing.T, f *setupFixture) string {
	t.Helper()
	w := request(f.h, "POST", "/api/spotify", `{"action":"connect","client_id":"`+strings.ToUpper(spotifyTestClient)+`"}`, true)
	if w.Code != 200 || len(f.opened) == 0 {
		t.Fatalf("connect = %d %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w); got["waiting"] != true || got["redirect_uri"] != "http://127.0.0.1:50123/spotify/callback" {
		t.Fatalf("connect state = %v", got)
	}
	u, err := url.Parse(f.opened[len(f.opened)-1])
	if err != nil || u.Host != "accounts.spotify.com" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("client_id") != spotifyTestClient {
		t.Fatalf("opened %v", u)
	}
	return u.Query().Get("state")
}

func TestSpotifyConnectCallbackAndDisconnect(t *testing.T) {
	f := newSetupFixture(t)
	if w := request(f.h, "POST", "/api/spotify", `{"action":"connect","client_id":"`+spotifyTestClient+`"}`, false); w.Code != 401 {
		t.Fatalf("unauthenticated connect = %d", w.Code)
	}
	state := connect(t, f)

	forged := request(f.h, "GET", "/spotify/callback?state=forged&code=evil", "", false)
	if forged.Code != 400 || len(f.exchange) != 0 {
		t.Fatalf("forged callback = %d, exchanges %d", forged.Code, len(f.exchange))
	}
	if w := request(f.h, "POST", spotifyCallbackPath+"?state="+state+"&code=c", "", false); w.Code != 405 {
		t.Fatalf("POST callback = %d", w.Code)
	}
	ok := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state)+"&code=the-code", "", false)
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), "Spotify is connected") {
		t.Fatalf("callback = %d %s", ok.Code, ok.Body.String())
	}
	for _, secret := range []string{"the-code", "refresh-secret", "access-secret", state} {
		if strings.Contains(ok.Body.String(), secret) {
			t.Fatalf("callback page shows %q", secret)
		}
	}
	if !strings.Contains(ok.Header().Get("Content-Security-Policy"), "default-src 'none'") || ok.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("callback headers = %v", ok.Header())
	}
	if len(f.stored) != 1 || f.stored[0] != "refresh-secret" || f.exchange[0].Get("code_verifier") == "" {
		t.Fatalf("token not stored through PKCE: stored=%v form=%v", f.stored, f.exchange)
	}
	cfg := f.cfg.Get()
	if cfg.Spotify.Mode != "api" || cfg.Spotify.ClientID != spotifyTestClient || !f.manager.Running() {
		t.Fatalf("after callback mode=%q id=%q running=%v", cfg.Spotify.Mode, cfg.Spotify.ClientID, f.manager.Running())
	}
	if replay := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state)+"&code=the-code", "", false); replay.Code != 400 || len(f.exchange) != 1 {
		t.Fatalf("replayed callback = %d, exchanges %d", replay.Code, len(f.exchange))
	}
	got := decodeBody(t, request(f.h, "GET", "/api/state", "", true))
	if sp, _ := got["spotify"].(map[string]any); sp["connected"] != true || sp["waiting"] != false || sp["available"] != true {
		t.Fatalf("state spotify = %v", got["spotify"])
	}

	got = decodeBody(t, request(f.h, "POST", "/api/spotify", `{"action":"disconnect"}`, true))
	if got["connected"] != false || f.manager.Running() || f.cleared != 1 || f.cfg.Get().Spotify.Mode != "system" {
		t.Fatalf("disconnect = %v running=%v cleared=%d mode=%q", got, f.manager.Running(), f.cleared, f.cfg.Get().Spotify.Mode)
	}
}

func TestSpotifyConnectRejectsBadInputAndBrowserFailure(t *testing.T) {
	f := newSetupFixture(t)
	for _, body := range []string{`{}`, `{"action":"connect"}`, `{"action":"connect","client_id":"xyz"}`, `{"action":"connect","client_id":"` + spotifyTestClient + `","secret":"s"}`, `{"action":"launch"}`} {
		if w := request(f.h, "POST", "/api/spotify", body, true); w.Code != 400 {
			t.Errorf("accepted %s: %d", body, w.Code)
		}
	}
	if len(f.opened) != 0 {
		t.Fatal("a rejected request opened the browser")
	}
	f.openErr = errors.New("no browser")
	if w := request(f.h, "POST", "/api/spotify", `{"action":"connect","client_id":"`+spotifyTestClient+`"}`, true); w.Code != http.StatusBadGateway {
		t.Fatalf("browser failure = %d", w.Code)
	}
	state, _ := url.Parse(f.opened[0])
	if w := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state.Query().Get("state"))+"&code=c", "", false); w.Code != 400 {
		t.Fatalf("a flow whose browser never opened stayed redeemable: %d", w.Code)
	}
	f.openErr = nil
	st := connect(t, f)
	if got := decodeBody(t, request(f.h, "POST", "/api/spotify", `{"action":"cancel"}`, true)); got["waiting"] != false {
		t.Fatalf("cancel = %v", got)
	}
	if w := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(st)+"&code=c", "", false); w.Code != 400 || len(f.exchange) != 0 {
		t.Fatalf("cancelled flow callback = %d", w.Code)
	}
	st = connect(t, f)
	denied := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(st)+"&error=access_denied", "", false)
	if denied.Code != http.StatusBadGateway || f.manager.Running() {
		t.Fatalf("denied = %d", denied.Code)
	}
	if got := decodeBody(t, request(f.h, "GET", "/api/spotify", "", true)); !strings.Contains(got["error"].(string), "not granted") {
		t.Fatalf("denial not reported: %v", got)
	}
}

func TestSpotifyConnectUnavailableWithoutManager(t *testing.T) {
	h, _ := fixtureHandler(t)
	if w := request(h, "GET", "/api/spotify", "", true); w.Code != 404 {
		t.Fatalf("spotify without a manager = %d", w.Code)
	}
	if w := request(h, "GET", spotifyCallbackPath+"?state=x&code=y", "", false); w.Code != 404 {
		t.Fatalf("callback without a manager = %d", w.Code)
	}
}

// /api/state names the broken file only after a startup that fell back to the last-good copy.
func TestStateReportsRestoredConfig(t *testing.T) {
	f := newSetupFixture(t)
	if _, ok := decodeBody(t, request(f.h, "GET", "/api/state", "", true))["config_restored"]; ok {
		t.Fatal("a normal start reported a restored config")
	}

	path := f.cfg.Path()
	if err := os.WriteFile(path, []byte("[ui\ntheme = "), 0o644); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Deps{Engine: presence.New(holder, nil, nil, nil, ""), Cfg: holder, CfgPath: path, Token: "fixture-token"})
	got, _ := decodeBody(t, request(h, "GET", "/api/state", "", true))["config_restored"].(map[string]any)
	broken, _ := got["broken_path"].(string)
	if !strings.HasPrefix(filepath.Base(broken), "setup.toml.broken-") || got["error"] == "" || got["at"] == "" {
		t.Fatalf("config_restored = %v", got)
	}
	if _, err := os.Stat(broken); err != nil {
		t.Fatalf("broken file not kept: %v", err)
	}
}

func TestSpotifyDisconnectDuringExchangeDoesNotReconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.toml")
	if err := config.Save(config.Defaults(), path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"refresh_token":"late-token"}`))
	}))
	defer token.Close()
	stored := 0
	link := &spotifyapi.Link{TokenURL: token.URL, HTTP: token.Client(), Store: func(string) error { stored++; return nil }}
	manager := spotifyapi.NewManager()
	defer manager.Stop()
	var opened string
	cleared := 0
	h := Handler(Deps{Cfg: holder, CfgPath: path, Token: "fixture-token", Spotify: manager, SpotifyLink: link,
		RedirectBase: "http://127.0.0.1:50123", OpenURL: func(u string) error { opened = u; return nil },
		ClearSpotifyToken: func() error { cleared++; return nil }})
	if w := request(h, "POST", "/api/spotify", `{"action":"connect","client_id":"`+spotifyTestClient+`"}`, true); w.Code != 200 {
		t.Fatalf("connect = %d %s", w.Code, w.Body.String())
	}
	u, err := url.Parse(opened)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- request(h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(u.Query().Get("state"))+"&code=c", "", false)
	}()
	<-entered
	w := request(h, "POST", "/api/spotify", `{"action":"disconnect"}`, true)
	close(release)
	callback := <-result
	if w.Code != 200 || callback.Code != http.StatusBadRequest {
		t.Fatalf("disconnect = %d; callback = %d", w.Code, callback.Code)
	}
	if stored != 0 || cleared != 1 || manager.Running() || holder.Get().Spotify.Mode != "system" {
		t.Fatalf("stored=%d cleared=%d running=%v mode=%q", stored, cleared, manager.Running(), holder.Get().Spotify.Mode)
	}
}

func TestSpotifyCallbackReportsConfigSaveFailure(t *testing.T) {
	f := newSetupFixture(t)
	state := connect(t, f)
	if err := os.Remove(f.cfg.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.cfg.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	w := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state)+"&code=c", "", false)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "could not save") {
		t.Fatalf("callback = %d %s", w.Code, w.Body.String())
	}
	if f.manager.Running() || f.cfg.Get().Spotify.Mode != "system" {
		t.Fatal("failed save started Spotify or changed active config")
	}
	if got := decodeBody(t, request(f.h, "GET", "/api/spotify", "", true)); got["waiting"] != false || got["error"] == nil {
		t.Fatalf("failure state = %v", got)
	}
}

func TestSpotifyBrowserFailureDoesNotCancelNewerConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.toml")
	if err := config.Save(config.Defaults(), path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"refresh_token":"token"}`))
	}))
	defer token.Close()
	link := &spotifyapi.Link{TokenURL: token.URL, HTTP: token.Client(), Store: func(string) error { return nil }}
	manager := spotifyapi.NewManager()
	defer manager.Stop()
	var calls atomic.Int32
	firstOpened, release := make(chan struct{}), make(chan struct{})
	var newerURL string
	h := Handler(Deps{Cfg: holder, CfgPath: path, Token: "fixture-token", Spotify: manager, SpotifyLink: link,
		RedirectBase: "http://127.0.0.1:50123", OpenURL: func(u string) error {
			if calls.Add(1) == 1 {
				close(firstOpened)
				<-release
				return errors.New("browser failed")
			}
			newerURL = u
			return nil
		}})
	body := `{"action":"connect","client_id":"` + spotifyTestClient + `"}`
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- request(h, "POST", "/api/spotify", body, true) }()
	<-firstOpened
	second := request(h, "POST", "/api/spotify", body, true)
	close(release)
	failed := <-first
	if second.Code != 200 || failed.Code != http.StatusBadGateway {
		t.Fatalf("newer connect = %d; first = %d", second.Code, failed.Code)
	}
	u, err := url.Parse(newerURL)
	if err != nil {
		t.Fatal(err)
	}
	callback := request(h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(u.Query().Get("state"))+"&code=c", "", false)
	if callback.Code != 200 || !manager.Running() {
		t.Fatalf("newer callback = %d %s", callback.Code, callback.Body.String())
	}
}

func TestSpotifyDisconnectReportsAndRetriesCredentialDeletionFailure(t *testing.T) {
	f := newSetupFixture(t)
	state := connect(t, f)
	if w := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state)+"&code=c", "", false); w.Code != 200 {
		t.Fatalf("callback = %d %s", w.Code, w.Body.String())
	}
	f.clearErr = errors.New("credential manager unavailable")
	w := request(f.h, "POST", "/api/spotify", `{"action":"disconnect"}`, true)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "could not remove Spotify credentials") {
		t.Fatalf("disconnect = %d %s", w.Code, w.Body.String())
	}
	if f.manager.Running() || f.cfg.Get().Spotify.Mode != "system" || f.cleared != 1 {
		t.Fatalf("running=%v mode=%q clears=%d", f.manager.Running(), f.cfg.Get().Spotify.Mode, f.cleared)
	}
	f.clearErr = nil
	w = request(f.h, "POST", "/api/spotify", `{"action":"disconnect"}`, true)
	if w.Code != 200 || f.cleared != 2 || f.manager.Running() || f.cfg.Get().Spotify.Mode != "system" {
		t.Fatalf("retry = %d clears=%d running=%v mode=%q", w.Code, f.cleared, f.manager.Running(), f.cfg.Get().Spotify.Mode)
	}
}

func TestSpotifyCallbackStopsOldManagerBeforeStoringToken(t *testing.T) {
	f := newSetupFixture(t)
	f.manager.Start(spotifyTestClient, "", time.Hour)
	if !f.manager.Running() {
		t.Fatal("old manager did not start")
	}
	oldRunningAtStore := true
	f.link.Store = func(string) error {
		oldRunningAtStore = f.manager.Running()
		return nil
	}
	state := connect(t, f)
	w := request(f.h, "GET", spotifyCallbackPath+"?state="+url.QueryEscape(state)+"&code=c", "", false)
	if w.Code != 200 || oldRunningAtStore || !f.manager.Running() {
		t.Fatalf("callback=%d old running at store=%v new running=%v", w.Code, oldRunningAtStore, f.manager.Running())
	}
}
