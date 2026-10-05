//go:build windows

// Package art resolves album artwork for the presence card: iTunes Search
// CDN URLs (preferred — Discord fetches them directly), SMTC thumbnails
// served from a localhost cache, or a bundled default cover.
package art

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Lookup is the result of resolving art for a track.
type Lookup struct {
	ImageURL  string // https URL (CDN) or http://127.0.0.1 local URL
	ListenURL string // track page URL for the Listen button, may be empty
	Via       string // "itunes", "smtc", "spotify", "default"
}

// Resolver resolves artwork with caching.
type Resolver struct {
	httpClient *http.Client
	cacheDir   string
	cacheMax   int64
	baseURL    string // local art server base, set by StartServer

	mu      sync.Mutex
	itu     map[string]ituEntry
	files   map[string]int64 // hash -> size, for LRU trim
	insrt   sync.Once
	defOnce sync.Once
	defURL  string
}

type ituEntry struct {
	l   Lookup
	exp time.Time
}

func New(cacheDir string, cacheMaxBytes int64) *Resolver {
	return &Resolver{
		httpClient: &http.Client{Timeout: 8 * time.Second},
		cacheDir:   cacheDir,
		cacheMax:   cacheMaxBytes,
		itu:        map[string]ituEntry{},
		files:      map[string]int64{},
	}
}

func keyOf(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}
func (r *Resolver) trimLocked() {
	var total int64
	for _, sz := range r.files {
		total += sz
	}
	if total <= r.cacheMax {
		return
	}
	// Crude trim: delete oldest files by modtime until under budget.
	type ent struct {
		name string
		mod  time.Time
		sz   int64
	}
	ents := make([]ent, 0, len(r.files))
	for name := range r.files {
		p := filepath.Join(r.cacheDir, name)
		if st, err := os.Stat(p); err == nil {
			ents = append(ents, ent{name, st.ModTime(), st.Size()})
		} else {
			delete(r.files, name)
		}
	}
	// oldest first
	for i := 0; i < len(ents); i++ {
		for j := i + 1; j < len(ents); j++ {
			if ents[j].mod.Before(ents[i].mod) {
				ents[i], ents[j] = ents[j], ents[i]
			}
		}
	}
	for _, e := range ents {
		if total <= r.cacheMax {
			break
		}
		if err := os.Remove(filepath.Join(r.cacheDir, e.name)); err == nil {
			total -= e.sz
			delete(r.files, e.name)
		}
	}
}

// Track is the minimal input the resolver needs.
type Track struct {
	Source string
	Title  string
	Artist string
	Album  string
	ArtURL string                 // known-good CDN URL (e.g. Spotify Web API)
	Thumb  func() ([]byte, error) // SMTC thumbnail reader, may be nil
}

// Resolve returns artwork per the configured preference chain.
// prefer: "cdn" (iTunes first) or "smtc" (thumbnail first).
func (r *Resolver) Resolve(t Track, prefer string) Lookup {
	// 0. Known-good URL from the track itself (Spotify Web API).
	if t.ArtURL != "" {
		return Lookup{ImageURL: t.ArtURL, Via: "spotify"}
	}
	key := keyOf(t.Source, t.Title, t.Artist, t.Album)
	// Browser titles identify videos/pages, not songs. Only supplied artwork
	// is trustworthy; never infer music artwork or a listen URL from the title.
	if t.Source == "browser" {
		if l := r.localArt(t, key); l.ImageURL != "" {
			return l
		}
		return Lookup{Via: "default"}
	}
	if prefer == "smtc" {
		if l := r.localArt(t, key); l.ImageURL != "" {
			return l
		}
		if l := r.itunesArt(t, key); l.ImageURL != "" {
			return l
		}
	} else {
		if l := r.itunesArt(t, key); l.ImageURL != "" {
			return l
		}
		if l := r.localArt(t, key); l.ImageURL != "" {
			return l
		}
	}
	return Lookup{Via: "default"}
}

// localArt stores the SMTC thumbnail in the cache and returns its URL.
func (r *Resolver) localArt(t Track, key string) Lookup {
	if t.Thumb == nil || r.baseURL == "" {
		return Lookup{}
	}
	data, err := t.Thumb()
	if err != nil || len(data) == 0 {
		log.Printf("art: smtc thumbnail: %v", err)
		return Lookup{}
	}
	ext := ".png"
	if len(data) > 2 && data[0] == 0xFF && data[1] == 0xD8 {
		ext = ".jpg"
	}
	name := key + ext
	if err := os.MkdirAll(r.cacheDir, 0o755); err == nil {
		p := filepath.Join(r.cacheDir, name)
		if err := os.WriteFile(p, data, 0o644); err == nil {
			r.mu.Lock()
			r.files[name] = int64(len(data))
			r.trimLocked()
			r.mu.Unlock()
			return Lookup{ImageURL: r.baseURL + "/art/" + name, Via: "smtc"}
		}
	}
	return Lookup{}
}

// itunesArt queries the public iTunes Search API for artwork + track URL.
func (r *Resolver) itunesArt(t Track, key string) Lookup {
	if strings.TrimSpace(t.Title) == "" {
		return Lookup{}
	}
	r.mu.Lock()
	if e, ok := r.itu[key]; ok && time.Now().Before(e.exp) {
		r.mu.Unlock()
		return e.l
	}
	r.mu.Unlock()

	q := url.Values{}
	q.Set("term", strings.TrimSpace(t.Title+" "+t.Artist))
	q.Set("media", "music")
	q.Set("limit", "1")
	req, err := http.NewRequest(http.MethodGet, "https://itunes.apple.com/search?"+q.Encode(), nil)
	if err != nil {
		return Lookup{}
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		log.Printf("art: itunes: %v", err)
		return Lookup{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("art: itunes: status %d", resp.StatusCode)
		return Lookup{}
	}
	var out struct {
		Results []struct {
			ArtworkURL100 string `json:"artworkUrl100"`
			TrackViewURL  string `json:"trackViewUrl"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Lookup{}
	}
	l := Lookup{}
	if len(out.Results) > 0 {
		big := strings.Replace(out.Results[0].ArtworkURL100, "100x100bb", "600x600bb", 1)
		l = Lookup{ImageURL: big, ListenURL: out.Results[0].TrackViewURL, Via: "itunes"}
	}
	r.mu.Lock()
	r.itu[key] = ituEntry{l, time.Now().Add(24 * time.Hour)}
	if len(r.itu) > 512 {
		for k := range r.itu {
			delete(r.itu, k)
			if len(r.itu) <= 384 {
				break
			}
		}
	}
	r.mu.Unlock()
	return l
}

// StartServer serves the art cache on 127.0.0.1. Returns the base URL.
func (r *Resolver) StartServer(port int) (string, error) {
	_ = os.MkdirAll(r.cacheDir, 0o755)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/art/", func(w http.ResponseWriter, req *http.Request) {
		name := filepath.Base(req.URL.Path)
		if !strings.HasSuffix(name, ".png") && !strings.HasSuffix(name, ".jpg") {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, req, filepath.Join(r.cacheDir, name))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	r.baseURL = "http://127.0.0.1:" + itoa(ln.Addr().(*net.TCPAddr).Port)
	return r.baseURL, nil
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// DefaultURL generates (once) a bundled fallback cover in the cache dir and
// returns its URL. Returns "" when the local server is not running.
func (r *Resolver) DefaultURL() string {
	if r.baseURL == "" {
		return ""
	}
	r.defOnce.Do(func() {
		name := "default.png"
		p := filepath.Join(r.cacheDir, name)
		if _, err := os.Stat(p); err != nil {
			img := image.NewRGBA(image.Rect(0, 0, 512, 512))
			// Warm gold gradient — the estate's lamp accent.
			for y := 0; y < 512; y++ {
				for x := 0; x < 512; x++ {
					v := uint8(38 + (x+y)*90/1024)
					img.Set(x, y, color.RGBA{R: 212 - v/3, G: 168 - v/3, B: 67, A: 255})
				}
			}
			f, err := os.Create(p)
			if err == nil {
				if err := png.Encode(f, img); err == nil {
					r.mu.Lock()
					r.files[name] = 0
					r.mu.Unlock()
				}
				f.Close()
			}
		}
		r.defURL = r.baseURL + "/art/" + name
	})
	return r.defURL
}

var _ = log.Print
