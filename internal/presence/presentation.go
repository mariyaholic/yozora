//go:build windows

package presence

import (
	"net/url"
	"regexp"
	"strings"

	"uika-resonance/internal/player"
)

func serviceName(t *player.Track) string {
	if t.Service == "youtube" {
		return "YouTube"
	}
	switch t.Source {
	case "youtubemusic":
		return "YouTube Music"
	case "spotify", "spotifyapi":
		return "Spotify"
	case "applemusic":
		return "Apple Music"
	default:
		if name := strings.TrimSpace(t.Player); name != "" {
			return name
		}
		return "Media Player"
	}
}

func validWebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func platformURL(raw, host string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), host) && u.User == nil
}

func discordAssetURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return ""
	}
	return raw
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func musicURL(t *player.Track, resolved string, spotifySearch bool) string {
	if t.Service == "youtube" {
		if videoIDPattern.MatchString(t.VideoID) {
			return "https://www.youtube.com/watch?v=" + t.VideoID
		}
		return ""
	}
	query := strings.TrimSpace(t.Title + " " + t.Artist)
	switch t.Source {
	case "youtubemusic":
		return ""
	case "spotify", "spotifyapi":
		if platformURL(t.ListenURL, "open.spotify.com") {
			return t.ListenURL
		}
		if spotifySearch && query != "" {
			return "https://open.spotify.com/search/" + url.PathEscape(query)
		}
	case "applemusic":
		if platformURL(t.ListenURL, "music.apple.com") {
			return t.ListenURL
		}
		if platformURL(resolved, "music.apple.com") {
			return resolved
		}
		if query != "" {
			return "https://music.apple.com/us/search?term=" + url.QueryEscape(query)
		}
	default:
		if validWebURL(t.ListenURL) {
			return t.ListenURL
		}
	}
	return ""
}
