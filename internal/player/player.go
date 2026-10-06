//go:build windows

package player

import (
	"strings"
	"time"
)

type Track struct {
	Source      string
	AppID       string
	Player      string
	Title       string
	Artist      string
	Album       string
	TrackNumber int32
	Playing     bool
	PositionSec float64
	DurationSec float64
	LastUpdated time.Time

	ArtURL    string
	ListenURL string

	Thumb func() ([]byte, error)
}

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

func (t Track) Identity() string {
	return strings.Join([]string{t.Source, t.Title, t.Artist, t.Album}, "\x00")
}

func (t Track) IdentityPlaying() string {
	s := "p"
	if t.Playing {
		s = "y"
	}
	return t.Identity() + "\x00" + s
}

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
