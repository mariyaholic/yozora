//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	cardtemplate "uika-resonance/internal/template"
)

// cardSettings is the panel's Discord card form: the TOML-only presence, button and
// template settings, flattened. The same shape is read, previewed and saved.
type cardSettings struct {
	PausedMode    string `json:"paused_mode"`
	Type          string `json:"type"`
	StatusDisplay string `json:"status_display"`
	ButtonsOn     bool   `json:"buttons_enabled"`
	ButtonLabel   string `json:"button_label"`
	SpotifySearch bool   `json:"spotify_search_fallback"`
	Details       string `json:"details"`
	State         string `json:"state"`
	LargeText     string `json:"large_text"`
	SmallText     string `json:"small_text"`
	PausedDetails string `json:"paused_details"`
}

// cardPayload is cardSettings with every field required: a save replaces the whole form,
// so a missing field is a mistake, not "leave it alone".
type cardPayload struct {
	PausedMode    *string `json:"paused_mode"`
	Type          *string `json:"type"`
	StatusDisplay *string `json:"status_display"`
	ButtonsOn     *bool   `json:"buttons_enabled"`
	ButtonLabel   *string `json:"button_label"`
	SpotifySearch *bool   `json:"spotify_search_fallback"`
	Details       *string `json:"details"`
	State         *string `json:"state"`
	LargeText     *string `json:"large_text"`
	SmallText     *string `json:"small_text"`
	PausedDetails *string `json:"paused_details"`
}

func cardFromConfig(c *config.Config) cardSettings {
	// A hand-edited value the engine would not act on is shown as what the engine does with it.
	mode, typ, display := c.Presence.PausedMode, c.Presence.Type, c.Presence.StatusDisplay
	if !config.ValidPausedMode(mode) {
		mode = "show"
	} else if mode == "clear" {
		mode = "hide" // the engine treats the two alike
	}
	if !config.ValidActivityType(typ) {
		typ = "listening"
	}
	if !config.ValidStatusDisplay(display) || display == "name" {
		display = "app"
	}
	return cardSettings{mode, typ, display, c.Buttons.Enabled, c.Buttons.Label, c.Buttons.SpotifySearch,
		c.Template.Details, c.Template.State, c.Template.LargeText, c.Template.SmallText, c.Template.Paused}
}

type cardPreset struct {
	ID       string       `json:"id"`
	Settings cardSettings `json:"settings"`
}

// Presets are complete forms, based on the same defaults used by Reset. Selecting
// one is only a form edit; saving still goes through the normal card endpoint.
func cardPresets() []cardPreset {
	defaults := cardFromConfig(config.Defaults())
	compact := defaults
	compact.LargeText, compact.SmallText = "", ""
	album := defaults
	album.Details = "{{if .Album}}{{.Album}}{{else}}{{.Title}}{{end}}"
	album.LargeText = "{{.Title}}"
	detailed := defaults
	detailed.State = "{{.Artist}}{{if .Album}} · {{.Album}}{{end}}"
	detailed.LargeText = "{{if .TrackNumber}}#{{.TrackNumber}}{{if .Album}} · {{end}}{{end}}{{.Album}}"
	return []cardPreset{{"default", defaults}, {"compact", compact}, {"album", album}, {"detailed", detailed}}
}

// decodeCard reads and validates a whole card form. The returned error is safe to show.
func decodeCard(body io.Reader) (cardSettings, error) {
	var p cardPayload
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return cardSettings{}, errors.New("invalid card")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return cardSettings{}, errors.New("trailing card payload")
	}
	if p.PausedMode == nil || p.Type == nil || p.StatusDisplay == nil || p.ButtonsOn == nil || p.ButtonLabel == nil || p.SpotifySearch == nil ||
		p.Details == nil || p.State == nil || p.LargeText == nil || p.SmallText == nil || p.PausedDetails == nil {
		return cardSettings{}, errors.New("every card field is required")
	}
	c := cardSettings{*p.PausedMode, *p.Type, *p.StatusDisplay, *p.ButtonsOn, *p.ButtonLabel, *p.SpotifySearch,
		*p.Details, *p.State, *p.LargeText, *p.SmallText, *p.PausedDetails}
	return c, c.validate()
}

func (c cardSettings) validate() error {
	if !config.ValidPausedMode(c.PausedMode) {
		return errors.New("paused_mode must be show, hide or clear")
	}
	if !config.ValidActivityType(c.Type) {
		return errors.New("type must be listening, watching or playing")
	}
	if !config.ValidStatusDisplay(c.StatusDisplay) {
		return errors.New("status_display must be app, state or details")
	}
	if err := plainText("button_label", c.ButtonLabel, cardtemplate.MaxButtonLabel); err != nil {
		return err
	}
	for _, f := range []struct{ name, value string }{
		{"details", c.Details}, {"state", c.State}, {"large_text", c.LargeText}, {"small_text", c.SmallText}, {"paused_details", c.PausedDetails},
	} {
		if err := plainText(f.name, f.value, config.MaxTemplate); err != nil {
			return err
		}
		if err := cardtemplate.Validate(f.value); err != nil {
			return fmt.Errorf("%s is not a valid template: %v", f.name, err)
		}
	}
	return nil
}

func plainText(name, value string, max int) error {
	if len([]rune(value)) > max {
		return fmt.Errorf("%s is at most %d characters", name, max)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s has a control character", name)
	}
	return nil
}

func (c cardSettings) apply(to *config.Config) {
	to.Presence.PausedMode, to.Presence.Type, to.Presence.StatusDisplay = c.PausedMode, c.Type, c.StatusDisplay
	to.Buttons.Enabled, to.Buttons.Label, to.Buttons.SpotifySearch = c.ButtonsOn, c.ButtonLabel, c.SpotifySearch
	to.Template.Details, to.Template.State, to.Template.LargeText, to.Template.SmallText, to.Template.Paused =
		c.Details, c.State, c.LargeText, c.SmallText, c.PausedDetails
}

// sampleTrack stands in for the preview when nothing is playing.
func sampleTrack(now time.Time) player.Track {
	return player.Track{Source: "spotify", Player: "Spotify", Title: "Night Walk", Artist: "Mira", Album: "After Hours",
		TrackNumber: 3, Playing: true, PositionSec: 82, DurationSec: 243, LastUpdated: now}
}

type cardPreview struct {
	Details string `json:"details"`
	State   string `json:"state"`
	Large   string `json:"large_text"`
	Small   string `json:"small_text"`
	Playing bool   `json:"playing"`
	Sample  bool   `json:"sample"`
	Hidden  bool   `json:"hidden"`
}

func registerCard(mux *http.ServeMux, d Deps, configMu *sync.Mutex, auth func(http.HandlerFunc) http.HandlerFunc) {
	jsonOut := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	hiddenList := func(c *config.Config) []player.HiddenRule {
		if c.Sources.Hidden == nil {
			return []player.HiddenRule{}
		}
		return c.Sources.Hidden
	}
	cardState := func() map[string]any {
		cfg := d.Cfg.Get()
		return map[string]any{"card": cardFromConfig(cfg), "defaults": cardFromConfig(config.Defaults()), "presets": cardPresets(), "hidden": hiddenList(cfg),
			"limits": map[string]int{"template": config.MaxTemplate, "label": cardtemplate.MaxButtonLabel, "rules": config.MaxHiddenRules, "field": config.MaxHiddenField}}
	}
	// Saving replaces the whole card form or nothing; the engine reads the config on its next
	// step, so no restart is needed.
	mux.HandleFunc("/api/card", auth(func(w http.ResponseWriter, r *http.Request) {
		configMu.Lock()
		defer configMu.Unlock()
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			card, err := decodeCard(http.MaxBytesReader(w, r.Body, 8192))
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			current, err := config.Load(d.CfgPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			card.apply(current)
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
		jsonOut(w, cardState())
	}))
	// Preview renders a candidate card, unsaved, against the track the panel shows, through the
	// same template engine the card is sent with.
	mux.HandleFunc("/api/card/preview", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		card, err := decodeCard(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		now := time.Now()
		out := cardPreview{}
		track, ok := sampleTrack(now), false
		if d.Engine != nil {
			var live player.Track
			if live, ok = d.Engine.CurrentTrack(); ok {
				track = live
			}
		}
		out.Sample = !ok
		out.Playing = track.Playing
		out.Hidden = player.IsHidden(track, d.Cfg.Get().Sources.Hidden)
		out.Details, out.State, out.Large, out.Small = cardtemplate.New(card.Details, card.State, card.LargeText, card.SmallText, card.PausedDetails).Render(track, now)
		jsonOut(w, out)
	}))
	// Hidden rules change one at a time, by value, so two panels or a hand edit cannot be
	// overwritten by a stale copy of the whole list.
	mux.HandleFunc("/api/hidden", auth(func(w http.ResponseWriter, r *http.Request) {
		configMu.Lock()
		defer configMu.Unlock()
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var payload struct {
				Add    *player.HiddenRule `json:"add"`
				Remove *player.HiddenRule `json:"remove"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil || (payload.Add == nil) == (payload.Remove == nil) {
				http.Error(w, "invalid hidden request", 400)
				return
			}
			if decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "trailing hidden payload", 400)
				return
			}
			current, err := config.Load(d.CfgPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if rule := payload.Add; rule != nil {
				rule.Artist, rule.Title = strings.TrimSpace(rule.Artist), strings.TrimSpace(rule.Title)
				next := append(slices.Clone(current.Sources.Hidden), *rule)
				if !slices.ContainsFunc(current.Sources.Hidden, rule.Same) {
					if err := config.ValidateHidden(next); err != nil {
						http.Error(w, err.Error(), 400)
						return
					}
					current.Sources.Hidden = next
				} else if err := config.ValidateHiddenRule(*rule); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
			} else {
				current.Sources.Hidden = slices.DeleteFunc(slices.Clone(current.Sources.Hidden), payload.Remove.Same)
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
		jsonOut(w, map[string]any{"hidden": hiddenList(d.Cfg.Get())})
	}))
}
