//go:build windows

package spotifyapi

import (
	"testing"
	"testing/synctest"
	"time"
	"uika-resonance/internal/player"
)

func TestRunLoopReplacesStaleTrackAndClearsOnNoContent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Provider{PollEvery: time.Second, Out: make(chan player.Track, 1)}
		stop := make(chan struct{})
		done := make(chan struct{})
		count := 0
		go func() {
			defer close(done)
			p.runLoop(stop, func() (*player.Track, error) {
				count++
				if count == 1 {
					return &player.Track{Title: "old"}, nil
				}
				if count == 2 {
					return &player.Track{Title: "new"}, nil
				}
				return nil, nil
			})
		}()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if got := <-p.Out; got.Title != "new" {
			t.Fatalf("latest queued track = %q", got.Title)
		}
		p.Out <- player.Track{Title: "stale"}
		time.Sleep(time.Second)
		synctest.Wait()
		if got := <-p.Out; got.Source != "spotifyapi" || got.Title != "" {
			t.Fatalf("no-content did not clear: %+v", got)
		}
		p.Out <- player.Track{Title: "unread"}
		time.Sleep(time.Second)
		synctest.Wait()
		close(stop)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("poller blocked on a full queue")
		}
	})
}

func TestRunLoopDiscardsPollCompletingAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Provider{PollEvery: time.Second, Out: make(chan player.Track, 1)}
		stop := make(chan struct{})
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			p.runLoop(stop, func() (*player.Track, error) {
				close(entered)
				<-release
				return &player.Track{Title: "late"}, nil
			})
		}()
		<-entered
		close(stop)
		close(release)
		<-done
		select {
		case got := <-p.Out:
			t.Fatalf("published after stop: %+v", got)
		default:
		}
	})
}
