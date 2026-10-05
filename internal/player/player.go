//go:build windows

// Package player defines the unified now-playing model and source
// arbitration across SMTC sessions and the optional Spotify Web API.
package player

import (
	"strings"
	"time"
)

// Track is one now-playing item from any source.
type Track struct {
	Source      string // applemusic|spotify|spotifyapi|browser|generic
	AppID       string
	Player      string // friendly player label
	Title       string
	Artist      string
	Album       string
	TrackNumber int32
	Playing     bool
	PositionSec float64
	DurationSec float64
	LastUpdated time.Time

	ArtURL    string // direct CDN URL when the source provides one
	ListenURL string // button target when known

	Thumb func() ([]byte, error) // one-shot artwork reader (SMTC), may be nil
}

// ElapsedSec estimates the playback position right now.
func (t Track) ElapsedSec(now time.Time) float64 {
	if t.Playing && !t.LastUpdated.IsZero() {
		d := now.Sub(t.LastUpdated).Seconds()
		if d < 0 {
			d = 0
		}
		return t.PositionSec + d
	}
	return t.PositionSec
}

// Identity identifies the track for change detection.
func (t Track) Identity() string {
	return strings.Join([]string{t.Source, t.Title, t.Artist, t.Album}, "\x00")
}

// IdentityPlaying includes the play/pause state.
func (t Track) IdentityPlaying() string {
	s := "p"
	if t.Playing {
		s = "y"
	}
	return t.Identity() + "\x00" + s
}

// IsBlocked applies canonical-source equality and custom AppID substring filters.
func IsBlocked(t Track, blocked []string) bool {
	app := strings.ToLower(t.AppID)
	source := strings.ToLower(strings.TrimSpace(t.Source))
	for _, b := range blocked {
		filter := strings.ToLower(strings.TrimSpace(b))
		if filter != "" && (filter == source || strings.Contains(app, filter)) {
			return true
		}
	}
	return false
}

// Arbitrate picks the session to show: playing sessions first, then paused,
// by configured source priority; blocked sources are dropped.
func Arbitrate(tracks []Track, order []string, blocked []string) *Track {
	prio := func(src string) int {
		for i, s := range order {
			if s == src {
				return i
			}
		}
		return len(order) + 1
	}
	best := -1
	bestKey := [2]int{}
	for i := range tracks {
		if IsBlocked(tracks[i], blocked) {
			continue
		}
		if strings.TrimSpace(tracks[i].Title) == "" {
			continue
		}
		playing := 0
		if tracks[i].Playing {
			playing = 1
		}
		key := [2]int{playing, -prio(tracks[i].Source)}
		if best < 0 || key[0] > bestKey[0] || (key[0] == bestKey[0] && key[1] > bestKey[1]) {
			best = i
			bestKey = key
		}
	}
	if best < 0 {
		return nil
	}
	return &tracks[best]
}
