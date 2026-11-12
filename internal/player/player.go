//go:build windows

package player

import (
	"strings"
	"time"
)

type Track struct {
	Source      string
	Service     string
	VideoID     string
	Channel     string
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
		reserved := filter == "youtube" || filter == "youtubemusic" || filter == "youtube_tabs" || filter == "twitch"
		if filter != "" && (filter == source || filter == strings.ToLower(strings.TrimSpace(t.Service)) || (!reserved && strings.Contains(app, filter))) {
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

// HiddenRule keeps a track off Discord by what it is called. An empty field is not
// checked, so a rule can name an artist, a title or both. A field matches the whole
// value, ignoring case and surrounding space; "*" inside it stands for any run of
// characters. Wildcards are only that: the point is "Artist *" or "* (Live)", not a
// pattern language.
type HiddenRule struct {
	Artist string `toml:"artist" json:"artist"`
	Title  string `toml:"title" json:"title"`
}

// Usable reports whether the rule can match anything at all: it needs a field with
// something besides wildcards, so a stray "*" can never hide every track.
func (r HiddenRule) Usable() bool {
	return strings.Trim(strings.TrimSpace(r.Artist), "*\t ") != "" || strings.Trim(strings.TrimSpace(r.Title), "*\t ") != ""
}

// Same reports whether two rules are the same rule once case and space are ignored.
func (r HiddenRule) Same(o HiddenRule) bool {
	return fold(r.Artist) == fold(o.Artist) && fold(r.Title) == fold(o.Title)
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Matches reports whether the track is the one the rule names.
func (r HiddenRule) Matches(t Track) bool {
	if !r.Usable() {
		return false
	}
	return (fold(r.Artist) == "" || globMatch(fold(r.Artist), fold(t.Artist))) &&
		(fold(r.Title) == "" || globMatch(fold(r.Title), fold(t.Title)))
}

// IsHidden reports whether any rule names the track. Like a blocked source, a hidden
// track is never published; unlike one, it stays visible in the panel.
func IsHidden(t Track, rules []HiddenRule) bool {
	for _, r := range rules {
		if r.Matches(t) {
			return true
		}
	}
	return false
}

// globMatch matches s against a pattern whose only special character is "*".
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, last)
}
