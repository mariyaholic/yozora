//go:build windows

package art

import (
	"context"
	"net/http"
	"regexp"
	"time"
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// YouTubeThumbnail probes only URLs constructed from a validated video ID.
func YouTubeThumbnail(id string, exists func(string) bool) (url, via string) {
	if youtubeID.MatchString(id) {
		for _, name := range []string{"mqdefault.jpg", "hqdefault.jpg"} {
			u := "https://i.ytimg.com/vi/" + id + "/" + name
			if exists(u) {
				return u, "youtube"
			}
		}
	}
	return YouTubeSymbolURL, "symbol"
}

func (r *Resolver) YouTubeThumbnail(id string) (string, string) {
	return YouTubeThumbnail(id, func(u string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if err != nil {
			return false
		}
		resp, err := r.httpClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}
