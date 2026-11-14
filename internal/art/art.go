//go:build windows

package art

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Lookup struct {
	DiscordURL string
	ImageURL   string
	ListenURL  string
	Via        string
}

type Resolver struct {
	httpClient *http.Client
	cacheDir   string
	cacheMax   int64
	baseURL    string
	// Catalog endpoints; tests point them at fakes.
	itunesURL string
	deezerURL string

	mu          sync.Mutex
	country     string
	itu         map[string]ituEntry
	files       map[string]int64
	cacheWarned bool
	insrt       sync.Once
	defOnce     sync.Once
	defURL      string
}

type ituEntry struct {
	l   Lookup
	exp time.Time
}

func New(cacheDir string, cacheMaxBytes int64) *Resolver {
	return &Resolver{
		httpClient: &http.Client{Timeout: 8 * time.Second},
		itunesURL:  "https://itunes.apple.com/search",
		deezerURL:  "https://api.deezer.com/search",
		country:    DefaultCountry,
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

// Cache bookkeeping is bounded even when the directory contains many tiny files.
const maxCachedFiles = 4096

// initCache accounts for thumbnails left by an earlier run. Read in batches and
// trim as we go, so even a large pre-existing directory does not become a
// permanent in-memory index. The generated default image is not a thumbnail.
func (r *Resolver) initCache() {
	r.insrt.Do(func() {
		dir, err := os.Open(r.cacheDir)
		if err != nil {
			log.Printf("art: cannot inspect cache; disk budget may be exceeded: %v", err)
			return
		}
		defer dir.Close()
		r.mu.Lock()
		defer r.mu.Unlock()
		for {
			entries, err := dir.ReadDir(64)
			for _, entry := range entries {
				name := entry.Name()
				ext := filepath.Ext(name)
				stem := strings.TrimSuffix(name, ext)
				if entry.IsDir() || (ext != ".png" && ext != ".jpg") || len(stem) != 40 {
					continue
				}
				if _, err := hex.DecodeString(stem); err != nil {
					continue
				}
				info, infoErr := entry.Info()
				if infoErr != nil {
					if !os.IsNotExist(infoErr) {
						r.warnCacheLocked(infoErr)
					}
				} else if info.Mode().IsRegular() {
					r.files[name] = info.Size()
					r.trimLocked()
				}
			}
			if err != nil {
				if err != io.EOF {
					r.warnCacheLocked(err)
				}
				break
			}
		}
	})
}

func (r *Resolver) warnCacheLocked(err error) {
	if !r.cacheWarned {
		log.Printf("art: cannot enforce disk cache budget: %v", err)
		r.cacheWarned = true
	}
}

func (r *Resolver) trimLocked() { r.trimLockedLimit(maxCachedFiles, os.Remove) }

// Failed eviction cannot be counted as freed disk space, but retaining every
// failed victim would make the bookkeeping unbounded. Drop it from the index
// after warning; the directory may therefore exceed the configured budget.
func (r *Resolver) trimLockedLimit(limit int, remove func(string) error) {
	type ent struct {
		name string
		mod  time.Time
		sz   int64
	}
	var total int64
	for _, sz := range r.files {
		total += sz
	}
	if total <= r.cacheMax && len(r.files) <= limit {
		return
	}
	ents := make([]ent, 0, len(r.files))
	total = 0
	for name := range r.files {
		if st, err := os.Stat(filepath.Join(r.cacheDir, name)); err == nil {
			ents = append(ents, ent{name, st.ModTime(), st.Size()})
			total += st.Size()
		} else {
			if !os.IsNotExist(err) {
				r.warnCacheLocked(err)
			}
			delete(r.files, name)
		}
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].mod.Before(ents[j].mod) })
	for _, e := range ents {
		if total <= r.cacheMax && len(r.files) <= limit {
			break
		}
		if err := remove(filepath.Join(r.cacheDir, e.name)); err != nil && !os.IsNotExist(err) {
			r.warnCacheLocked(err)
		}
		// total now describes tracked files, not bytes successfully removed.
		total -= e.sz
		delete(r.files, e.name)
	}
}

type Track struct {
	Source string
	Title  string
	Artist string
	Album  string
	ArtURL string
	Thumb  func() ([]byte, error)
}

func (r *Resolver) Resolve(t Track, prefer string) Lookup {

	if t.Source == "youtubemusic" {
		l := r.localArt(t, keyOf(t.Source, t.Title, t.Artist, t.Album))
		l.DiscordURL = YouTubeMusicSymbolURL
		if l.ImageURL == "" {
			l.Via = "symbol"
		}
		return l
	}
	if t.ArtURL != "" {
		return Lookup{ImageURL: t.ArtURL, Via: "spotify"}
	}
	key := keyOf(t.Source, t.Title, t.Artist, t.Album)

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
		if l := r.catalogArt(t, key); l.ImageURL != "" {
			return l
		}
	} else {
		if l := r.catalogArt(t, key); l.ImageURL != "" {
			return l
		}
		if l := r.localArt(t, key); l.ImageURL != "" {
			return l
		}
	}
	return Lookup{Via: "default"}
}

// catalogArt asks iTunes first and Deezer only when iTunes has nothing.
func (r *Resolver) catalogArt(t Track, key string) Lookup {
	if l := r.itunesArt(t, key); l.ImageURL != "" {
		return l
	}
	return r.deezerArt(t)
}

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
	if int64(len(data)) > r.cacheMax {
		return Lookup{}
	}
	if err := os.MkdirAll(r.cacheDir, 0o755); err != nil {
		return Lookup{}
	}
	r.initCache()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.WriteFile(filepath.Join(r.cacheDir, name), data, 0o644); err == nil {
		r.files[name] = int64(len(data))
		r.trimLocked()
		if _, ok := r.files[name]; ok {
			return Lookup{ImageURL: r.baseURL + "/art/" + name, Via: "smtc"}
		}
	}
	return Lookup{}
}

func (r *Resolver) itunesArt(t Track, key string) Lookup {
	if strings.TrimSpace(t.Title) == "" {
		return Lookup{}
	}
	// The store is regional, so the same song can hit in one country and miss in another.
	country := r.Country()
	key = keyOf("itunes", country, key)
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
	q.Set("country", country)
	req, err := http.NewRequest(http.MethodGet, r.itunesURL+"?"+q.Encode(), nil)
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
	r.remember(key, l)
	return l
}

func (r *Resolver) remember(key string, l Lookup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.itu[key] = ituEntry{l, time.Now().Add(24 * time.Hour)}
	if len(r.itu) > 128 {
		for k := range r.itu {
			delete(r.itu, k)
			if len(r.itu) <= 96 {
				break
			}
		}
	}
}

func (r *Resolver) StartServer(port int) (string, error) {
	_ = os.MkdirAll(r.cacheDir, 0o755)
	r.initCache()
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

func (r *Resolver) DefaultURL() string {
	if r.baseURL == "" {
		return ""
	}
	r.defOnce.Do(func() {
		name := "default.png"
		p := filepath.Join(r.cacheDir, name)
		if _, err := os.Stat(p); err != nil {
			img := image.NewRGBA(image.Rect(0, 0, 512, 512))

			for y := 0; y < 512; y++ {
				for x := 0; x < 512; x++ {
					v := uint8(38 + (x+y)*90/1024)
					img.Set(x, y, color.RGBA{R: 212 - v/3, G: 168 - v/3, B: 67, A: 255})
				}
			}
			f, err := os.Create(p)
			if err == nil {
				_ = png.Encode(f, img)
				f.Close()
			}
		}
		r.defURL = r.baseURL + "/art/" + name
	})
	return r.defURL
}
