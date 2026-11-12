package smtc

import (
	"slices"
	"testing"
)

func TestKnownPlayers(t *testing.T) {
	for _, tc := range []struct{ appID, source, name string }{
		// Apple Music and the Apple TV app share a publisher.
		{"AppleInc.AppleMusicWin_nzyj5cx40ttqa!App", "applemusic", "Apple Music"},
		{"APPLEINC.APPLEMUSICWIN_NZYJ5CX40TTQA!APP", "applemusic", "Apple Music"},
		{"AppleMusic.exe", "applemusic", "Apple Music"},
		{"AppleInc.AppleTVWin_nzyj5cx40ttqa!App", "generic", "Apple TV"},
		{"AppleInc.AppleTVWin_nzyj5cx40ttqa!AppleTV", "generic", "Apple TV"},
		{"iTunes.exe", "generic", "iTunes"},

		{"Spotify.exe", "spotify", "Spotify"},
		{"SpotifyAB.SpotifyMusic_zpdnekdrzrea0!Spotify", "spotify", "Spotify"},
		{"MSEdge._crx_cinhimbnkkaeohfgghhklpknlkffjgod", "youtubemusic", "YouTube Music"},
		{"Chrome._crx_cinhimbnkkaeohfgghhklpknlkffjgod_Profile 2", "youtubemusic", "YouTube Music"},
		{"music.youtube.com-5929F88E_vezhnr0wkvrcy!App", "youtubemusic", "YouTube Music"},
		{"com.github.th-ch.youtube-music", "youtubemusic", "YouTube Music"},
		{"com.squirrel.youtube_music_desktop_app.youtube-music-desktop-app", "youtubemusic", "YouTube Music"},

		// Newly named players stay in the generic source.
		{"TIDAL.exe", "generic", "Tidal"},
		{"WiMPMusic.27241E05630EA_kn85bz84x7te4!TIDAL", "generic", "Tidal"},
		{"Deezer.62021768415AF_q7m17pa7q8kj0!Deezer.Music", "generic", "Deezer"},
		{"Qobuz.exe", "generic", "Qobuz"},
		{"Amazon Music.exe", "generic", "Amazon Music"},
		{"AmazonMobileLLC.AmazonMusic_kc6t79cpj4tp0!AmazonMobileLLC.AmazonMusic", "generic", "Amazon Music"},
		{"foobar2000.exe", "generic", "foobar2000"},
		{`C:\Program Files\foobar2000\foobar2000.exe`, "generic", "foobar2000"},
		{"MusicBee.exe", "generic", "MusicBee"},
		{"AIMP.exe", "generic", "AIMP"},
		{"vlc.exe", "generic", "VLC"},
		{"VideoLAN.VLC_paz6r1rewnh0a!App", "generic", "VLC"},
		{"Microsoft.ZuneMusic_8wekyb3d8bbwe!Microsoft.ZuneMusic", "generic", "Media Player"},
		{"wmplayer.exe", "generic", "Windows Media Player"},

		// Browsers.
		{"MSEdge", "browser", "Microsoft Edge"},
		{"msedge.exe", "browser", "Microsoft Edge"},
		{"MSEdge.Profile1", "browser", "Microsoft Edge"},
		{"Chrome", "browser", "Google Chrome"},
		{"Chrome._crx_aaaabbbbccccddddeeeeffffgggghhhh", "browser", "Google Chrome"},
		{"Brave", "browser", "Brave"},
		{"Opera", "browser", "Opera"},
		{"Vivaldi", "browser", "Vivaldi"},
		{"TheBrowserCompany.Arc_ttt1ap7aakyb4!Arc", "browser", "Arc"},
		{"308046B0AF4A39CB", "browser", "Firefox"},
		{"firefox.exe", "browser", "Firefox"},

		// Substrings of known names no longer match.
		{"Contoso.ArcadeMusic_abc123!App", "generic", "ArcadeMusic"},
		{"Pineapple.exe", "generic", "Pineapple"},
		{"EdgeTunes.exe", "generic", "EdgeTunes"},
		{"Fabrikam.SearchPlayer_x1y2z3!App", "generic", "SearchPlayer"},
		{"MSEdgeHelper", "generic", "MSEdgeHelper"},
	} {
		t.Run(tc.appID, func(t *testing.T) {
			if got := Classify(tc.appID); got != tc.source {
				t.Errorf("Classify(%q) = %q, want %q", tc.appID, got, tc.source)
			}
			if got := FriendlyName(tc.appID); got != tc.name {
				t.Errorf("FriendlyName(%q) = %q, want %q", tc.appID, got, tc.name)
			}
		})
	}
}

func TestUnknownPlayerNames(t *testing.T) {
	for _, tc := range []struct{ appID, want string }{
		{"strawberry.exe", "Strawberry"},
		{"MediaMonkey.exe", "MediaMonkey"},
		{`D:\Apps\my-player\my-player.exe`, "My Player"},
		{"Contoso.CoolPlayer_8wekyb3d8bbwe!App", "CoolPlayer"},
		{"Contoso.27241E05630EA_kn85bz84x7te4!Jukebox", "Jukebox"},
		{"com.example.tune-box", "Tune Box"},
		{"Plexamp", "Plexamp"},
		{"F0DC299D809B9700", "Media Player"},
		{"", "Media Player"},
	} {
		if got := FriendlyName(tc.appID); got != tc.want {
			t.Errorf("FriendlyName(%q) = %q, want %q", tc.appID, got, tc.want)
		}
		if got := Classify(tc.appID); got != "generic" {
			t.Errorf("Classify(%q) = %q, want generic", tc.appID, got)
		}
	}
}

// The table must only use the six canonical sources (config.OrderSources),
// without importing config into this package.
func TestKnownPlayersUseCanonicalSources(t *testing.T) {
	canonical := []string{"applemusic", "spotify", "spotifyapi", "youtubemusic", "browser", "generic"}
	for _, p := range knownPlayers {
		if !slices.Contains(canonical, p.source) || p.name == "" || len(p.ids)+len(p.contains) == 0 {
			t.Errorf("bad entry %+v", p)
		}
	}
}
