//go:build windows

package presence

import (
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/art"
	"uika-resonance/internal/player"
)

func TestBrowserLoopbackArtworkDoesNotFallbackToDefaultImage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "browser", Title: "fixture livestream", Artist: "fixture channel", Playing: true, LastUpdated: now}
		engine, resolver := newArtworkTestEngine(t, track)
		engine.Cfg.Get().Sources.Blocked = nil
		client := engine.ipc.(*recordingActivityClient)
		engine.Art = &defaultingArtwork{queued: resolver}
		engine.step(now)
		request := nextArtworkRequest(t, resolver, track.Title)
		request.result <- art.Lookup{ImageURL: "http://127.0.0.1:49152/art/thumb.jpg", Via: "smtc"}
		synctest.Wait()
		engine.step(now.Add(2 * time.Second))
		if len(client.activities) != 1 || client.activities[0] == nil {
			t.Fatalf("video metadata not published: %d frames", len(client.activities))
		}
		if got := client.activities[0].Assets.LargeImage; got != "" {
			t.Fatalf("video card fell back to default image: %q", got)
		}

		if got := engine.Status().ArtURL; got != "http://127.0.0.1:49152/art/thumb.jpg" {
			t.Fatalf("dashboard lost the real thumbnail: %q", got)
		}
	})
}

func TestPublicArtworkStillResolvesToDefaultWhenMissing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "spotify", Title: "fixture song", Artist: "fixture artist", Playing: true, LastUpdated: now}
		engine, resolver := newArtworkTestEngine(t, track)
		engine.Art = &defaultingArtwork{queued: resolver}
		client := engine.ipc.(*recordingActivityClient)
		engine.step(now)
		request := nextArtworkRequest(t, resolver, track.Title)
		request.result <- art.Lookup{}
		synctest.Wait()
		if got := engine.Status().ArtURL; got != "https://fixture.invalid/default.png" {
			t.Fatalf("music fallback artwork = %q, want default cover", got)
		}
		engine.step(now.Add(2 * time.Second))
		if len(client.activities) != 1 || client.activities[0] == nil {
			t.Fatalf("music metadata not published: %d frames", len(client.activities))
		}
		if got := client.activities[0].Assets.LargeImage; got != "https://fixture.invalid/default.png" {
			t.Fatalf("music card lost default cover: %q", got)
		}
	})
}

type defaultingArtwork struct {
	queued *queuedArtwork
}

func (d *defaultingArtwork) Resolve(track art.Track, prefer string) art.Lookup {
	base := d.queued.Resolve(track, prefer)
	if base.Via == "" && base.ImageURL == "" {
		return art.Lookup{ImageURL: "https://fixture.invalid/default.png", Via: "default"}
	}
	return base
}

func (d *defaultingArtwork) DefaultURL() string { return d.queued.DefaultURL() }
