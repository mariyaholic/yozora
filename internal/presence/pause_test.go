//go:build windows

package presence

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	"uika-resonance/internal/smtc"
)

func pauseEngine(t *testing.T, tracks ...player.Track) (*Engine, *reconnectClient) {
	t.Helper()
	h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	client := &reconnectClient{alive: true}
	e := &Engine{Cfg: h, lastSMTCSessions: tracks}
	e.dial = func(string) (activityClient, error) { return client, nil }
	return e, client
}

func lastSent(c *reconnectClient) string {
	if len(c.sent) == 0 {
		return "none"
	}
	if c.sent[len(c.sent)-1] == nil {
		return "cleared"
	}
	return c.sent[len(c.sent)-1].Details
}

func TestPauseClearsPublishesNothingAndResumeRepublishes(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "song", Playing: true, LastUpdated: now}
	e, client := pauseEngine(t, track)
	e.step(now)
	e.step(now.Add(time.Second))
	if lastSent(client) != "song" {
		t.Fatalf("track not published: %s", lastSent(client))
	}

	// Inside the 5 s send gate the card stays up; the clear follows the gate, not a later step.
	e.Pause(time.Time{})
	if st := e.Status(); !st.Paused || st.PausedUntil != nil {
		t.Fatalf("status = paused %v until %v", st.Paused, st.PausedUntil)
	}
	e.step(now.Add(2 * time.Second))
	if lastSent(client) != "song" {
		t.Fatalf("cleared inside the send gate: %s", lastSent(client))
	}
	e.step(now.Add(7 * time.Second))
	if lastSent(client) != "cleared" || e.Status().Published != nil {
		t.Fatalf("pause did not clear Discord: %s", lastSent(client))
	}
	sends := len(client.sent)

	// A new track while paused is detected but never sent.
	e.mu.Lock()
	e.lastSMTCSessions = []player.Track{{Source: "spotify", Player: "Spotify", Title: "next", Playing: true, LastUpdated: now}}
	e.mu.Unlock()
	for at := 8 * time.Second; at < 3*time.Minute; at += 250 * time.Millisecond {
		e.step(now.Add(at))
	}
	if len(client.sent) != sends {
		t.Fatalf("published while paused: %+v", client.sent[sends:])
	}
	if st := e.Status(); st.Title != "next" {
		t.Fatalf("preview stopped following the player: %q", st.Title)
	}

	e.Resume()
	if e.Status().Paused {
		t.Fatal("still paused after Resume")
	}
	e.step(now.Add(3*time.Minute + time.Second))
	if lastSent(client) != "next" {
		t.Fatalf("resume did not republish the current track: %s", lastSent(client))
	}
}

func TestPauseExpiryRepublishes(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "song", Playing: true, LastUpdated: now}
	e, client := pauseEngine(t, track)
	e.step(now)
	e.step(now.Add(time.Second))
	until := now.Add(time.Minute)
	e.Pause(until)
	e.step(now.Add(10 * time.Second))
	if lastSent(client) != "cleared" {
		t.Fatalf("not cleared: %s", lastSent(client))
	}
	e.step(until.Add(-time.Second))
	if lastSent(client) != "cleared" {
		t.Fatalf("published before the timer ran out: %s", lastSent(client))
	}
	e.step(until)
	if lastSent(client) != "song" || e.pause.active {
		t.Fatalf("expiry did not republish: %s", lastSent(client))
	}
}

// Resuming inside the send gate must wait for the gate, as any other update does.
func TestResumeDuringSendGateWaitsForGate(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "song", Playing: true, LastUpdated: now}
	e, client := pauseEngine(t, track)
	e.step(now)
	e.step(now.Add(time.Second))
	e.Pause(time.Time{})
	e.step(now.Add(6 * time.Second)) // clears at 6 s
	if lastSent(client) != "cleared" {
		t.Fatal("not cleared")
	}
	e.Resume()
	e.step(now.Add(8 * time.Second))
	if lastSent(client) != "cleared" {
		t.Fatalf("resume bypassed the send gate: %s", lastSent(client))
	}
	e.step(now.Add(11 * time.Second))
	if lastSent(client) != "song" {
		t.Fatalf("not republished once the gate passed: %s", lastSent(client))
	}
}

// Pausing and resuming before the clear happened must not resend an unchanged card.
func TestResumeBeforeClearSendsNothing(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "song", Playing: true, LastUpdated: now}
	e, client := pauseEngine(t, track)
	e.step(now)
	e.step(now.Add(time.Second))
	n := len(client.sent)
	e.Pause(time.Time{})
	e.step(now.Add(2 * time.Second))
	e.Resume()
	e.step(now.Add(3 * time.Second))
	e.step(now.Add(30 * time.Second))
	if len(client.sent) != n {
		t.Fatalf("extra sends: %+v", client.sent[n:])
	}
}

func TestPauseWithNothingPublishedDoesNotTouchDiscord(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	e, client := pauseEngine(t)
	dialed := false
	e.dial = func(string) (activityClient, error) { dialed = true; return client, nil }
	e.Pause(time.Time{})
	e.step(now)
	if dialed || len(client.sent) != 0 {
		t.Fatalf("paused idle engine contacted Discord: dialed=%v sent=%d", dialed, len(client.sent))
	}
}

func TestPauseEndsTestCardAndRefusesAnother(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	e, client := pauseEngine(t)
	if err := e.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	e.step(now)
	if !isTestCard(client.sent[len(client.sent)-1]) {
		t.Fatal("test card not shown")
	}
	e.Pause(time.Time{})
	e.step(now.Add(6 * time.Second))
	if lastSent(client) != "cleared" {
		t.Fatalf("test card survived the pause: %s", lastSent(client))
	}
	if err := e.SendTestCard(); !errors.Is(err, ErrPaused) {
		t.Fatalf("test card while paused: %v", err)
	}
}

func TestPausedStatusReportsEndTime(t *testing.T) {
	e, _ := pauseEngine(t)
	until := time.Now().Add(time.Hour)
	e.Pause(until)
	st := e.Status()
	if !st.Paused || st.PausedUntil == nil || !st.PausedUntil.Equal(until) {
		t.Fatalf("status = %v %v", st.Paused, st.PausedUntil)
	}
	e.Pause(time.Now().Add(-time.Second))
	if e.Status().Paused {
		t.Fatal("an expired pause still reads as paused")
	}
}

// The idle backoff must not sleep through a timed pause's end.
func TestIdleBackoffDoesNotDelayPauseExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
		if err != nil {
			t.Fatal(err)
		}
		client := &reconnectClient{alive: true}
		smtcCh := make(chan []smtc.Track)
		e := &Engine{Cfg: h, smtcCh: smtcCh, spotifyCh: make(chan player.Track)}
		e.dial = func(string) (activityClient, error) { return client, nil }
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		every := func() time.Duration { synctest.Wait(); return time.Duration(e.tickEvery.Load()) }

		time.Sleep(90 * time.Second)
		if every() != tickIdle {
			t.Fatal("engine did not go idle")
		}
		until := time.Now().Add(10 * time.Minute)
		e.Pause(until)
		time.Sleep(5 * time.Minute)
		if every() != tickIdle {
			t.Fatal("a distant pause expiry kept the engine at full speed")
		}
		time.Sleep(5*time.Minute - time.Second)
		if every() != tickActive {
			t.Fatalf("not at full speed near the expiry: %s", every())
		}
		time.Sleep(time.Second + tickActive)
		synctest.Wait()
		if e.Status().Paused || e.pause.active {
			t.Fatal("pause not ended within one active tick of its expiry")
		}
		cancel()
		<-done
	})
}
