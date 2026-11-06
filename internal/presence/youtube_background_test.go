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

const backgroundVideoID = "dQw4w9WgXcQ"
const backgroundThumbnail = "https://i.ytimg.com/vi/" + backgroundVideoID + "/mqdefault.jpg"

func assertBackgroundYouTube(t *testing.T, e *Engine, wantID string) {
	t.Helper()
	client := e.ipc.(*recordingActivityClient)
	if len(client.activities) == 0 {
		t.Fatal("no published activity")
	}
	a := client.activities[len(client.activities)-1]
	if a == nil || a.Name != "YouTube" || a.Type != 3 {
		t.Fatalf("lost YouTube presentation: %+v", a)
	}
	wantImage := art.YouTubeSymbolURL
	if wantID != "" {
		wantImage = "https://i.ytimg.com/vi/" + wantID + "/mqdefault.jpg"
	}
	if a.Assets.LargeImage != wantImage {
		t.Fatalf("image = %q, want %q", a.Assets.LargeImage, wantImage)
	}
	for _, b := range a.Buttons {
		if b.Label == "Watch on YouTube" {
			if wantID == "" || b.URL != "https://www.youtube.com/watch?v="+wantID {
				t.Fatalf("stale watch button: %+v", b)
			}
			return
		}
	}
	if wantID != "" {
		t.Fatal("lost verified watch button")
	}
}

func TestYouTubeBackgroundRetainsVerifiedVideoAcrossPauseResume(t *testing.T) {
	for _, mode := range []string{"tabs-enabled", "tabs-disabled", "tab-read-error", "hide-when-paused"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", AppID: "msedge", Title: "Video", Playing: true, LastUpdated: now}
				e, queued := newArtworkTestEngine(t, track)
				artwork := &youtubeArtwork{queuedArtwork: queued}
				e.Art = artwork
				e.Cfg.Get().Sources.Blocked = nil
				if mode == "tabs-disabled" {
					e.Cfg.Get().Sources.Blocked = []string{"youtube_tabs"}
				}
				if mode == "hide-when-paused" {
					e.Cfg.Get().Presence.PausedMode = "hide"
				}
				foreground, addressReads, tabReads := true, 0, 0
				e.EnumerateWindows = func() []browserwin.Window {
					title := "Unrelated - Microsoft Edge"
					if foreground {
						title = "Video - YouTube - Microsoft Edge"
					}
					return []browserwin.Window{{HWND: 42, Title: title}}
				}
				e.ReadAddressBar = func(uintptr) (string, error) {
					addressReads++
					if !foreground {
						t.Error("read unrelated foreground tab's address bar")
					}
					return "https://www.youtube.com/watch?v=" + backgroundVideoID, nil
				}
				e.ReadTabNames = func(uintptr, int) ([]string, error) {
					tabReads++
					if mode == "tabs-disabled" {
						t.Error("read tabs while disabled")
					}
					if mode == "tab-read-error" {
						return nil, errors.New("unavailable")
					}
					if !e.lastSMTCSessions[0].Playing {
						return []string{"Video - YouTube"}, nil
					}
					return []string{"Video - YouTube - Audio playing"}, nil
				}
				e.step(now)
				nextArtworkRequest(t, queued, "Video").result <- art.Lookup{}
				synctest.Wait()
				e.step(now.Add(time.Second))
				assertBackgroundYouTube(t, e, backgroundVideoID)
				foreground = false
				e.step(now.Add(6 * time.Second))
				if artwork.thumbnailCalls != 1 || len(e.ipc.(*recordingActivityClient).activities) != 1 {
					t.Fatal("backgrounding alone replaced the card")
				}
				for i, playing := range []bool{false, true} {
					at := now.Add(time.Duration(7+i*7) * time.Second)
					e.lastSMTCSessions[0].Playing = playing
					e.step(at)
					nextArtworkRequest(t, queued, "Video").result <- art.Lookup{}
					synctest.Wait()
					e.step(at.Add(time.Second))
					if !playing && mode == "hide-when-paused" {
						frames := e.ipc.(*recordingActivityClient).activities
						if frames[len(frames)-1] != nil {
							t.Fatal("retention overrode paused hide policy")
						}
					} else {
						assertBackgroundYouTube(t, e, backgroundVideoID)
					}
					if e.Status().ArtURL != backgroundThumbnail {
						t.Fatalf("dashboard lost thumbnail: %q", e.Status().ArtURL)
					}
				}
				if addressReads != 1 || artwork.thumbnailCalls != 1 {
					t.Fatalf("unnecessary URL/thumbnail reread: %d/%d", addressReads, artwork.thumbnailCalls)
				}
				if mode == "tabs-disabled" && tabReads != 0 {
					t.Fatal("disabled tabs were scanned")
				}
				// Revoking YouTube wins over retained evidence at the normal rate gate.
				e.Cfg.Get().Sources.Blocked = []string{"youtube"}
				e.step(now.Add(23 * time.Second))
				frames := e.ipc.(*recordingActivityClient).activities
				if frames[len(frames)-1] != nil {
					t.Fatal("retained video survived source revocation")
				}
			})
		})
	}
}

func TestYouTubeBackgroundDoesNotReuseVideoAfterSelectionBreak(t *testing.T) {
	for _, mode := range []string{"gap", "different-title", "different-browser", "ambiguous-foreground", "new-foreground-id"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", AppID: "msedge", Title: "Video", Playing: true, LastUpdated: now}
				e, queued := newArtworkTestEngine(t, track)
				artwork := &youtubeArtwork{queuedArtwork: queued}
				e.Art = artwork
				e.Cfg.Get().Sources.Blocked = nil
				first := true
				e.EnumerateWindows = func() []browserwin.Window {
					if first || mode == "new-foreground-id" {
						return []browserwin.Window{{HWND: 42, Title: "Video - YouTube - Microsoft Edge"}}
					}
					if mode == "ambiguous-foreground" {
						return []browserwin.Window{{HWND: 42, Title: "Video - YouTube - Microsoft Edge"}, {HWND: 43, Title: "Video - YouTube - Microsoft Edge"}}
					}
					return []browserwin.Window{{HWND: 42, Title: "Unrelated - Microsoft Edge"}}
				}
				e.ReadAddressBar = func(uintptr) (string, error) {
					id := backgroundVideoID
					if !first {
						if mode != "new-foreground-id" {
							t.Error("unexpected foreground URL read")
						}
						id = "Abc_1234-xy"
					}
					return "https://www.youtube.com/watch?v=" + id, nil
				}
				e.ReadTabNames = func(uintptr, int) ([]string, error) {
					return []string{e.lastSMTCSessions[0].Title + " - YouTube - Audio playing"}, nil
				}
				e.step(now)
				nextArtworkRequest(t, queued, "Video").result <- art.Lookup{}
				synctest.Wait()
				e.step(now.Add(time.Second))
				assertBackgroundYouTube(t, e, backgroundVideoID)
				first = false
				switch mode {
				case "gap":
					e.lastSMTCSessions = nil
					e.step(now.Add(6 * time.Second))
					e.lastSMTCSessions = []player.Track{track}
				case "different-title":
					e.lastSMTCSessions[0].Title = "Other video"
				case "different-browser":
					e.lastSMTCSessions[0].AppID = "chrome"
				default:
					e.lastSMTCSessions[0].Playing = false
				}
				e.step(now.Add(7 * time.Second))
				nextArtworkRequest(t, queued, e.lastSMTCSessions[0].Title).result <- art.Lookup{}
				synctest.Wait()
				e.step(now.Add(8 * time.Second))
				wantID := ""
				if mode == "new-foreground-id" {
					wantID = "Abc_1234-xy"
				}
				assertBackgroundYouTube(t, e, wantID)
				if mode != "new-foreground-id" && artwork.thumbnailCalls != 1 {
					t.Fatal("reused old video ID after selection break")
				}
			})
		})
	}
}
