//go:build windows

package player

import "testing"

// Priority-hierarchy semantics: earlier entries in `order` win when several
// sources play; a vanished higher-ranked entry promotes the rest without
// reshuffling.
func TestArbitrateHonorsPriorityHierarchy(t *testing.T) {
	tracks := []Track{
		{Source: "browser", AppID: "MSEdge", Title: "video", Playing: true},
		{Source: "spotify", AppID: "Spotify.exe", Title: "song", Playing: true},
	}
	// Browser ranked above spotify: browser wins even though spotify is
	// canonical-first in the old default order.
	if got := Arbitrate(tracks, []string{"browser", "spotify", "spotifyapi", "applemusic", "generic"}, nil); got == nil || got.Source != "browser" {
		t.Fatalf("priority order not honored: got=%+v", got)
	}
	// Reversed order flips the winner.
	if got := Arbitrate(tracks, []string{"spotify", "browser"}, nil); got == nil || got.Source != "spotify" {
		t.Fatalf("reversed order not honored: got=%+v", got)
	}
}

func TestArbitratePromotesWhenTopPriorityDisappears(t *testing.T) {
	// 4 sources ordered A>B>C>D; only C visible → C wins.
	partial := []Track{{Source: "spotifyapi", Title: "api", Playing: true}, {Source: "generic", Title: "gen", Playing: true}}
	order := []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"}
	if got := Arbitrate(partial, order, nil); got == nil || got.Source != "spotifyapi" {
		t.Fatalf("fallback ignored order: got=%+v", got)
	}
	// When the top-priority (browser) vanishes, remaining three are exposed
	// *in their hierarchy order*, no reshuffling.
	withBrowser := make([]Track, 0, len(partial)+1)
	withBrowser = append(withBrowser, partial...)
	withBrowser = append(withBrowser, Track{Source: "browser", Title: "video", Playing: true})
	got := Arbitrate(withBrowser, order, nil)
	// browser ranks below spotifyapi in this order, so spotifyapi still wins;
	// reorder to put browser first and confirm it takes over.
	got = Arbitrate(withBrowser, []string{"applemusic", "spotify", "browser", "spotifyapi", "generic"}, nil)
	if got == nil || got.Source != "browser" {
		t.Fatalf("expected browser at top: got=%+v", got)
	}
}

func TestArbitratePlayingBeatsHigherRankedPaused(t *testing.T) {
	tracks := []Track{
		{Source: "browser", Title: "paused video", Playing: false},
		{Source: "generic", Title: "playing player", Playing: true},
	}
	// A ranked-but-paused source must not beat a lower-ranked playing one:
	// hierarchy is the tiebreak *within* each playing state.
	order := []string{"browser", "generic"}
	if got := Arbitrate(tracks, order, nil); got == nil || got.Source != "generic" {
		t.Fatalf("paused outranked playing: got=%+v", got)
	}
}

func TestArbitrateMissingSourcesRankLast(t *testing.T) {
	tracks := []Track{{Source: "unknown-source", Title: "mystery", Playing: true}, {Source: "browser", Title: "video", Playing: true}}
	got := Arbitrate(tracks, []string{"browser", "spotify"}, nil)
	if got == nil || got.Source != "browser" {
		t.Fatalf("unknown sources must rank below listed ones: got=%+v", got)
	}
}
