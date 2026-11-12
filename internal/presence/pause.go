//go:build windows

package presence

import (
	"errors"
	"log"
	"time"
)

// ErrPaused means a test card was asked for while sharing is paused.
var ErrPaused = errors.New("sharing is paused")

// pauseState lives in memory only. A pause that outlived a restart could hide
// the user from Discord with nothing left to say why.
type pauseState struct {
	active bool
	until  time.Time // zero: until the user resumes
}

// nearExpiry is how close to a timed pause's end the engine stays at full speed,
// so the idle backoff (tickIdle) can never delay the resume.
const nearExpiry = 2 * tickIdle

// Pause stops sharing until the given time. A zero time pauses until Resume.
// Detection keeps running, so the panel preview still follows the player.
func (e *Engine) Pause(until time.Time) {
	e.mu.Lock()
	e.pause = pauseState{active: true, until: until}
	// A test card is a deliberate publish; a pause supersedes it, and the loop clears it.
	e.test.requested = false
	if e.test.card.State == "sending" || e.test.card.State == "shown" {
		e.test.card = TestCard{State: "done"}
	}
	e.mu.Unlock()
	if until.IsZero() {
		log.Println("presence: sharing paused")
	} else {
		log.Printf("presence: sharing paused until %s", until.Format(time.TimeOnly))
	}
	e.writeState()
}

// Resume ends a pause. The current track is republished by the normal step,
// so the usual send-rate gate still applies.
func (e *Engine) Resume() {
	e.mu.Lock()
	was := e.pause.active
	e.pause = pauseState{}
	e.mu.Unlock()
	if was {
		log.Println("presence: sharing resumed")
		e.writeState()
	}
}

// Paused reports whether sharing is paused at now and until when (zero: no end).
func (e *Engine) Paused(now time.Time) (bool, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pausedLocked(now)
}

func (e *Engine) pausedLocked(now time.Time) (bool, time.Time) {
	if !e.pause.active || (!e.pause.until.IsZero() && !now.Before(e.pause.until)) {
		return false, time.Time{}
	}
	return true, e.pause.until
}

// stepPause runs once per step. It ends an expired pause and, while paused,
// withdraws what is on Discord. It returns whether sharing is paused.
func (e *Engine) stepPause(now time.Time, minGap time.Duration) bool {
	e.mu.Lock()
	if !e.pause.active {
		e.mu.Unlock()
		return false
	}
	if paused, _ := e.pausedLocked(now); !paused {
		e.pause = pauseState{}
		e.mu.Unlock()
		log.Println("presence: pause ended, sharing resumed")
		e.writeState()
		return false
	}
	shown := e.published != nil
	lastSend := e.lastSend
	e.mu.Unlock()
	// Same gate as the other clears; the next step retries until it passes.
	if shown && (lastSend.IsZero() || now.Sub(lastSend) >= minGap) {
		e.clear(now, "")
	}
	return true
}

// pauseNearExpiry reports a timed pause about to end, which must not be slept through.
func (e *Engine) pauseNearExpiry(now time.Time) bool {
	return e.pause.active && !e.pause.until.IsZero() && e.pause.until.Sub(now) < nearExpiry
}
