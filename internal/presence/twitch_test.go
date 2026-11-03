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

type twitchArtwork struct {
	*queuedArtwork
	previewCalls int
}

func (r *twitchArtwork) TwitchThumbnail(login string) (string, string) {
	r.previewCalls++
	return art.TwitchPreviewURL(login), "twitch"
}

func TestTwitchChannelResolution(t *testing.T) {
	for _, mode := range []string{"live", "ambiguous", "blocked", "vod", "changed"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", Title: "Ranked grind", Artist: "Streamer", Playing: true, LastUpdated: now}
				e, queued := newArtworkTestEngine(t, track)
				artwork := &twitchArtwork{queuedArtwork: queued}
				e.Art = artwork
				e.Cfg.Get().Sources.Blocked = nil
				if mode == "blocked" {
					e.Cfg.Get().Sources.Blocked = []string{"twitch"}
				}
				scans, reads := 0, 0
				e.EnumerateWindows = func() []browserwin.Window {
					scans++
					title := "streamer - Twitch - Microsoft​ Edge"
					if scans > 1 && mode == "changed" {
						title = "other - Twitch - Microsoft​ Edge"
					}
					if mode == "ambiguous" {
						return []browserwin.Window{{HWND: 42, Title: title}, {HWND: 43, Title: title}}
					}
					return []browserwin.Window{{HWND: 42, Title: title}}
				}
				e.ReadAddressBar = func(hwnd uintptr) (string, error) {
					reads++
					if mode == "blocked" || mode == "ambiguous" {
						t.Fatal("address bar read without a unique, allowed match")
					}
					if mode == "vod" {
						return "https://www.twitch.tv/videos/123456", nil
					}
					return "https://www.twitch.tv/streamer", nil
				}
				e.step(now)
				nextArtworkRequest(t, queued, "Ranked grind").result <- art.Lookup{}
				synctest.Wait()
				e.step(now.Add(7 * time.Second))
				c := e.browserCache[track.Identity()]
				if c.Service != "twitch" {
					t.Fatalf("not classified as Twitch: %+v", c)
				}
				client := e.ipc.(*recordingActivityClient)
				if mode == "blocked" {
					if len(client.activities) != 0 {
						t.Fatal("blocked Twitch track published")
					}
					return
				}
				a := client.activities[0]
				if a.Type != 3 || a.Name != "Twitch" {
					t.Fatalf("want Watching Twitch, got %+v", a)
				}
				if mode == "live" {
					want := art.TwitchPreviewURL("streamer")
					if c.Channel != "streamer" || artwork.previewCalls != 1 || a.Assets.LargeImage != want || e.Status().Via != "twitch" {
						t.Fatalf("missing live preview: %+v %+v", c, a.Assets)
					}
					if len(a.Buttons) == 0 || a.Buttons[0].Label != "Watch on Twitch" || a.Buttons[0].URL != "https://www.twitch.tv/streamer" {
						t.Fatalf("missing watch button: %+v", a.Buttons)
					}
					return
				}
				if c.Channel != "" || artwork.previewCalls != 0 || a.Assets.LargeImage != art.TwitchSymbolURL {
					t.Fatalf("channel should be discarded: %+v %+v", c, a.Assets)
				}
				for _, b := range a.Buttons {
					if b.Label != "Yozora" {
						t.Fatal("unexpected watch button", b)
					}
				}
			})
		})
	}
}

func TestYouTubeVideoNamedTwitchStaysYouTube(t *testing.T) {
	titles := []string{"Clip - Twitch - YouTube - Microsoft​ Edge"}
	if _, n := browserwin.MatchYouTubeTitle(titles, "Clip - Twitch"); n != 1 {
		t.Fatal("YouTube match lost")
	}
	if _, n := browserwin.MatchTwitchTitle(titles, "Clip - Twitch", ""); n != 0 {
		t.Fatal("YouTube title classified as Twitch")
	}
}
