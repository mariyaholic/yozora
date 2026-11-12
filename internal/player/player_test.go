//go:build windows

package player

import (
	"testing"

	"uika-resonance/internal/smtc"
)

func TestIsBlockedSharesCanonicalAndAppIDRules(t *testing.T) {
	for _, b := range []string{" BrOwSeR ", "FIREFOX"} {
		tr := Track{Source: "browser", AppID: "org.mozilla.firefox", Title: "video", Playing: true}
		if !IsBlocked(tr, []string{b}) || Arbitrate([]Track{tr}, nil, []string{b}) != nil {
			t.Fatalf("filter %q did not block", b)
		}
	}
	if IsBlocked(Track{Source: "spotify", AppID: "Spotify.exe"}, []string{"", "browser"}) {
		t.Fatal("unrelated filter blocked music")
	}
}

func TestDisableBrowserFallsBackToMusic(t *testing.T) {
	tracks := []Track{{Source: "browser", Title: "video", Playing: true}, {Source: "spotify", Title: "music", Playing: true}}
	got := Arbitrate(tracks, []string{"browser", "spotify"}, []string{"BROWSER"})
	if got == nil || got.Source != "spotify" {
		t.Fatalf("fallback = %+v", got)
	}
}

func TestArbitrateBlocksCanonicalSource(t *testing.T) {
	tracks := []Track{{
		Source:  "browser",
		AppID:   "org.mozilla.firefox",
		Title:   "A song",
		Playing: true,
	}}

	if got := Arbitrate(tracks, []string{"browser"}, []string{"browser"}); got != nil {
		t.Fatalf("Arbitrate() = %+v, want blocked source to be dropped", got)
	}
}

func TestReservedSourceFilters(t *testing.T) {
	music := Track{Source: "youtubemusic", AppID: "com.github.th-ch.youtube-music", Title: "song", Playing: true}
	pwa := Track{Source: "youtubemusic", AppID: "MSEdge._crx_cinhimbnkkaeohfgghhklpknlkffjgod"}
	youtube := Track{Source: "browser", Service: "youtube", Title: "video", Playing: true}
	for _, tc := range []struct {
		track  Track
		filter string
		want   bool
	}{
		{music, "youtube", false}, {music, "youtubemusic", true},
		{pwa, "MSEdge", true}, {youtube, "browser", true}, {youtube, "youtube", true},
		{Track{Source: "browser", AppID: "youtube_tabs"}, "youtube_tabs", false},
		{Track{Source: "browser", AppID: "youtube"}, "youtube", false},
		{music, "th-ch", true},
	} {
		if got := IsBlocked(tc.track, []string{tc.filter}); got != tc.want {
			t.Errorf("%+v / %s: %v", tc.track, tc.filter, got)
		}
	}
	if got := Arbitrate([]Track{youtube, music}, []string{"browser", "youtubemusic"}, []string{"youtube"}); got == nil || got.Source != "youtubemusic" {
		t.Fatalf("no fallthrough: %+v", got)
	}
}

func TestYouTubeServiceBlockingAndFallback(t *testing.T) {
	youtube := Track{Source: "browser", Service: "youtube", Title: "video", Playing: true}
	plain := Track{Source: "browser", Title: "plain", Playing: true}
	music := Track{Source: "youtubemusic", AppID: "com.github.th-ch.youtube-music", Title: "song", Playing: true}
	if !IsBlocked(youtube, []string{"youtube"}) || !IsBlocked(youtube, []string{"browser"}) || IsBlocked(plain, []string{"youtube"}) || IsBlocked(music, []string{"youtube"}) {
		t.Fatal("incorrect service blocking")
	}
	got := Arbitrate([]Track{youtube, music}, []string{"browser", "youtubemusic"}, []string{"youtube"})
	if got == nil || got.Source != "youtubemusic" {
		t.Fatal("blocked YouTube did not fall through")
	}
}

// Custom per-app filters still substring-match the raw AppID, independent of
// how the player table classifies or names the session.
func TestCustomAppFiltersMatchAppID(t *testing.T) {
	for _, tc := range []struct {
		appID, filter string
		want          bool
	}{
		{"MSEdge", "msedge", true},
		{"MSEdge.Profile1", "msedge", true},
		{"MSEdge._crx_cinhimbnkkaeohfgghhklpknlkffjgod", "msedge", true},
		{"Chrome", "msedge", false},
		{"foobar2000.exe", "foobar", true},
		{"AppleInc.AppleTVWin_nzyj5cx40ttqa!App", "AppleTVWin", true},
		{"AppleInc.AppleMusicWin_nzyj5cx40ttqa!App", "AppleTVWin", false},
		{"TIDAL.exe", "generic", true},
		{"AppleInc.AppleTVWin_nzyj5cx40ttqa!App", "applemusic", false},
	} {
		tr := Track{Source: smtc.Classify(tc.appID), AppID: tc.appID, Player: smtc.FriendlyName(tc.appID), Title: "t", Playing: true}
		if got := IsBlocked(tr, []string{tc.filter}); got != tc.want {
			t.Errorf("IsBlocked(%q, %q) = %v, want %v", tc.appID, tc.filter, got, tc.want)
		}
	}
}
