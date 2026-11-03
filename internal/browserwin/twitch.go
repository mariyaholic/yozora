//go:build windows

package browserwin

import (
	"net/url"
	"regexp"
	"strings"
)

var twitchLogin = regexp.MustCompile(`^[A-Za-z0-9_]{3,25}$`)

// Edge may append a profile after the tab's Twitch suffix. This stays out of
// StripBrowserDecoration so a YouTube video titled "… - Twitch" keeps its YouTube suffix.
var twitchProfile = regexp.MustCompile(`( - Twitch(?: and \d+ more pages?)?) - [^-]+$`)

// Twitch serves these as top-level pages, so a path of exactly one of them is never a channel.
var twitchReserved = map[string]bool{
	"directory": true, "videos": true, "settings": true, "search": true, "downloads": true,
	"jobs": true, "p": true, "turbo": true, "subscriptions": true, "inventory": true,
	"wallet": true, "drops": true, "friends": true, "messages": true, "following": true,
	"prime": true, "store": true, "login": true, "signup": true, "moderator": true,
	"popout": true, "clips": true, "collections": true, "u": true, "team": true,
}

// MatchTwitchTitle counts windows whose stripped title is "<channel> - Twitch" for this track.
// Twitch's media session reports the stream title and the channel as artist; without one,
// Chromium falls back to the page title. index is valid only when count is one.
func MatchTwitchTitle(titles []string, smtcTitle, smtcArtist string) (index int, count int) {
	index = -1
	if smtcTitle == "" {
		return
	}
	for i, title := range titles {
		stripped := StripBrowserDecoration(title)
		if strings.HasSuffix(stripped, " - YouTube") {
			continue
		}
		if !strings.HasSuffix(stripped, " - Twitch") {
			stripped = morePages.ReplaceAllString(twitchProfile.ReplaceAllString(stripped, "${1}"), "")
		}
		page, ok := strings.CutSuffix(stripped, " - Twitch")
		if !ok || page == "" {
			continue
		}
		if page == smtcTitle || smtcTitle == page+" - Twitch" || (smtcArtist != "" && strings.EqualFold(page, smtcArtist)) {
			index = i
			count++
		}
	}
	if count != 1 {
		index = -1
	}
	return
}

// ParseTwitchChannel returns the lowercase login of a live channel page. VODs, clips and
// directory pages carry no live preview, so they are rejected.
func ParseTwitchChannel(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", false
	}
	switch strings.ToLower(u.Host) {
	case "twitch.tv", "www.twitch.tv", "m.twitch.tv":
	default:
		return "", false
	}
	login := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/")
	if !twitchLogin.MatchString(login) || twitchReserved[strings.ToLower(login)] {
		return "", false
	}
	return strings.ToLower(login), true
}
