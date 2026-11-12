//go:build windows

package art

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCountryCode(t *testing.T) {
	for in, want := range map[string]string{
		"JP": "JP", "jp": "JP", " gb ": "GB", "ja-JP": "JP", "en_GB": "GB", "zh-Hans-CN": "CN",
		"": "", "001": "", "USA": "", "1a": "", "-": "",
	} {
		if got := CountryCode(in); got != want {
			t.Errorf("CountryCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetCountryFallsBackToUS(t *testing.T) {
	r := New(t.TempDir(), 1<<20)
	if r.Country() != "US" {
		t.Fatalf("default %q", r.Country())
	}
	r.SetCountry("ja-JP")
	if r.Country() != "JP" {
		t.Fatalf("got %q", r.Country())
	}
	r.SetCountry("nonsense")
	if r.Country() != "US" {
		t.Fatalf("unknown region must fall back, got %q", r.Country())
	}
}

const itunesHit = `{"results":[{"artworkUrl100":"https://fixture.invalid/a/100x100bb.jpg","trackViewUrl":"https://fixture.invalid/song"}]}`

func TestITunesSendsCountryAndCacheKeyIncludesIt(t *testing.T) {
	var countries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		countries = append(countries, req.URL.Query().Get("country"))
		w.Write([]byte(itunesHit))
	}))
	defer srv.Close()
	r := New(t.TempDir(), 1<<20)
	r.itunesURL = srv.URL
	tr := Track{Source: "spotify", Title: "Song", Artist: "Artist"}
	r.SetCountry("JP")
	r.Resolve(tr, "cdn")
	r.Resolve(tr, "cdn") // cached
	r.SetCountry("GB")
	r.Resolve(tr, "cdn") // a different storefront is a different entry
	if strings.Join(countries, ",") != "JP,GB" {
		t.Fatalf("requests by country: %v", countries)
	}
	if keyOf("itunes", "JP", "k") == keyOf("itunes", "GB", "k") {
		t.Fatal("cache key ignores country")
	}
}

func deezerFixture(t *testing.T, body string, status int, hits *atomic.Int32) *Resolver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		if q := req.URL.Query().Get("q"); !strings.Contains(q, `artist:"`) || !strings.Contains(q, `track:"`) {
			t.Errorf("query %q", q)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	r := New(t.TempDir(), 1<<20)
	r.deezerURL = srv.URL
	return r
}

func TestDeezerFallbackGoodMatchAndCaching(t *testing.T) {
	var hits atomic.Int32
	r := deezerFixture(t, `{"data":[
	 {"title":"Other Song","artist":{"name":"Artist"},"album":{"cover_xl":"https://cdn.invalid/wrong.jpg"}},
	 {"title":"Song (Remastered 2011)","artist":{"name":"The Artist"},"album":{"cover_xl":"","cover_big":"https://cdn.invalid/big.jpg"}}]}`, 200, &hits)
	l := r.deezerArt(Track{Title: "Song", Artist: "Artist"})
	if l.ImageURL != "https://cdn.invalid/big.jpg" || l.Via != "deezer" || l.ListenURL != "" {
		t.Fatalf("lookup %+v", l)
	}
	r.deezerArt(Track{Title: "Song", Artist: "Artist"})
	if hits.Load() != 1 {
		t.Fatalf("cache missed: %d requests", hits.Load())
	}
}

func TestDeezerPrefersXL(t *testing.T) {
	var hits atomic.Int32
	r := deezerFixture(t, `{"data":[{"title":"Song","artist":{"name":"Artist"},"album":{"cover_xl":"https://cdn.invalid/xl.jpg","cover_big":"https://cdn.invalid/big.jpg"}}]}`, 200, &hits)
	if l := r.deezerArt(Track{Title: "Song", Artist: "Artist"}); l.ImageURL != "https://cdn.invalid/xl.jpg" {
		t.Fatalf("%+v", l)
	}
}

func TestDeezerRejectsWeakMatches(t *testing.T) {
	for name, body := range map[string]string{
		"title is longer":  `{"data":[{"title":"Song Part 2","artist":{"name":"Artist"},"album":{"cover_xl":"https://cdn.invalid/x.jpg"}}]}`,
		"wrong artist":     `{"data":[{"title":"Song","artist":{"name":"Someone Else"},"album":{"cover_xl":"https://cdn.invalid/x.jpg"}}]}`,
		"no results":       `{"data":[]}`,
		"insecure cover":   `{"data":[{"title":"Song","artist":{"name":"Artist"},"album":{"cover_xl":"http://cdn.invalid/x.jpg"}}]}`,
		"not json":         `<html>`,
		"tiny artist name": `{"data":[{"title":"Song","artist":{"name":"A"},"album":{"cover_xl":"https://cdn.invalid/x.jpg"}}]}`,
	} {
		var hits atomic.Int32
		r := deezerFixture(t, body, 200, &hits)
		if l := r.deezerArt(Track{Title: "Song", Artist: "Artist"}); l.ImageURL != "" {
			t.Errorf("%s accepted: %+v", name, l)
		}
	}
	var hits atomic.Int32
	r := deezerFixture(t, `{"data":[{"title":"Song","artist":{"name":"Artist"},"album":{"cover_xl":"https://cdn.invalid/x.jpg"}}]}`, 200, &hits)
	if l := r.deezerArt(Track{Title: "Song"}); l.ImageURL != "" || hits.Load() != 0 {
		t.Errorf("a title with no artist must not be searched: %+v hits=%d", l, hits.Load())
	}
}

func TestDeezerNetworkAndStatusErrors(t *testing.T) {
	var hits atomic.Int32
	r := deezerFixture(t, `{}`, 500, &hits)
	if l := r.deezerArt(Track{Title: "Song", Artist: "Artist"}); l.ImageURL != "" {
		t.Fatalf("%+v", l)
	}
	r.deezerURL = "http://127.0.0.1:1" // refused
	if l := r.deezerArt(Track{Title: "Song", Artist: "Artist"}); l.ImageURL != "" {
		t.Fatalf("%+v", l)
	}
	// A failure is not remembered as a miss, so the next track retries.
	r.mu.Lock()
	n := len(r.itu)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("errors were cached: %d", n)
	}
}

func TestResolveFallsBackToDeezerOnlyAfterITunesMiss(t *testing.T) {
	var deezerHits atomic.Int32
	dz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		deezerHits.Add(1)
		w.Write([]byte(`{"data":[{"title":"Song","artist":{"name":"Artist"},"album":{"cover_xl":"https://cdn.invalid/xl.jpg"}}]}`))
	}))
	defer dz.Close()
	itunesBody := `{"results":[]}`
	it := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.Write([]byte(itunesBody)) }))
	defer it.Close()
	r := New(t.TempDir(), 1<<20)
	r.itunesURL, r.deezerURL = it.URL, dz.URL
	tr := Track{Source: "spotify", Title: "Song", Artist: "Artist"}
	if l := r.Resolve(tr, "cdn"); l.Via != "deezer" || l.ImageURL == "" {
		t.Fatalf("miss should reach deezer: %+v", l)
	}
	itunesBody = itunesHit
	tr.Title = "Song B"
	before := deezerHits.Load()
	if l := r.Resolve(tr, "cdn"); l.Via != "itunes" || deezerHits.Load() != before {
		t.Fatalf("iTunes hit must not call deezer: %+v", l)
	}
}

func TestBrowserAndYouTubeMusicNeverReachDeezer(t *testing.T) {
	for _, src := range []string{"browser", "youtubemusic"} {
		r := New(t.TempDir(), 1<<20)
		r.httpClient.Transport = fixtureTransport(func(*http.Request) (*http.Response, error) { t.Error("catalog lookup"); return nil, nil })
		r.Resolve(Track{Source: src, Title: "Song", Artist: "Artist"}, "cdn")
	}
}
