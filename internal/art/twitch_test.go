//go:build windows

package art

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTwitchThumbnail(t *testing.T) {
	for _, tc := range []struct {
		login string
		live  bool
		calls int
		via   string
	}{
		{"streamer", true, 1, "twitch"},
		{"streamer", false, 1, "symbol"},
		{"Streamer", true, 0, "symbol"},
		{"../x", true, 0, "symbol"},
	} {
		calls := 0
		u, via := TwitchThumbnail(tc.login, func(u string) bool {
			calls++
			if u != "https://static-cdn.jtvnw.net/previews-ttv/live_user_"+tc.login+"-640x360.jpg" {
				t.Fatal(u)
			}
			return tc.live
		})
		if calls != tc.calls || via != tc.via || (via == "symbol" && u != TwitchSymbolURL) {
			t.Errorf("%s: %s %s calls=%d", tc.login, u, via, calls)
		}
	}
}

// An offline channel's preview redirects to a placeholder image, which must not count as live.
func TestTwitchThumbnailRejectsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "offline") {
			http.Redirect(w, r, "/placeholder.jpg", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	r := New(t.TempDir(), 1<<20)
	r.httpClient = srv.Client()
	r.httpClient.Transport = rewrite{srv.URL, r.httpClient.Transport}
	if u, via := r.TwitchThumbnail("onlinechan"); via != "twitch" || !strings.Contains(u, "live_user_onlinechan") {
		t.Fatalf("live: %s %s", u, via)
	}
	if u, via := r.TwitchThumbnail("offlinechan"); via != "symbol" || u != TwitchSymbolURL {
		t.Fatalf("offline: %s %s", u, via)
	}
}

type rewrite struct {
	base string
	next http.RoundTripper
}

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	target, _ := http.NewRequest(req.Method, rw.base+req.URL.Path, nil)
	out.URL = target.URL
	out.Host = target.URL.Host
	return rw.next.RoundTrip(out)
}
