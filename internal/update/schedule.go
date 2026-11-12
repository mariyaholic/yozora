package update

import "time"

const (
	// Interval is the most often Yozora asks GitHub.
	Interval = 24 * time.Hour
	// StartupDelay keeps the check out of the way of startup.
	StartupDelay = 2 * time.Minute
	// RetryAfterFailure is how soon a failed check (offline, rate limited) tries again.
	RetryAfterFailure = time.Hour
	// MaxJitter spreads the checks of many installs that started together.
	MaxJitter = 90 * time.Second
)

// NextDelay is how long to wait before the next check, given what is
// remembered. A check is due 24 h after the last successful one, or 1 h after
// a failed attempt if that comes sooner; with no history it is due at startup.
// It never waits less than StartupDelay or more than Interval (a clock set
// backwards must not postpone checks for years), plus jitter.
func NextDelay(st State, now time.Time, jitter time.Duration) time.Duration {
	due := now.Add(StartupDelay)
	if !st.CheckedAt.IsZero() {
		due = st.CheckedAt.Add(Interval)
	}
	if st.AttemptAt.After(st.CheckedAt) {
		if retry := st.AttemptAt.Add(RetryAfterFailure); st.CheckedAt.IsZero() || retry.Before(due) {
			due = retry
		}
	}
	d := due.Sub(now)
	if d < StartupDelay {
		d = StartupDelay
	}
	if d > Interval {
		d = Interval
	}
	return d + jitter
}
