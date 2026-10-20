//go:build windows

package smtc

import "testing"

func TestYouTubeMusicClassification(t *testing.T) {
	for _, tc := range []struct{ appID, want string }{
		{"Spotify.exe", "spotify"},
		{"SpotifyAB.SpotifyMusic_zpdnekdrzrea0!Spotify", "spotify"},
		{"AppleInc.AppleMusicWin_nzyj5cx40ttqa!App", "applemusic"},
		{"MSEdge", "browser"},
		{"Chrome", "browser"},
		{"MSEdge._crx_cinhimbnkkaeohfgghhklpknlkffjgod", "youtubemusic"},
		{"MSEDGE._CRX_CINHIMBNKKAEOHFGGHHKLPKNLKFFJGOD", "youtubemusic"},
		{"Chrome._crx_cinhimbnkkaeohfgghhklpknlkffjgod_Profile 2", "youtubemusic"},
		{"CHROME._CRX_CINHIMBNKKAEOHFGGHHKLPKNLKFFJGOD_PROFILE 2", "youtubemusic"},
		{"music.youtube.com-5929f88e_vezhnr0wkvrcy!App", "youtubemusic"},
		{"MUSIC.YOUTUBE.COM-5929F88E_VEZHNR0WKVRCY!APP", "youtubemusic"},
		{"com.github.th-ch.youtube-music", "youtubemusic"},
		{"COM.GITHUB.TH-CH.YOUTUBE-MUSIC", "youtubemusic"},
		{"com.squirrel.youtube_music_desktop_app.youtube-music-desktop-app", "youtubemusic"},
		{"COM.SQUIRREL.YOUTUBE_MUSIC_DESKTOP_APP.YOUTUBE-MUSIC-DESKTOP-APP", "youtubemusic"},
	} {
		t.Run(tc.appID, func(t *testing.T) {
			if got := Classify(tc.appID); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.appID, got, tc.want)
			}
			if tc.want == "youtubemusic" && FriendlyName(tc.appID) != "YouTube Music" {
				t.Errorf("FriendlyName(%q) = %q", tc.appID, FriendlyName(tc.appID))
			}
		})
	}
}
