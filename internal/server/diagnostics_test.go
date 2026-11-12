//go:build windows

package server

import (
	"net/http"
	"strings"
	"testing"

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
)

func TestDiagnosticsReportRedactsPrivateText(t *testing.T) {
	cfg := config.Defaults()
	cfg.Discord.ClientID = "1556599139436077056"
	cfg.UI.Theme = "uika"
	cfg.Sources.Blocked = []string{"browser", "generic", "youtube", "youtube_tabs", "twitch", "youtubemusic"}
	st := presence.Status{
		Connected: true, Source: "spotify", Via: "deezer",
		Title: "Secret Song Title", Artist: "Hidden Artist", Album: "Private Album",
		LastErr: "send: open C:\\Users\\Alice Smith\\AppData\\Local\\uika-resonance\\state.json failed while playing \"secret song title\" by hidden artist " +
			"token=BQDx9aZ0123456789abcdefghijklmnopqrstuvwxyz client 1556599139436077056 id 0123456789abcdef0123456789abcdef https://api.example.test/x?access_token=zzz",
	}
	report := buildDiagnostics(diagInput{Version: "0.1.9", OSBuild: "10.0.22631", Status: st, Cfg: cfg})
	t.Logf("copied report:\n%s", report)
	for _, leak := range []string{"Secret Song", "secret song", "Hidden Artist", "hidden artist", "Private Album", "Alice", "Smith", "AppData", "BQDx", "1556599139436077056", "0123456789abcdef", "access_token=zzz", "C:\\"} {
		if strings.Contains(report, leak) {
			t.Errorf("report leaks %q:\n%s", leak, report)
		}
	}
	for _, want := range []string{"Version: 0.1.9", "Windows: 10.0.22631", "Source: spotify", "Artwork route: deezer", "Discord: connected", "Last error: send:", "Enabled sources: applemusic, spotify, spotifyapi", "Theme: uika", "%USERPROFILE%"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "browser") {
		t.Errorf("blocked source listed:\n%s", report)
	}
}

func TestDiagnosticsReportWhenQuiet(t *testing.T) {
	report := buildDiagnostics(diagInput{Cfg: config.Defaults()})
	for _, want := range []string{"Discord: not running", "Last error: none", "Version: unknown", "Source: unknown"} {
		if !strings.Contains(report, want) {
			t.Errorf("missing %q:\n%s", want, report)
		}
	}
}

func TestDiagnosticsAndRecentEndpoints(t *testing.T) {
	h, _ := fixtureHandler(t)
	if w := request(h, "GET", "/api/diagnostics", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("diagnostics without token: %d", w.Code)
	}
	if w := request(h, "GET", "/api/recent", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("recent without token: %d", w.Code)
	}
	w := request(h, "GET", "/api/diagnostics", "", true)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "Yozora diagnostics\n") || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("diagnostics: %d %q", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/recent", "", true)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"recent":[]}` {
		t.Fatalf("recent: %d %q", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/recent", "{}", true); w.Code != 405 {
		t.Fatalf("recent POST: %d", w.Code)
	}
}
