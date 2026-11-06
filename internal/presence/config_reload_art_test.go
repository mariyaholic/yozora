//go:build windows

package presence

import (
	"testing"
	"testing/synctest"
	"time"
	"uika-resonance/internal/art"
	"uika-resonance/internal/browserwin"
	"uika-resonance/internal/player"
)

func TestArtworkPreferenceReloadRejectsOlderGenerationOnUnchangedTrack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "spotify", Title: "Same item", Playing: true, LastUpdated: now}
		e, resolver := newArtworkTestEngine(t, track)
		e.step(now)
		old := nextArtworkRequest(t, resolver, track.Title)
		e.Cfg.Get().Art.Prefer = "smtc"
		e.step(now.Add(time.Second))
		fresh := nextArtworkRequest(t, resolver, track.Title)
		if fresh.prefer != "smtc" {
			t.Fatalf("preference = %q", fresh.prefer)
		}
		fresh.result <- art.Lookup{ImageURL: "https://example.test/new.jpg"}
		synctest.Wait()
		old.result <- art.Lookup{ImageURL: "https://example.test/old.jpg"}
		synctest.Wait()
		if e.Status().ArtURL != "https://example.test/new.jpg" {
			t.Fatal("stale generation replaced reloaded artwork")
		}
		e.step(now.Add(7 * time.Second))
		if e.ipc.(*recordingActivityClient).activities[0].Assets.LargeImage != "https://example.test/new.jpg" {
			t.Fatal("unchanged item did not publish reloaded artwork")
		}
	})
}

func TestYouTubeTabToggleReloadsAnUnchangedItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "browser", AppID: "msedge", Title: "Video", Playing: true, LastUpdated: now}
		e, resolver := newArtworkTestEngine(t, track)
		e.Cfg.Get().Sources.Blocked = []string{"youtube_tabs"}
		e.EnumerateWindows = func() []browserwin.Window { return []browserwin.Window{{HWND: 1, Title: "Unrelated - Microsoft Edge"}} }
		reads := 0
		e.ReadTabNames = func(uintptr, int) ([]string, error) { reads++; return []string{"Video - YouTube - Audio playing"}, nil }
		e.ReadAddressBar = func(uintptr) (string, error) { t.Fatal("background tab address read"); return "", nil }
		for i, enabled := range []bool{false, true, false, true} {
			if enabled {
				e.Cfg.Get().Sources.Blocked = nil
			} else {
				e.Cfg.Get().Sources.Blocked = []string{"youtube_tabs"}
			}
			at := now.Add(time.Duration(i*10) * time.Second)
			e.step(at)
			nextArtworkRequest(t, resolver, track.Title).result <- art.Lookup{ImageURL: "https://example.test/media.jpg"}
			synctest.Wait()
			e.step(at.Add(7 * time.Second))
			c := e.browserCache[browserIdentity(track)]
			if (c.Service == "youtube") != enabled || c.VideoID != "" {
				t.Fatalf("toggle %v: %+v", enabled, c)
			}
		}
		if reads != 2 {
			t.Fatalf("tab reads = %d", reads)
		}
	})
}

func TestVerifiedYouTubeContinuitySurvivesDisablingTabDiscovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "browser", AppID: "msedge", Title: "Video", Playing: true, LastUpdated: now}
		e, queued := newArtworkTestEngine(t, track)
		e.Art = &youtubeArtwork{queuedArtwork: queued}
		e.Cfg.Get().Sources.Blocked = nil
		foreground := true
		e.EnumerateWindows = func() []browserwin.Window {
			title := "Unrelated - Microsoft Edge"
			if foreground {
				title = "Video - YouTube - Microsoft Edge"
			}
			return []browserwin.Window{{HWND: 1, Title: title}}
		}
		e.ReadAddressBar = func(uintptr) (string, error) {
			if !foreground {
				t.Fatal("background address read")
			}
			return "https://www.youtube.com/watch?v=" + backgroundVideoID, nil
		}
		e.step(now)
		nextArtworkRequest(t, queued, track.Title).result <- art.Lookup{}
		synctest.Wait()
		e.step(now.Add(time.Second))
		assertBackgroundYouTube(t, e, backgroundVideoID)
		foreground = false
		e.Cfg.Get().Sources.Blocked = []string{"youtube_tabs"}
		e.ReadTabNames = func(uintptr, int) ([]string, error) { t.Fatal("disabled tab scan"); return nil, nil }
		e.step(now.Add(7 * time.Second))
		nextArtworkRequest(t, queued, track.Title).result <- art.Lookup{}
		synctest.Wait()
		e.step(now.Add(14 * time.Second))
		assertBackgroundYouTube(t, e, backgroundVideoID)
	})
}

func TestDisablingTabsReclassifiesColdBlockedYouTubeItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "browser", AppID: "msedge", Title: "Video", Playing: true, LastUpdated: now}
		e, resolver := newArtworkTestEngine(t, track)
		e.Cfg.Get().Sources.Blocked = []string{"youtube"}
		e.EnumerateWindows = func() []browserwin.Window { return []browserwin.Window{{HWND: 1, Title: "Unrelated - Microsoft Edge"}} }
		reads := 0
		e.ReadTabNames = func(uintptr, int) ([]string, error) { reads++; return []string{"Video - YouTube - Audio playing"}, nil }
		e.ReadAddressBar = func(uintptr) (string, error) { t.Fatal("background address read"); return "", nil }
		e.step(now)
		nextArtworkRequest(t, resolver, track.Title).result <- art.Lookup{}
		synctest.Wait()
		e.step(now.Add(7 * time.Second))
		if e.cur != nil || len(e.ipc.(*recordingActivityClient).activities) != 0 {
			t.Fatal("blocked YouTube item was published")
		}
		e.Cfg.Get().Sources.Blocked = []string{"youtube", "youtube_tabs"}
		e.step(now.Add(14 * time.Second))
		nextArtworkRequest(t, resolver, track.Title).result <- art.Lookup{ImageURL: "https://example.test/browser.jpg"}
		synctest.Wait()
		e.step(now.Add(21 * time.Second))
		frames := e.ipc.(*recordingActivityClient).activities
		if e.cur == nil || e.cur.Service != "" || len(frames) != 1 || frames[0] == nil || frames[0].Name == "YouTube" {
			t.Fatal("disabled discovery left browser item suppressed or leaked a YouTube card")
		}
		if reads != 1 {
			t.Fatalf("scanned disabled tabs: %d", reads)
		}
	})
}
