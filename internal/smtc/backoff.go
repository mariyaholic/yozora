//go:build windows

package smtc

import "time"

// Idle polling defaults. With no media session for IdleAfter, the watcher polls
// every IdleInterval instead of every Interval, so a newly started player is
// seen within IdleInterval.
const (
	DefaultIdleInterval = 5 * time.Second
	DefaultIdleAfter    = time.Minute
)

// idleBackoff slows a poll loop once it has seen nothing for a while and returns
// to full speed on the first sign of activity.
type idleBackoff struct {
	active, idle, after time.Duration

	quietSince time.Time
	slow       bool
}

// observe records one poll result and returns the interval to poll at next and
// whether it differs from the previous one.
func (b *idleBackoff) observe(now time.Time, busy bool) (time.Duration, bool) {
	if busy || b.idle <= b.active {
		b.quietSince = time.Time{}
		if b.slow {
			b.slow = false
			return b.active, true
		}
		return b.active, false
	}
	if b.quietSince.IsZero() {
		b.quietSince = now
	}
	if !b.slow && now.Sub(b.quietSince) >= b.after {
		b.slow = true
		return b.idle, true
	}
	if b.slow {
		return b.idle, false
	}
	return b.active, false
}
