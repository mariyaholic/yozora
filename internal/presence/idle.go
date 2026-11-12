//go:build windows

package presence

import "time"

// Engine tick rates. The engine steps every tickActive while anything could be
// shown; after idleAfter with no media session and nothing published it steps
// every tickIdle. New SMTC or Spotify input wakes it at once, so the slower tick
// never delays a new track: that is bounded by the SMTC watcher's own idle poll.
const (
	tickActive = 250 * time.Millisecond
	tickIdle   = 2 * time.Second
	idleAfter  = time.Minute
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
