//go:build windows

package presence

import (
	"context"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	"uika-resonance/internal/smtc"
)

func TestIdleBackoffEntersAfterQuietPeriodAndLeavesAtOnce(t *testing.T) {
	b := idleBackoff{active: time.Second, idle: 5 * time.Second, after: time.Minute}
	t0 := time.Unix(1000, 0)
	if d, changed := b.observe(t0, false); d != time.Second || changed {
		t.Fatalf("first quiet poll: %s %v", d, changed)
	}
	if d, changed := b.observe(t0.Add(59*time.Second), false); d != time.Second || changed {
		t.Fatalf("slowed before the quiet period: %s %v", d, changed)
	}
	if d, changed := b.observe(t0.Add(60*time.Second), false); d != 5*time.Second || !changed {
		t.Fatalf("did not slow after the quiet period: %s %v", d, changed)
	}
	if d, changed := b.observe(t0.Add(65*time.Second), false); d != 5*time.Second || changed {
		t.Fatalf("idle poll changed rate: %s %v", d, changed)
	}
	if d, changed := b.observe(t0.Add(66*time.Second), true); d != time.Second || !changed {
		t.Fatalf("activity did not restore full speed: %s %v", d, changed)
	}
	// Activity restarts the quiet period rather than resuming the old one.
	if d, _ := b.observe(t0.Add(67*time.Second), false); d != time.Second {
		t.Fatal("quiet period was not restarted")
	}
	if d, _ := b.observe(t0.Add(126*time.Second), false); d != time.Second {
		t.Fatal("slowed before a full new quiet period")
	}
	if d, _ := b.observe(t0.Add(127*time.Second), false); d != 5*time.Second {
		t.Fatal("did not slow after a full new quiet period")
	}

	off := idleBackoff{active: 5 * time.Second, idle: 2 * time.Second, after: time.Minute}
	off.observe(t0, false)
	if d, changed := off.observe(t0.Add(time.Hour), false); d != 5*time.Second || changed {
		t.Fatal("an idle rate faster than the active one was applied")
	}
}

func TestRunBacksOffWhenIdleAndWakesOnNewMedia(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		holder, err := config.NewHolder(filepath.Join(t.TempDir(), "cadence.toml"))
		if err != nil {
			t.Fatal(err)
		}
		smtcCh := make(chan []smtc.Track)
		spotifyCh := make(chan player.Track)
		client := &reconnectClient{alive: true}
		e := &Engine{Cfg: holder, smtcCh: smtcCh, spotifyCh: spotifyCh}
		e.dial = func(string) (activityClient, error) { return client, nil }
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		every := func() time.Duration { synctest.Wait(); return time.Duration(e.tickEvery.Load()) }

		time.Sleep(59 * time.Second)
		if got := every(); got != tickActive {
			t.Fatalf("backed off before a quiet minute: %s", got)
		}
		time.Sleep(2 * time.Second)
		if got := every(); got != tickIdle {
			t.Fatalf("no backoff after a quiet minute: %s", got)
		}

		// The Web API's "nothing playing" report is not activity.
		spotifyCh <- player.Track{Source: "spotifyapi", Player: "Spotify", LastUpdated: time.Now()}
		if got := every(); got != tickIdle {
			t.Fatalf("untitled Spotify report woke the engine: %s", got)
		}

		smtcCh <- []smtc.Track{{AppID: "Spotify.exe", Title: "Song", Artist: "Artist", Playing: true, LastUpdated: time.Now()}}
		if got := every(); got != tickActive {
			t.Fatalf("new media did not restore full speed: %s", got)
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if len(client.sent) == 0 || client.sent[len(client.sent)-1] == nil {
			t.Fatal("track was not published within two seconds of waking")
		}

		// The player closes, but its card stays up until the idle clear, so the
		// engine keeps full speed and the clear is not delayed.
		smtcCh <- nil
		time.Sleep(80 * time.Second)
		if got := every(); got != tickActive {
			t.Fatalf("backed off with a card still published: %s", got)
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if client.sent[len(client.sent)-1] != nil {
			t.Fatal("card was not cleared after idle_clear_secs")
		}
		time.Sleep(61 * time.Second)
		if got := every(); got != tickIdle {
			t.Fatalf("no backoff a minute after the clear: %s", got)
		}

		cancel()
		<-done
	})
}
