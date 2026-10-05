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
