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

func TestYouTubeTabsResolution(t *testing.T) {
	for _, mode := range []string{"enabled", "prefixed", "duplicates", "blocked", "duplicates-blocked", "off", "off-normalized", "title", "error", "bound", "total-cap", "cap-edge", "multiwindow", "nonbrowser"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", Title: "Video", Playing: true, LastUpdated: now}
				if mode == "nonbrowser" {
					track.Source = "spotify"
				}
				e, resolver := newArtworkTestEngine(t, track)
				e.Cfg.Get().Sources.Blocked = nil
				if mode == "blocked" || mode == "duplicates-blocked" {
					e.Cfg.Get().Sources.Blocked = []string{"youtube"}
				}
				if mode == "off" {
					e.Cfg.Get().Sources.Blocked = []string{"youtube_tabs"}
				}
				if mode == "off-normalized" {
					e.Cfg.Get().Sources.Blocked = []string{" YouTube_Tabs "}
				}
				scans, reads := 0, 0
				e.EnumerateWindows = func() []browserwin.Window {
					scans++
					windows := []browserwin.Window{{HWND: 1, Title: "Unrelated - Microsoft Edge"}}
					if mode == "title" {
						windows[0].Title = "Video - YouTube - Microsoft Edge"
					}
					if mode == "bound" || mode == "total-cap" || mode == "cap-edge" || mode == "multiwindow" {
						for i := 2; i <= 9; i++ {
							windows = append(windows, browserwin.Window{HWND: uintptr(i)})
						}
					}
					return windows
				}
				e.ReadAddressBar = func(uintptr) (string, error) {
					if mode != "title" {
						t.Fatal("tab match read address bar")
					}
					return "", nil
				}
				e.ReadTabNames = func(hwnd uintptr, limit int) ([]string, error) {
					reads++
					if mode == "off" || mode == "off-normalized" || mode == "title" || mode == "nonbrowser" {
						t.Fatal("unexpected tab reader call")
					}
					if mode == "error" {
						return nil, errors.New("unavailable")
					}
					if mode == "bound" && hwnd < 9 {
						return nil, nil
					}
					const match = "Video - YouTube - Audio playing"
					if mode == "total-cap" || mode == "cap-edge" {
						want := 200
						if hwnd == 2 {
							want = 50
						}
						if hwnd > 2 || limit != want {
							t.Fatalf("window %d: limit %d, want %d", hwnd, limit, want)
						}
						if hwnd == 1 {
							return make([]string, 150), nil
						}
						names := make([]string, limit)
						if mode == "cap-edge" {
							names[limit-1] = match
						} else {
							// An over-returning injectable reader must not extend the scan budget.
							names = append(names, match)
						}
						return names, nil
					}
					if mode == "prefixed" {
						return []string{"(3) " + match}, nil
					}
					if mode == "duplicates" || mode == "duplicates-blocked" {
						return []string{match, match + " - Memory usage - 317 MB"}, nil
					}
					return []string{match}, nil
				}
				e.step(now)
				request := nextArtworkRequest(t, resolver, "Video")
				request.result <- art.Lookup{ImageURL: "https://example.test/media-thumbnail.jpg"}
				synctest.Wait()
				e.step(now.Add(7 * time.Second))
				client := e.ipc.(*recordingActivityClient)
				if mode == "blocked" || mode == "duplicates-blocked" {
					if len(client.activities) != 0 {
						t.Fatal("blocked tab published")
					}
					return
				}
				if len(client.activities) != 1 {
					t.Fatalf("activities: %d", len(client.activities))
				}
				a := client.activities[0]
				if (mode == "enabled" || mode == "prefixed" || mode == "duplicates" || mode == "multiwindow" || mode == "cap-edge") && e.Status().ArtURL != art.YouTubeSymbolURL {
					t.Fatal("tab match retained thumbnail")
				}
				if mode == "enabled" || mode == "prefixed" || mode == "duplicates" || mode == "multiwindow" || mode == "cap-edge" || mode == "title" {
					if a.Type != 3 || a.Assets.LargeImage != art.YouTubeSymbolURL || e.browserCache[track.Identity()].VideoID != "" {
						t.Fatalf("wrong presentation: %+v", a)
					}
					for _, b := range a.Buttons {
						if b.Label != "Yozora" {
							t.Fatal("unexpected button")
						}
					}
				} else if e.browserCache[track.Identity()].Service != "" {
					t.Fatal("unexpected YouTube classification")
				}
				if mode == "bound" && reads != 8 {
					t.Fatalf("read %d windows", reads)
				}
				if (mode == "total-cap" || mode == "cap-edge") && reads != 2 {
					t.Fatalf("read %d windows, want 2", reads)
				}
				if mode == "multiwindow" && reads != 1 {
					t.Fatalf("continued after first matching window: %d reads", reads)
				}
				if mode != "nonbrowser" && scans != 1 {
					t.Fatalf("enumerations: %d", scans)
				}
			})
		})
	}
}
