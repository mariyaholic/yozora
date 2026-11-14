//go:build windows

package art

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserSuppliedURLBypassesReaders(t *testing.T) {
	r := New(t.TempDir(), 1<<20)
	r.httpClient.Transport = fixtureTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network lookup"); return nil, nil })
	const supplied = "https://fixture.invalid/supplied-thumbnail.jpg"
	got := r.Resolve(Track{Source: "browser", ArtURL: supplied, Thumb: func() ([]byte, error) { t.Fatal("unexpected thumbnail read"); return nil, nil }}, "cdn")
	if got.ImageURL != supplied || got.ListenURL != "" {
		t.Fatalf("supplied URL changed: %+v", got)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestBrowserNeverSearchesMusic(t *testing.T) {
	for _, prefer := range []string{"cdn", "smtc"} {
		for _, thumbnail := range []bool{false, true} {
			t.Run(prefer+"/"+map[bool]string{false: "missing", true: "supplied"}[thumbnail], func(t *testing.T) {
				r := New(t.TempDir(), 1<<20)
				r.baseURL = "http://127.0.0.1:43210"
				requests := 0
				r.httpClient.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"artworkUrl100":"https://fixture.invalid/unrelated/100x100bb.jpg","trackViewUrl":"https://fixture.invalid/song"}]}`))}, nil
				})
				tr := Track{Source: "browser", Title: "Synthetic video title"}
				var raw bytes.Buffer
				if thumbnail {
					if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
						t.Fatal(err)
					}
					tr.Thumb = func() ([]byte, error) { return raw.Bytes(), nil }
				}
				got := r.Resolve(tr, prefer)
				if requests != 0 {
					t.Errorf("browser triggered %d music search requests", requests)
				}
				if got.ListenURL != "" {
					t.Errorf("guessed music listen URL: %q", got.ListenURL)
				}
				if thumbnail {
					if got.Via != "smtc" {
						t.Fatalf("supplied thumbnail ignored: %+v", got)
					}
					stored, err := os.ReadFile(filepath.Join(r.cacheDir, keyOf(tr.Source, tr.Title, tr.Artist, tr.Album)+".png"))
					if err != nil || !bytes.Equal(stored, raw.Bytes()) {
						t.Fatalf("thumbnail bytes not preserved: %v", err)
					}
				} else if got.Via != "default" || got.ImageURL != "" {
					t.Errorf("missing thumbnail must be honest fallback: %+v", got)
				}
			})
		}
	}
}

func TestYouTubeMusicNeverSearchesCatalog(t *testing.T) {
	for _, prefer := range []string{"cdn", "smtc"} {
		for _, thumbnail := range []bool{false, true} {
			r := New(t.TempDir(), 1<<20)
			r.baseURL = "http://127.0.0.1:43210"
			r.httpClient.Transport = fixtureTransport(func(*http.Request) (*http.Response, error) { t.Error("unexpected catalog lookup"); return nil, nil })
			track := Track{Source: "youtubemusic", Title: "song", ArtURL: "https://ignored.test/art"}
			if thumbnail {
				track.Thumb = func() ([]byte, error) { return []byte("thumbnail"), nil }
			}
			got := r.Resolve(track, prefer)
			if got.DiscordURL != YouTubeMusicSymbolURL || got.ListenURL != "" {
				t.Fatalf("lookup: %+v", got)
			}
			if thumbnail {
				if got.Via != "smtc" || !strings.HasPrefix(got.ImageURL, r.baseURL) {
					t.Fatalf("local art missing: %+v", got)
				}
			} else if got.Via != "symbol" || got.ImageURL != "" {
				t.Fatalf("fallback: %+v", got)
			}
		}
	}
}

func TestCacheIncludesFilesFromEarlierRun(t *testing.T) {
	dir := t.TempDir()
	old := keyOf("old") + ".png"
	newer := keyOf("newer") + ".jpg"
	if err := os.WriteFile(filepath.Join(dir, old), []byte("123456"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, newer), []byte("123456"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, old), past, past); err != nil {
		t.Fatal(err)
	}
	// Unrelated files and the default placeholder are never thumbnail victims.
	for _, name := range []string{"default.png", "unrelated.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := New(dir, 6)
	r.initCache()
	if _, err := os.Stat(filepath.Join(dir, old)); !os.IsNotExist(err) {
		t.Fatalf("old thumbnail still exists: %v", err)
	}
	for _, name := range []string{newer, "default.png", "unrelated.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("lost %s: %v", name, err)
		}
	}
}

func TestOversizedThumbnailDoesNotReturnBrokenURL(t *testing.T) {
	r := New(t.TempDir(), 3)
	r.baseURL = "http://127.0.0.1:43210"
	got := r.localArt(Track{Thumb: func() ([]byte, error) { return []byte("1234"), nil }}, keyOf("oversized"))
	if got.ImageURL != "" {
		t.Fatalf("returned URL for oversized image: %+v", got)
	}
	entries, err := os.ReadDir(r.cacheDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cache entries = %v, %v", entries, err)
	}
}

func TestDefaultArtSurvivesThumbnailEviction(t *testing.T) {
	r := New(t.TempDir(), 5)
	r.baseURL = "http://127.0.0.1:43210"
	if r.DefaultURL() == "" {
		t.Fatal("missing default URL")
	}
	for _, title := range []string{"first", "second"} {
		key := keyOf(title)
		got := r.localArt(Track{Thumb: func() ([]byte, error) { return []byte("12345"), nil }}, key)
		if got.ImageURL == "" {
			t.Fatalf("missing local image for %s", title)
		}
		if title == "first" {
			past := time.Now().Add(-time.Hour)
			if err := os.Chtimes(filepath.Join(r.cacheDir, key+".png"), past, past); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(r.cacheDir, "default.png")); err != nil {
		t.Fatalf("default image evicted: %v", err)
	}
}

func TestCacheIndexStaysBoundedWhenEvictionFails(t *testing.T) {
	for _, budget := range []int64{2, 1000} {
		t.Run(fmt.Sprintf("budget-%d", budget), func(t *testing.T) {
			r := New(t.TempDir(), budget)
			const limit = 2
			for i := 0; i < 8; i++ {
				name := keyOf(fmt.Sprint(i)) + ".png"
				if err := os.WriteFile(filepath.Join(r.cacheDir, name), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				r.files[name] = 1
				r.trimLockedLimit(limit, func(string) error { return os.ErrPermission })
				if len(r.files) > limit {
					t.Fatalf("index grew to %d entries", len(r.files))
				}
			}
			if !r.cacheWarned {
				t.Fatal("failed evictions did not report an unenforceable disk budget")
			}
			entries, err := os.ReadDir(r.cacheDir)
			if err != nil || len(entries) != 8 {
				t.Fatalf("failed evictions unexpectedly deleted files: %d, %v", len(entries), err)
			}
		})
	}
}
