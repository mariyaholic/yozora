//go:build windows

package presence

import (
	"net/url"
	"strings"

	"uika-resonance/internal/player"
)

func serviceName(t *player.Track) string {
	switch t.Source {
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

// discordAssetURL keeps only artwork Discord's image proxy can actually
// fetch. Loopback art-server URLs render in the local dashboard but not in
// the server-side activity, so they are omitted instead of sent broken.
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

// SMTC supplies text, not a canonical track URL. A service-specific search is
// an honest fallback; never send Spotify listeners to an iTunes lookup result.
func musicURL(t *player.Track, resolved string, spotifySearch bool) string {
	query := strings.TrimSpace(t.Title + " " + t.Artist)
	switch t.Source {
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
