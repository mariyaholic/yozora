//go:build windows

package server

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	"uika-resonance/internal/presence"
	"uika-resonance/internal/spotifyapi"
	"uika-resonance/internal/sysutil"
	"uika-resonance/internal/version"
)

//go:embed ui.html
var uiHTML string

// Theme faces ship inside the binary; the panel never fetches fonts from the network.
//
//go:embed fonts/*.woff2
var fontFiles embed.FS

type Deps struct {
	Engine     *presence.Engine
	Cfg        *config.Holder
	CfgPath    string
	Token      string
	DiscordApp string

	// Spotify connect from the panel. Without them the panel hides the connect controls.
	Spotify           *spotifyapi.Manager
	SpotifyLink       *spotifyapi.Link
	ClearSpotifyToken func() error
	OpenURL           func(string) error
	// RedirectBase is the panel's own loopback origin; Start fills it in.
	RedirectBase string
}

func validateSourceOrder(order, canonical []string) error {
	if len(order) != len(canonical) {
		return errors.New("order must contain every source exactly once")
	}
	seen := make(map[string]bool, len(order))
	for _, source := range order {
		known := false
		for _, c := range canonical {
			if source == c {
				known = true
				break
			}
		}
		if !known || seen[source] {
			return errors.New("order must contain every canonical source exactly once")
		}
		seen[source] = true
	}
	return nil
}

func constantTimeEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()
	var configMu sync.Mutex
	sources := config.ToggleKeys
	writeSources := func(w http.ResponseWriter) {
		cfg := d.Cfg.Get()
		enabled := make(map[string]bool, len(sources))
		for _, source := range sources {
			enabled[source] = !player.IsBlocked(player.Track{Source: source}, cfg.Sources.Blocked)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "Yozora", "enabled": enabled, "order": cfg.Sources.Order})
	}
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if d.Token == "" || (!constantTimeEqual(r.URL.Query().Get("t"), d.Token) && !constantTimeEqual(r.Header.Get("X-Uika-Token"), d.Token)) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	// Fonts are public OFL files, so they need no token; only exact embedded names are served.
	mux.HandleFunc("/fonts/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method", 405)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/fonts/")
		if strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".woff2") {
			http.NotFound(w, r)
			return
		}
		b, err := fontFiles.ReadFile("fonts/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "font/woff2")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(b)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if d.Token == "" || !constantTimeEqual(r.URL.Query().Get("t"), d.Token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "dashboard nonce unavailable", http.StatusInternalServerError)
			return
		}
		nonce := base64.RawURLEncoding.EncodeToString(b)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; img-src 'self' http: https:; font-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		t, err := template.New("ui").Parse(uiHTML)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		ui := d.Cfg.Get().UI
		_ = t.Execute(w, map[string]any{"Token": d.Token, "App": d.DiscordApp, "Nonce": nonce, "Theme": ui.Theme, "Motion": ui.Motion,
			"Language": ui.Language, "Lang": pageLang(ui.Language, r.Header.Get("Accept-Language")), "Version": version.Version})
	})
	mux.HandleFunc("/api/state", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stateResponse{Status: d.Engine.Status(), Spotify: d.spotifyState(), Restored: d.Cfg.Restored()})
	}))
	mux.HandleFunc("/api/sources", auth(func(w http.ResponseWriter, r *http.Request) {
		configMu.Lock()
		defer configMu.Unlock()
		switch r.Method {
		case http.MethodGet:
			writeSources(w)
		case http.MethodPost:
			var payload struct {
				Enabled map[string]*bool `json:"enabled"`
				Order   []string         `json:"order"`
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				http.Error(w, "source payload too large or unreadable", 413)
				return
			}
			decoder := json.NewDecoder(strings.NewReader(string(body)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil {
				http.Error(w, "invalid sources", 400)
				return
			}
			if payload.Enabled == nil && payload.Order == nil {
				http.Error(w, "empty sources payload", 400)
				return
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				http.Error(w, "trailing source payload", 400)
				return
			}
			if payload.Enabled != nil {
				for key, enabled := range payload.Enabled {
					known := false
					for _, source := range sources {
						if key == source {
							known = true
							break
						}
					}
					if !known || enabled == nil {
						http.Error(w, "unknown source or non-boolean switch", 400)
						return
					}
				}
			}
			if payload.Order != nil {
				if err := validateSourceOrder(payload.Order, canonicalSources); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
			}
			current, err := config.Load(d.CfgPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if payload.Enabled != nil {
				blocked := make([]string, 0, len(current.Sources.Blocked)+len(payload.Enabled))
				for _, block := range current.Sources.Blocked {
					if _, changed := payload.Enabled[strings.ToLower(strings.TrimSpace(block))]; !changed {
						blocked = append(blocked, block)
					}
				}
				for _, source := range sources {
					if enabled, changed := payload.Enabled[source]; changed && !*enabled {
						blocked = append(blocked, source)
					}
				}
				current.Sources.Blocked = blocked
			}
			if payload.Order != nil {
				current.Sources.Order = payload.Order
			}
			if err := config.Save(current, d.CfgPath); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err := d.Cfg.Reload(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeSources(w)
		default:
			http.Error(w, "method", 405)
		}
	}))
	// Appearance is the panel's theme, motion mode and language. A POST may change any of them; an invalid
	// field rejects the whole request, so a partly valid body never saves half of itself.
	mux.HandleFunc("/api/appearance", auth(func(w http.ResponseWriter, r *http.Request) {
		configMu.Lock()
		defer configMu.Unlock()
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var payload struct {
				Theme    *string `json:"theme"`
				Motion   *string `json:"motion"`
				Language *string `json:"language"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil || (payload.Theme == nil && payload.Motion == nil && payload.Language == nil) ||
				(payload.Theme != nil && !config.ValidTheme(*payload.Theme)) || (payload.Motion != nil && !config.ValidMotion(*payload.Motion)) ||
				(payload.Language != nil && !config.ValidLanguage(*payload.Language)) {
				http.Error(w, "invalid appearance", 400)
				return
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				http.Error(w, "trailing appearance payload", 400)
				return
			}
			current, err := config.Load(d.CfgPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if payload.Theme != nil {
				current.UI.Theme = *payload.Theme
			}
			if payload.Motion != nil {
				current.UI.Motion = *payload.Motion
			}
			if payload.Language != nil {
				current.UI.Language = *payload.Language
			}
			if err := config.Save(current, d.CfgPath); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err := d.Cfg.Reload(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		default:
			http.Error(w, "method", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		ui := d.Cfg.Get().UI
		_ = json.NewEncoder(w).Encode(map[string]any{"theme": ui.Theme, "themes": config.Themes, "motion": ui.Motion, "motions": config.Motions,
			"language": ui.Language, "languages": config.Languages})
	}))
	mux.HandleFunc("/api/autostart", auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var payload struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&payload); err != nil {
				http.Error(w, "invalid autostart payload", 400)
				return
			}
			apply := sysutil.RemoveAutostart
			if payload.Enabled {
				apply = sysutil.InstallAutostart
			}
			if err := apply(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		default:
			http.Error(w, "method", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": sysutil.AutostartEnabled()})
	}))
	registerSetup(mux, d, &configMu, auth)
	registerCard(mux, d, &configMu, auth)
	return mux
}

// pageLang is the lang attribute the panel is first drawn with. "system" follows the
// browser's own language, which the page script confirms from navigator.language.
func pageLang(setting, acceptLanguage string) string {
	if setting == "en" || setting == "ja" {
		return setting
	}
	first, _, _ := strings.Cut(acceptLanguage, ",")
	first, _, _ = strings.Cut(strings.TrimSpace(first), ";")
	if tag := strings.ToLower(first); tag == "ja" || strings.HasPrefix(tag, "ja-") {
		return "ja"
	}
	return "en"
}

func Start(d Deps, port int) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	base := "http://" + ln.Addr().String()
	d.RedirectBase = base
	srv := &http.Server{Handler: Handler(d), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return base, nil
}
