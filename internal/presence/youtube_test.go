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

func TestBrowserClassification(t *testing.T) {
	for _, mode := range []string{"blocked", "enabled", "thumbnail", "plain", "nonbrowser", "fallback", "published", "duplicate-blocked", "duplicate-enabled", "duplicate-published", "edge-single-page"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "browser", Title: "Video", Player: "Browser", Playing: true, LastUpdated: now}
				if mode == "nonbrowser" {
					track.Source = "spotify"
				}
				e, resolver := newArtworkTestEngine(t, track)
				cfg := e.Cfg.Get()
				cfg.Sources.Blocked = []string{"youtube"}
				cfg.Sources.Order = []string{"browser", "spotify"}
				if mode == "enabled" || mode == "thumbnail" || mode == "duplicate-enabled" || mode == "edge-single-page" {
					cfg.Sources.Blocked = nil
				}
				client := e.ipc.(*recordingActivityClient)
				calls := 0
				e.EnumerateWindows = func() []browserwin.Window {
					calls++
					title := "Video - YouTube - Google Chrome"
					if mode == "plain" {
						title = "Other - YouTube - Google Chrome"
					}
					if mode == "edge-single-page" {
						title = "Video - YouTube and 1 more page - Personal - Microsoft\u200b Edge"
					}
					if mode == "duplicate-blocked" || mode == "duplicate-enabled" || mode == "duplicate-published" {
						return []browserwin.Window{{HWND: 1, Title: title}, {HWND: 2, Title: "Video - YouTube - Microsoft Edge"}}
					}
					return []browserwin.Window{{HWND: 1, Title: title}}
				}
				if mode == "fallback" {
					e.lastSMTCSessions = append(e.lastSMTCSessions, player.Track{Source: "spotify", Title: "Song", Playing: true, LastUpdated: now})
				}
				if mode == "published" || mode == "duplicate-published" {
					old := track
					e.publishedTrack = &old
					e.sentID = track.IdentityPlaying()
				}
				e.step(now)
				request := nextArtworkRequest(t, resolver, track.Title)
				// The title result is not yet committed; no browser frame may escape.
				e.step(now.Add(2 * time.Second))
				if len(client.activities) != 0 && mode != "nonbrowser" {
					t.Fatal("browser sent while classification pending")
				}
				lookup := art.Lookup{}
				if mode == "thumbnail" {
					lookup.ImageURL = "http://127.0.0.1:49152/art/thumb.jpg"
					lookup.Via = "smtc"
				}
				request.result <- lookup
				synctest.Wait()
				e.step(now.Add(7 * time.Second))
				if mode == "fallback" {
					req := nextArtworkRequest(t, resolver, "Song")
					req.result <- art.Lookup{}
					synctest.Wait()
					e.step(now.Add(8 * time.Second))
				}
				switch mode {
				case "blocked", "duplicate-blocked":
					if len(client.activities) != 0 {
						t.Fatal("blocked YouTube published")
					}
				case "published", "duplicate-published":
					if len(client.activities) != 1 || client.activities[0] != nil {
						t.Fatal("published blocked browser not cleared")
					}
				case "fallback":
					if len(client.activities) != 1 || client.activities[0] == nil || client.activities[0].Name != "Spotify" {
						t.Fatal("no fallback")
					}
				case "enabled", "thumbnail", "duplicate-enabled", "edge-single-page":
					if len(client.activities) != 1 {
						t.Fatalf("frames: %d", len(client.activities))
					}
					a := client.activities[0]
					wantVia := "symbol"
					if mode == "thumbnail" {
						wantVia = "smtc"
					}
					if e.Status().Via != wantVia {
						t.Fatalf("Via = %q, want %q", e.Status().Via, wantVia)
					}
					dashboard := art.YouTubeSymbolURL
					if mode == "thumbnail" {
						dashboard = lookup.ImageURL
					}
					if a.Name != "YouTube" || a.Type != 3 || a.Assets.LargeImage != art.YouTubeSymbolURL || e.Status().ArtURL != dashboard {
						t.Fatalf("presentation: %+v", a)
					}
					for _, b := range a.Buttons {
						if b.Label != "Yozora" {
							t.Fatal("unexpected video button")
						}
					}
				case "plain":
					if len(client.activities) != 1 || client.activities[0].Name != "Browser" || client.activities[0].Type != 2 {
						t.Fatal("plain browser changed")
					}
				case "nonbrowser":
					if calls != 0 {
						t.Fatal("enumerated nonbrowser")
					}
				}
				if mode != "nonbrowser" && calls != 1 {
					t.Fatalf("enumerations: %d", calls)
				}
			})
		})
	}
}

func TestYouTubeMusicURL(t *testing.T) {
	for _, id := range []string{"", "abcdefghij", "abcdefghijkl", "abcdefghij!", "Abc_1234-xy"} {
		got := musicURL(&player.Track{Source: "browser", Service: "youtube", VideoID: id, ListenURL: "https://example.test"}, "", true)
		if id == "Abc_1234-xy" {
			if got != "https://www.youtube.com/watch?v="+id {
				t.Fatal(got)
			}
		} else if got != "" {
			t.Fatal(got)
		}
	}
}

func TestYouTubeButtonLabel(t *testing.T) {
	for _, label := range []string{"", "Listen along", "Custom"} {
		track := player.Track{Source: "browser", Service: "youtube", VideoID: "Abc_1234-xy", Title: "Video", Playing: true}
		e, _ := newArtworkTestEngine(t, track)
		e.Cfg.Get().Buttons.Label = label
		e.Cfg.Get().Sources.Blocked = nil
		e.send(&track, time.Now())
		a := e.ipc.(*recordingActivityClient).activities[0]
		want := "Watch on YouTube"
		if label == "Custom" {
			want = label
		}
		if len(a.Buttons) == 0 || a.Buttons[0].Label != want || a.Buttons[0].URL != "https://www.youtube.com/watch?v=Abc_1234-xy" {
			t.Fatalf("buttons: %+v", a.Buttons)
		}
	}
}

func TestBlockedBrowserDoesNotEnumerate(t *testing.T) {
	track := player.Track{Source: "browser", Title: "Video", Playing: true, LastUpdated: time.Now()}
	e, _ := newArtworkTestEngine(t, track)
	e.EnumerateWindows = func() []browserwin.Window {
		t.Error("enumerated blocked browser")
		return nil
	}
	e.step(time.Now())
	if e.cur != nil {
		t.Fatal("blocked browser selected")
	}
}

func TestYouTubeSendRechecksBlocking(t *testing.T) {
	track := player.Track{Source: "browser", Title: "Video", Playing: true, LastUpdated: time.Now()}
	e, _ := newArtworkTestEngine(t, track)
	e.Cfg.Get().Sources.Blocked = nil
	e.browserCache = map[string]browserClassification{track.Identity(): {Service: "youtube"}}
	picked := e.pick(time.Now())
	if picked == nil || picked.Service != "youtube" {
		t.Fatal("YouTube not picked")
	}
	e.Cfg.Get().Sources.Blocked = []string{"youtube"}
	e.send(picked, time.Now())
	if len(e.ipc.(*recordingActivityClient).activities) != 0 {
		t.Fatal("blocked track sent")
	}
}

func TestYouTubePublishedSnapshotRevocation(t *testing.T) {
	track := player.Track{Source: "browser", Service: "youtube", Title: "Video", Playing: true}
	e, _ := newArtworkTestEngine(t, track)
	e.Cfg.Get().Sources.Blocked = nil
	e.send(&track, time.Now())
	e.browserCache = nil
	e.Cfg.Get().Sources.Blocked = []string{"youtube"}
	e.step(time.Now().Add(10 * time.Second))
	frames := e.ipc.(*recordingActivityClient).activities
	if len(frames) != 2 || frames[0] == nil || frames[1] != nil {
		t.Fatalf("frames: %+v", frames)
	}
}

func TestYouTubeActivityName(t *testing.T) {
	track := player.Track{Source: "browser", Service: "youtube", Player: "Browser"}
	e, _ := newArtworkTestEngine(t, track)
	cfg := e.Cfg.Get()
	cfg.Discord.Names = map[string]string{"browser": "Custom browser"}
	if got := e.activityName(cfg, &track); got != "YouTube" {
		t.Fatal(got)
	}
	cfg.Discord.Names["youtube"] = "Custom YouTube"
	if got := e.activityName(cfg, &track); got != "Custom YouTube" {
		t.Fatal(got)
	}
	track.Service = ""
	if got := e.activityName(cfg, &track); got != "Custom browser" {
		t.Fatal(got)
	}
}
