//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
	"uika-resonance/internal/spotifyapi"
)

// Discord application IDs are snowflakes: 17 to 20 digits.
var discordAppIDPattern = regexp.MustCompile(`^[0-9]{17,20}$`)

const spotifyCallbackPath = "/spotify/callback"

// spotifyState is the Web API connection as the panel sees it on every state tick.
type spotifyState struct {
	Available   bool   `json:"available"`
	Connected   bool   `json:"connected"`
	Waiting     bool   `json:"waiting"`
	ClientID    string `json:"client_id,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	Error       string `json:"error,omitempty"`
}

type stateResponse struct {
	presence.Status
	Spotify spotifyState `json:"spotify"`
}

func (d Deps) spotifyAvailable() bool {
	return d.Spotify != nil && d.SpotifyLink != nil && d.RedirectBase != ""
}

func (d Deps) spotifyState() spotifyState {
	s := spotifyState{Available: d.spotifyAvailable()}
	if !s.Available {
		return s
	}
	s.ClientID = d.Cfg.Get().Spotify.ClientID
	s.RedirectURI = d.RedirectBase + spotifyCallbackPath
	s.Connected = d.Spotify.Running()
	link := d.SpotifyLink.Status()
	s.Waiting = link.Waiting
	s.Error = link.Error
	if s.Connected && s.Error == "" {
		s.Error = d.Spotify.Err()
	}
	return s
}

// decodeStrict reads one small JSON object with no unknown fields and nothing after it.
func decodeStrict(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// registerSetup adds the first-run checklist, test card and Spotify connect endpoints.
func registerSetup(mux *http.ServeMux, d Deps, configMu *sync.Mutex, auth func(http.HandlerFunc) http.HandlerFunc) {
	defaultAppID := config.Defaults().Discord.ClientID
	setupBody := func() map[string]any {
		cfg := d.Cfg.Get()
		return map[string]any{"onboarded": cfg.UI.Onboarded, "client_id": cfg.Discord.ClientID, "default_client_id": cfg.Discord.ClientID == defaultAppID}
	}
	// update applies a change to the saved config under the panel's config lock.
	update := func(change func(*config.Config)) error {
		configMu.Lock()
		defer configMu.Unlock()
		current, err := config.Load(d.CfgPath)
		if err != nil {
			return err
		}
		change(current)
		if err := config.Save(current, d.CfgPath); err != nil {
			return err
		}
		_, err = d.Cfg.Reload()
		return err
	}

	mux.HandleFunc("/api/setup", auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var payload struct {
				Onboarded *bool   `json:"onboarded"`
				ClientID  *string `json:"client_id"`
			}
			if !decodeStrict(w, r, 256, &payload) || (payload.Onboarded == nil && payload.ClientID == nil) {
				http.Error(w, "invalid setup payload", 400)
				return
			}
			appID := ""
			if payload.ClientID != nil {
				// An empty ID goes back to Yozora's own Discord application.
				appID = strings.TrimSpace(*payload.ClientID)
				if appID == "" {
					appID = defaultAppID
				}
				if !discordAppIDPattern.MatchString(appID) {
					http.Error(w, "a Discord application ID is 17 to 20 digits", 400)
					return
				}
			}
			if err := update(func(c *config.Config) {
				if payload.Onboarded != nil {
					c.UI.Onboarded = *payload.Onboarded
				}
				if payload.ClientID != nil {
					c.Discord.ClientID = appID
				}
			}); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		default:
			http.Error(w, "method", 405)
			return
		}
		writeJSON(w, 200, setupBody())
	}))

	mux.HandleFunc("/api/test-card", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		if err := d.Engine.SendTestCard(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"state": "sending", "seconds": int(presence.TestCardFor / time.Second)})
	}))

	mux.HandleFunc("/api/spotify", auth(func(w http.ResponseWriter, r *http.Request) {
		if !d.spotifyAvailable() {
			http.Error(w, "spotify connect unavailable", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var payload struct {
				Action   string `json:"action"`
				ClientID string `json:"client_id"`
			}
			if !decodeStrict(w, r, 256, &payload) {
				http.Error(w, "invalid spotify payload", 400)
				return
			}
			switch payload.Action {
			case "connect":
				id := strings.ToLower(strings.TrimSpace(payload.ClientID))
				if !spotifyapi.ValidClientID(id) {
					http.Error(w, "a Spotify client ID is 32 letters and digits (0-9, a-f)", 400)
					return
				}
				authURL, err := d.SpotifyLink.Begin(id, d.RedirectBase+spotifyCallbackPath)
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				if d.OpenURL == nil || d.OpenURL(authURL) != nil {
					d.SpotifyLink.Cancel()
					http.Error(w, "could not open the browser", http.StatusBadGateway)
					return
				}
			case "cancel":
				d.SpotifyLink.Cancel()
			case "disconnect":
				d.SpotifyLink.Cancel()
				d.Spotify.Stop()
				clearToken := d.ClearSpotifyToken
				if clearToken == nil {
					clearToken = spotifyapi.ClearRefreshToken
				}
				// A token that was never stored is already gone; the config change still applies.
				_ = clearToken()
				if err := update(func(c *config.Config) { c.Spotify.Mode = "system" }); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			default:
				http.Error(w, "unknown spotify action", 400)
				return
			}
		default:
			http.Error(w, "method", 405)
			return
		}
		writeJSON(w, 200, d.spotifyState())
	}))

	// Spotify redirects the system browser here, so this route carries no panel token. The
	// single-use state from the flow the panel started is what authorizes it.
	mux.HandleFunc(spotifyCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		page := func(code int, message string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Yozora</title><body style="font:15px system-ui,sans-serif;margin:3em auto;max-width:28em;padding:0 1em"><h1 style="font-size:20px">Yozora</h1><p>`+html.EscapeString(message)+`</p></body>`)
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return
		}
		if !d.spotifyAvailable() {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		clientID, err := d.SpotifyLink.Complete(q.Get("state"), q.Get("code"), q.Get("error"))
		switch {
		case errors.Is(err, spotifyapi.ErrStateMismatch), errors.Is(err, spotifyapi.ErrNoPendingFlow):
			page(http.StatusBadRequest, "This sign-in link is not valid any more. Start again from Yozora.")
			return
		case err != nil:
			page(http.StatusBadGateway, "Spotify was not connected. Return to Yozora to try again.")
			return
		}
		if err := update(func(c *config.Config) {
			c.Spotify.Mode = "api"
			c.Spotify.ClientID = clientID
		}); err != nil {
			page(http.StatusInternalServerError, "Spotify signed in, but Yozora could not save the setting. Return to Yozora to try again.")
			return
		}
		cfg := d.Cfg.Get()
		d.Spotify.Start(clientID, cfg.Spotify.Market, time.Duration(cfg.Spotify.PollSecs)*time.Second)
		page(http.StatusOK, "Spotify is connected. You can close this tab.")
	})
}
