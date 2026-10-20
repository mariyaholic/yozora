//go:build windows

package browserwin

import (
	"net/url"
	"regexp"
	"strings"
)

var watchID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ParseYouTubeWatchID returns only a validated active-watch video ID.
func ParseYouTubeWatchID(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Path != "/watch" {
		return "", false
	}
	switch strings.ToLower(u.Host) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
	default:
		return "", false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", false
	}
	id := ""
	for _, v := range q["v"] {
		if !watchID.MatchString(v) || (id != "" && id != v) {
			return "", false
		}
		id = v
	}
	return id, id != ""
}
