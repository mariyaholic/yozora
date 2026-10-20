//go:build windows

package player

import "testing"

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
