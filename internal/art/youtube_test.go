//go:build windows

package art

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestYouTubeThumbnail(t *testing.T) {
	for _, tc := range []struct {
		name, id       string
		success, calls int
		suffix, via    string
	}{
		{"mq", "dQw4w9WgXcQ", 1, 1, "/mqdefault.jpg", "youtube"},
		{"hq", "dQw4w9WgXcQ", 2, 2, "/hqdefault.jpg", "youtube"},
		{"symbol", "dQw4w9WgXcQ", 0, 2, "", "symbol"},
		{"invalid", "../invalid", 0, 0, "", "symbol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			u, via := YouTubeThumbnail(tc.id, func(u string) bool {
				calls++
				suffix := "/mqdefault.jpg"
				if calls == 2 {
					suffix = "/hqdefault.jpg"
				}
				if u != "https://i.ytimg.com/vi/"+tc.id+suffix {
					t.Fatal(u)
				}
				return calls == tc.success
			})
			if calls != tc.calls || via != tc.via || (via == "symbol" && u != YouTubeSymbolURL) || (via == "youtube" && !strings.HasSuffix(u, tc.suffix)) {
				t.Fatalf("%s %s, calls=%d", u, via, calls)
			}
		})
	}
}

type youtubeTransport func(*http.Request) (*http.Response, error)

func (f youtubeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type youtubeBody struct {
	io.Reader
	closed bool
}

func (b *youtubeBody) Close() error { b.closed = true; return nil }

func TestResolverYouTubeThumbnail(t *testing.T) {
	var bodies []*youtubeBody
	calls := 0
	r := &Resolver{httpClient: &http.Client{Transport: youtubeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodHead || req.URL.Host != "i.ytimg.com" {
			t.Fatal("unexpected request")
		}
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("missing short timeout")
		}
		b := &youtubeBody{Reader: strings.NewReader("")}
		bodies = append(bodies, b)
		status := 204
		if calls == 2 {
			status = 200
		}
		return &http.Response{StatusCode: status, Body: b, Header: make(http.Header)}, nil
	})}}
	u, via := r.YouTubeThumbnail("dQw4w9WgXcQ")
	if !strings.HasSuffix(u, "/hqdefault.jpg") || via != "youtube" || calls != 2 {
		t.Fatalf("%s %s %d", u, via, calls)
	}
	for _, b := range bodies {
		if !b.closed {
			t.Fatal("body not closed")
		}
	}
}
