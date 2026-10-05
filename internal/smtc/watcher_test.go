//go:build windows

package smtc

import "testing"

func TestPublishLatestReplacesPendingSnapshot(t *testing.T) {
	out := make(chan []Track, 1)
	out <- []Track{{Title: "stale track"}}
	publishLatest(out, []Track{{Title: "current track"}})
	select {
	case got := <-out:
		if len(got) != 1 || got[0].Title != "current track" {
			t.Fatalf("snapshot = %+v, want newest track", got)
		}
	default:
		t.Fatal("no snapshot published")
	}
}
