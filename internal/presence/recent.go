//go:build windows

package presence

import (
	"sync"
	"time"
)

// recentMax is how many distinct tracks the panel's Recent list keeps.
const recentMax = 20

// RecentTrack is one track that was published to Discord. It lives in memory only and is
// gone when Yozora quits.
type RecentTrack struct {
	Title   string    `json:"title"`
	Artist  string    `json:"artist"`
	Service string    `json:"service"`
	ArtURL  string    `json:"art_url,omitempty"`
	At      time.Time `json:"at"`
}

// recentRing holds the newest tracks first. The zero value is ready to use.
type recentRing struct {
	mu    sync.Mutex
	items []RecentTrack
}

func sameRecent(a, b RecentTrack) bool {
	return a.Title == b.Title && a.Artist == b.Artist && a.Service == b.Service
}

// add records a publish. A track already at the front only has its artwork refreshed, since
// artwork often arrives after the first send and a pause or resume is not a new play. A track
// heard earlier moves to the front with the new time instead of appearing twice.
func (r *recentRing) add(t RecentTrack) {
	if t.Title == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) > 0 && sameRecent(r.items[0], t) {
		if t.ArtURL != "" {
			r.items[0].ArtURL = t.ArtURL
		}
		return
	}
	for i, old := range r.items {
		if sameRecent(old, t) {
			if t.ArtURL == "" {
				t.ArtURL = old.ArtURL
			}
			r.items = append(r.items[:i], r.items[i+1:]...)
			break
		}
	}
	r.items = append([]RecentTrack{t}, r.items...)
	if len(r.items) > recentMax {
		r.items = r.items[:recentMax]
	}
}

// list returns a copy, newest first.
func (r *recentRing) list() []RecentTrack {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecentTrack(nil), r.items...)
}

// Recent lists the tracks published to Discord this run, newest first.
func (e *Engine) Recent() []RecentTrack { return e.recent.list() }
