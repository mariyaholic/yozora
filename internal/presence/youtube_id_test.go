//go:build windows

package presence

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/art"
	"uika-resonance/internal/browserwin"
	"uika-resonance/internal/player"
)

type youtubeArtwork struct {
	*queuedArtwork
	thumbnailCalls int
}

func (r *youtubeArtwork) YouTubeThumbnail(id string) (string, string) {
	r.thumbnailCalls++
	return "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg", "youtube"
}

func TestYouTubeAddressBarResolution(t *testing.T) {
	for _, mode := range []string{"id", "ambiguous", "blocked", "nomatch", "changed", "differenthwnd", "error", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", Title: "Video", Playing: true, LastUpdated: now}
				e, queued := newArtworkTestEngine(t, track)
				artwork := &youtubeArtwork{queuedArtwork: queued}
				e.Art = artwork
				e.Cfg.Get().Sources.Blocked = nil
				if mode == "blocked" {
					e.Cfg.Get().Sources.Blocked = []string{"youtube"}
				}
				scans, reads := 0, 0
				e.EnumerateWindows = func() []browserwin.Window {
					scans++
					title, hwnd := "Video - YouTube - Google Chrome", uintptr(42)
					if mode == "nomatch" || (scans > 1 && mode == "changed") {
						title = "Other - YouTube - Google Chrome"
					}
					if scans > 1 && mode == "differenthwnd" {
						hwnd = 43
					}
					if mode == "ambiguous" {
						return []browserwin.Window{{HWND: hwnd, Title: title}, {HWND: 43, Title: title}}
					}
					return []browserwin.Window{{HWND: hwnd, Title: title}}
				}
				e.ReadAddressBar = func(hwnd uintptr) (string, error) {
					reads++
					if mode == "blocked" || mode == "nomatch" || mode == "ambiguous" {
						t.Fatal("UIA called without authorization/title match")
					}
					if hwnd != 42 {
						t.Fatal("wrong HWND")
					}
					if mode == "error" {
						return "", errors.New("unavailable")
					}
					if mode == "invalid" {
						return "https://evil.test/watch?v=dQw4w9WgXcQ", nil
					}
					return "https://www.youtube.com/watch?v=dQw4w9WgXcQ", nil
				}
				e.step(now)
				req := nextArtworkRequest(t, queued, "Video")
				req.result <- art.Lookup{}
				synctest.Wait()
				e.step(now.Add(7 * time.Second))
				if (mode == "blocked" || mode == "nomatch" || mode == "ambiguous") && reads != 0 {
					t.Fatal("unexpected UIA call")
				}
				if mode != "blocked" && mode != "nomatch" && mode != "ambiguous" && reads != 1 {
					t.Fatal("UIA read count", reads)
				}
				c := e.browserCache[track.Identity()]
				client := e.ipc.(*recordingActivityClient)
				if mode == "id" {
					want := "https://i.ytimg.com/vi/dQw4w9WgXcQ/mqdefault.jpg"
					if c.VideoID != "dQw4w9WgXcQ" || artwork.thumbnailCalls != 1 || len(client.activities) != 1 {
						t.Fatal("missing ID presentation")
					}
					a := client.activities[0]
					if a.Type != 3 || a.Assets.LargeImage != want || e.Status().ArtURL != want || e.Status().Via != "youtube" {
						t.Fatalf("wrong images/type: %+v", a)
					}
					if len(a.Buttons) == 0 || a.Buttons[0].Label != "Watch on YouTube" || a.Buttons[0].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
						t.Fatal("missing watch button")
					}
				} else {
					if c.VideoID != "" || artwork.thumbnailCalls != 0 {
						t.Fatal("ID should be discarded")
					}
					if mode != "blocked" && mode != "nomatch" {
						a := client.activities[0]
						if a.Type != 3 || a.Assets.LargeImage != art.YouTubeSymbolURL || e.Status().ArtURL != art.YouTubeSymbolURL {
							t.Fatal("commit-2 fallback changed")
						}
						for _, b := range a.Buttons {
							if b.Label != "Yozora" {
								t.Fatal("unexpected watch button")
							}
						}
					}
				}
			})
		})
	}
}
