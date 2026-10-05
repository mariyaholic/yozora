//go:build windows

package art

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
