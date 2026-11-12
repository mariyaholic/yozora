//go:build windows

package smtc

import (
	"testing"
	"time"
)

func TestWatcherBacksOffWithoutSessions(t *testing.T) {
	w := NewWatcher(time.Second)
	b := idleBackoff{active: w.Interval, idle: w.IdleInterval, after: w.IdleAfter}
	t0 := time.Unix(1000, 0)
	for s := 0; s < 60; s++ {
		if d, changed := b.observe(t0.Add(time.Duration(s)*time.Second), false); d != time.Second || changed {
			t.Fatalf("slowed after %ds: %s", s, d)
		}
	}
	if d, changed := b.observe(t0.Add(60*time.Second), false); d != DefaultIdleInterval || !changed {
		t.Fatalf("no backoff after a minute without sessions: %s", d)
	}
	if d, changed := b.observe(t0.Add(65*time.Second), true); d != time.Second || !changed {
		t.Fatalf("a session did not restore the configured interval: %s", d)
	}

	// A configured poll interval already slower than the idle one is left alone.
	slow := idleBackoff{active: 10 * time.Second, idle: DefaultIdleInterval, after: time.Minute}
	slow.observe(t0, false)
	if d, changed := slow.observe(t0.Add(time.Hour), false); d != 10*time.Second || changed {
		t.Fatalf("idle interval overrode a slower configured one: %s", d)
	}
}
