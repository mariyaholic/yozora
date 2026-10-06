//go:build windows

package server

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
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
)

//go:embed ui.html
var uiHTML string

type Deps struct {
	Engine     *presence.Engine
	Cfg        *config.Holder
	CfgPath    string
	Token      string
	DiscordApp string
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
	sources := canonicalSources
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
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; img-src 'self' http: https:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		t, err := template.New("ui").Parse(uiHTML)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = t.Execute(w, map[string]any{"Token": d.Token, "App": d.DiscordApp, "Nonce": nonce})
	})
	mux.HandleFunc("/api/state", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.Engine.Status())
	}))
	mux.HandleFunc("/api/config", auth(func(w http.ResponseWriter, r *http.Request) {
		configMu.Lock()
		defer configMu.Unlock()
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/toml")
			c := d.Cfg.Get()
			_ = c
			fmt.Fprint(w, "# Read the file at "+d.CfgPath+" — write via POST here.\n")
		case http.MethodPost:
			c := config.Defaults()
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := json.NewDecoder(r.Body).Decode(c); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if err := config.Save(c, d.CfgPath); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_, _ = d.Cfg.Reload()
			fmt.Fprint(w, "ok")
		default:
			http.Error(w, "method", 405)
		}
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
				if err := validateSourceOrder(payload.Order, sources); err != nil {
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
	return mux
}

func Start(d Deps, port int) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: Handler(d), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}
