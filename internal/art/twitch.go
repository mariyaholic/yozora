//go:build windows

package art

import (
	"context"
	"net/http"
	"regexp"
	"time"
)

var twitchLogin = regexp.MustCompile(`^[a-z0-9_]{3,25}$`)

// TwitchPreviewURL is the live preview Twitch's CDN serves for a channel login.
func TwitchPreviewURL(login string) string {
	return "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + login + "-640x360.jpg"
}

// TwitchThumbnail probes only the preview URL built from a validated login. An offline
// channel's preview redirects to a placeholder, so live reports whether the CDN served it directly.
func TwitchThumbnail(login string, live func(string) bool) (url, via string) {
	if twitchLogin.MatchString(login) {
		if u := TwitchPreviewURL(login); live(u) {
			return u, "twitch"
		}
	}
	return TwitchSymbolURL, "symbol"
}

func (r *Resolver) TwitchThumbnail(login string) (string, string) {
	return TwitchThumbnail(login, func(u string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if err != nil {
			return false
		}
		client := *r.httpClient
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}
