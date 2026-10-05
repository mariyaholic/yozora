//go:build windows

package presence

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
)

func TestSendUsesPlatformPresentation(t *testing.T) {
	for _, tc := range []struct {
		name, source, direct, lookup, wantURL string
	}{
		{"Spotify SMTC", "spotify", "", "https://music.apple.com/us/album/example/123?i=456", "https://open.spotify.com/search/fixture%20song%20fixture%20artist"},
		{"Spotify API", "spotifyapi", "https://open.spotify.com/track/fixture", "", "https://open.spotify.com/track/fixture"},
		{"Apple Music resolved", "applemusic", "", "https://music.apple.com/us/album/example/123?i=456", "https://music.apple.com/us/album/example/123?i=456"},
		{"Apple Music unresolved", "applemusic", "", "", "https://music.apple.com/us/search?term=fixture+song+fixture+artist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
			if err != nil {
				t.Fatal(err)
			}
			client := &recordingActivityClient{}
			now := time.Now()
			track := &player.Track{Source: tc.source, Title: "fixture song", Artist: "fixture artist", Album: "fixture album", Playing: true, LastUpdated: now, DurationSec: 200, ListenURL: tc.direct}
			engine := &Engine{Cfg: holder, ipc: client, cur: track, lastArtURL: "https://example.test/album.jpg", lastListenURL: tc.lookup}
			engine.send(track, now)
			if len(client.activities) != 1 {
				t.Fatalf("published %d activities, want 1", len(client.activities))
			}
			a := client.activities[0]
			wantName := "Spotify"
			if tc.source == "applemusic" {
				wantName = "Apple Music"
			}
			if a.Name != wantName || a.Type != discordipc.TypeListening || a.StatusDisplayType != 0 {
				t.Errorf("header = %q type=%d display=%d; want Listening to %s", a.Name, a.Type, a.StatusDisplayType, wantName)
			}
			if a.Assets == nil || a.Assets.LargeImage != engine.lastArtURL || a.Assets.SmallImage != holder.Get().Discord.SmallImage || a.Assets.SmallText != "Yozora" {
				t.Errorf("album/badge assets = %+v", a.Assets)
			}
			if len(a.Buttons) != 2 {
				t.Fatalf("buttons = %+v, want music followed by Yozora", a.Buttons)
			}
			if a.Buttons[0].URL != tc.wantURL || !strings.Contains(a.Buttons[0].Label, wantName) {
				t.Errorf("music button = %+v", a.Buttons[0])
			}
			if a.Buttons[1].Label != "Yozora" || a.Buttons[1].URL != holder.Get().Buttons.YozoraURL {
				t.Errorf("Yozora button = %+v", a.Buttons[1])
			}
		})
	}
}

func TestStatusDisplayTypeMatchesDiscordEnum(t *testing.T) {
	for value, want := range map[string]int{"app": 0, "name": 0, "state": 1, "details": 2} {
		if got := statusDisplayType(value); got != want {
			t.Errorf("statusDisplayType(%q) = %d, want %d", value, got, want)
		}
	}
}
