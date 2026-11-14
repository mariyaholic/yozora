//go:build windows

package config

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestConcurrentReloadDoesNotPublishOlderConfigLast(t *testing.T) {
	h := &Holder{}
	h.val.Store(Defaults())
	older, newer := Defaults(), Defaults()
	older.Log.Level, newer.Log.Level = "older", "newer"
	readOlder, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 2)
	go func() {
		_, err := h.reload(func(string) (*Config, []byte, error) {
			close(readOlder)
			<-release
			return older, nil, nil
		})
		finished <- err
	}()
	<-readOlder
	go func() {
		_, err := h.reload(func(string) (*Config, []byte, error) { return newer, nil, nil })
		finished <- err
	}()
	// The old implementation completes the newer reload here; serialization
	// blocks it until the first is released. Do not wait indefinitely.
	completed := 0
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
		completed++
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for ; completed < 2; completed++ {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
	if got := h.Get().Log.Level; got != "newer" {
		t.Fatalf("active config = %q; newer reload was overwritten", got)
	}
}

func TestChangeHookCanReload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &Holder{}
		h.val.Store(Defaults())
		next := Defaults()
		next.Log.Level = "debug"
		final := Defaults()
		final.Log.Level = "warn"
		calls := 0
		h.OnChange(func(c *Config) {
			calls++
			if c == next {
				changed, err := h.reload(func(string) (*Config, []byte, error) { return final, nil, nil })
				if err != nil || !changed {
					t.Errorf("nested reload: changed=%v, error=%v", changed, err)
				}
			}
		})
		if changed, err := h.reload(func(string) (*Config, []byte, error) { return next, nil, nil }); err != nil || !changed {
			t.Fatalf("reload: changed=%v, error=%v", changed, err)
		}
		if calls != 2 || h.Get() != final {
			t.Fatalf("hook called %d times", calls)
		}
	})
}

func TestConcurrentReloadAppliesLatestAfterBlockedHook(t *testing.T) {
	h := &Holder{}
	h.val.Store(Defaults())
	older, intermediate, newer := Defaults(), Defaults(), Defaults()
	older.Log.Level, intermediate.Log.Level, newer.Log.Level = "older", "intermediate", "newer"
	entered, release := make(chan struct{}), make(chan struct{})
	seen := make(chan string, 3)
	h.OnChange(func(c *Config) {
		if c == older {
			close(entered)
			<-release
		}
		seen <- c.Log.Level
	})
	firstDone := make(chan error, 1)
	go func() {
		_, err := h.reload(func(string) (*Config, []byte, error) { return older, nil, nil })
		firstDone <- err
	}()
	<-entered
	secondDone := make(chan error, 1)
	go func() {
		_, err := h.reload(func(string) (*Config, []byte, error) { return intermediate, nil, nil })
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("newer reload blocked behind a callback")
	}
	if _, err := h.reload(func(string) (*Config, []byte, error) { return newer, nil, nil }); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if first, second := <-seen, <-seen; first != "older" || second != "newer" {
		t.Fatalf("callback order = %q, %q; want older, newer", first, second)
	}
	if len(seen) != 0 {
		t.Fatal("an intermediate config was not coalesced")
	}
	if got := h.Get().Log.Level; got != "newer" {
		t.Fatalf("active config = %q, want newer", got)
	}
}
