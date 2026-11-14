//go:build windows

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
)

const settingsMaxBytes = 64 << 10
const settingsFormat = "yozora-panel-settings"

type portableSources struct {
	Enabled map[string]bool `json:"enabled"`
	Order   []string        `json:"order"`
}
type portableAppearance struct {
	Theme    string `json:"theme"`
	Motion   string `json:"motion"`
	Language string `json:"language"`
}
type portableSettings struct {
	Format     string             `json:"format"`
	Version    int                `json:"version"`
	Card       cardSettings       `json:"card"`
	Sources    portableSources    `json:"sources"`
	Appearance portableAppearance `json:"appearance"`
}

func settingsFromConfig(c *config.Config) portableSettings {
	enabled := make(map[string]bool, len(config.ToggleKeys))
	for _, key := range config.ToggleKeys {
		enabled[key] = !player.IsBlocked(player.Track{Source: key}, c.Sources.Blocked)
	}
	return portableSettings{settingsFormat, 1, cardFromConfig(c), portableSources{enabled, slices.Clone(c.Sources.Order)},
		portableAppearance{c.UI.Theme, c.UI.Motion, c.UI.Language}}
}

// Hash the projection incrementally, so a hand-edited, oversized template can
// still be replaced via import without serializing or returning it in preview.
func settingsTag(s portableSettings) string {
	h := sha256.New()
	addString := func(value string) { hashSettingsString(h, value) }
	addBool := func(value bool) {
		if value {
			_, _ = h.Write([]byte{1})
		} else {
			_, _ = h.Write([]byte{0})
		}
	}
	addString(s.Format)
	addString(fmt.Sprint(s.Version))
	c := s.Card
	for _, value := range []string{c.PausedMode, c.Type, c.StatusDisplay} {
		addString(value)
	}
	addBool(c.ButtonsOn)
	addString(c.ButtonLabel)
	addBool(c.SpotifySearch)
	for _, value := range []string{c.Details, c.State, c.LargeText, c.SmallText, c.PausedDetails} {
		addString(value)
	}
	for _, key := range config.ToggleKeys {
		addString(key)
		addBool(s.Sources.Enabled[key])
	}
	for _, key := range s.Sources.Order {
		addString(key)
	}
	for _, value := range []string{s.Appearance.Theme, s.Appearance.Motion, s.Appearance.Language} {
		addString(value)
	}
	return fmt.Sprintf("\"%x\"", h.Sum(nil))
}

func hashSettingsString(h hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	var buf [4096]byte
	for len(value) > 0 {
		n := copy(buf[:], value)
		_, _ = h.Write(buf[:n])
		value = value[n:]
	}
}

// Export only a document that the import validator would accept. Check large
// strings before marshaling so a hand-edited config cannot create an unbounded response.
func encodeSettingsExport(s portableSettings) ([]byte, error) {
	c := s.Card
	for _, value := range []string{c.PausedMode, c.Type, c.StatusDisplay, c.ButtonLabel, c.Details, c.State, c.LargeText, c.SmallText, c.PausedDetails} {
		if len(value) > settingsMaxBytes {
			return nil, errors.New("saved panel settings cannot be exported; fix invalid card settings first")
		}
	}
	data, err := json.Marshal(s)
	if err != nil || len(data) > settingsMaxBytes {
		return nil, errors.New("saved panel settings cannot be exported; fix invalid card settings first")
	}
	decoded, err := decodeSettings(data)
	if err != nil || !reflect.DeepEqual(decoded, s) {
		return nil, errors.New("saved panel settings cannot be exported; fix invalid card settings first")
	}
	return data, nil
}

// strictJSON rejects duplicate object keys at every level, including keys that use
// different JSON escapes. Exact field names are then checked by settingsObject.
func strictJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate key")
				}
				seen[name] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("invalid object")
			}
		case '[':
			for dec.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("invalid array")
			}
		default:
			return errors.New("invalid value")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func settingsObject(data []byte, names ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil || len(fields) != len(names) {
		return nil, errors.New("invalid object")
	}
	for _, name := range names {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, errors.New("missing field")
		}
	}
	return fields, nil
}

func decodeSettings(data []byte) (portableSettings, error) {
	invalid := errors.New("invalid settings document")
	if !utf8.Valid(data) {
		return portableSettings{}, invalid
	}
	if err := strictJSON(data); err != nil {
		return portableSettings{}, invalid
	}
	root, err := settingsObject(data, "format", "version", "card", "sources", "appearance")
	if err != nil {
		return portableSettings{}, invalid
	}
	var result portableSettings
	if json.Unmarshal(root["format"], &result.Format) != nil || result.Format != settingsFormat ||
		json.Unmarshal(root["version"], &result.Version) != nil || result.Version != 1 {
		return portableSettings{}, errors.New("unsupported settings format or version")
	}
	if _, err = settingsObject(root["card"], "paused_mode", "type", "status_display", "buttons_enabled", "button_label", "spotify_search_fallback", "details", "state", "large_text", "small_text", "paused_details"); err != nil {
		return portableSettings{}, invalid
	}
	result.Card, err = decodeCard(bytes.NewReader(root["card"]))
	if err != nil {
		return portableSettings{}, errors.New("invalid card settings")
	}
	// The card route accepts legacy aliases but presents their effective values.
	if result.Card.PausedMode == "clear" {
		result.Card.PausedMode = "hide"
	}
	if result.Card.StatusDisplay == "name" {
		result.Card.StatusDisplay = "app"
	}
	src, err := settingsObject(root["sources"], "enabled", "order")
	if err != nil {
		return portableSettings{}, invalid
	}
	if _, err = settingsObject(src["enabled"], config.ToggleKeys...); err != nil {
		return portableSettings{}, errors.New("invalid source switches")
	}
	if json.Unmarshal(src["enabled"], &result.Sources.Enabled) != nil || json.Unmarshal(src["order"], &result.Sources.Order) != nil ||
		validateSourceOrder(result.Sources.Order, canonicalSources) != nil {
		return portableSettings{}, errors.New("invalid sources")
	}
	appearance, err := settingsObject(root["appearance"], "theme", "motion", "language")
	if err != nil || json.Unmarshal(appearance["theme"], &result.Appearance.Theme) != nil || json.Unmarshal(appearance["motion"], &result.Appearance.Motion) != nil ||
		json.Unmarshal(appearance["language"], &result.Appearance.Language) != nil || !config.ValidTheme(result.Appearance.Theme) ||
		!config.ValidMotion(result.Appearance.Motion) || !config.ValidLanguage(result.Appearance.Language) {
		return portableSettings{}, errors.New("invalid appearance")
	}
	return result, nil
}

func (s portableSettings) apply(c *config.Config) {
	s.Card.apply(c)
	blocked := make([]string, 0, len(c.Sources.Blocked)+len(config.ToggleKeys))
	for _, entry := range c.Sources.Blocked {
		if !slices.Contains(config.ToggleKeys, strings.ToLower(strings.TrimSpace(entry))) {
			blocked = append(blocked, entry)
		}
	}
	for _, key := range config.ToggleKeys {
		if !s.Sources.Enabled[key] {
			blocked = append(blocked, key)
		}
	}
	c.Sources.Blocked = blocked
	c.Sources.Order = slices.Clone(s.Sources.Order)
	c.UI.Theme, c.UI.Motion, c.UI.Language = s.Appearance.Theme, s.Appearance.Motion, s.Appearance.Language
}

func registerSettings(mux *http.ServeMux, d Deps, mu *sync.Mutex, auth func(http.HandlerFunc) http.HandlerFunc) {
	registerSettingsWithSave(mux, d, mu, auth, config.Save)
}

func registerSettingsWithSave(mux *http.ServeMux, d Deps, mu *sync.Mutex, auth func(http.HandlerFunc) http.HandlerFunc, save func(*config.Config, string) error) {
	writeJSON := func(w http.ResponseWriter, v any, tag string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("ETag", tag)
		_ = json.NewEncoder(w).Encode(v)
	}
	readCurrent := func(w http.ResponseWriter) (*config.Config, bool) {
		cfg, err := config.Load(d.CfgPath)
		if err != nil {
			http.Error(w, "could not load settings", 500)
			return nil, false
		}
		return cfg, true
	}
	readCandidate := func(w http.ResponseWriter, r *http.Request) (portableSettings, bool) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, settingsMaxBytes))
		if err != nil {
			http.Error(w, "settings document too large or unreadable", 413)
			return portableSettings{}, false
		}
		s, err := decodeSettings(data)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return portableSettings{}, false
		}
		return s, true
	}
	mux.HandleFunc("/api/settings", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		cfg, ok := readCurrent(w)
		if !ok {
			return
		}
		s := settingsFromConfig(cfg)
		data, err := encodeSettingsExport(s)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="yozora-panel-settings.json"`)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("ETag", settingsTag(s))
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("/api/settings/preview", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		candidate, ok := readCandidate(w, r)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		cfg, ok := readCurrent(w)
		if !ok {
			return
		}
		current := settingsFromConfig(cfg)
		summary := map[string]bool{"card_changed": !reflect.DeepEqual(candidate.Card, current.Card), "sources_changed": !reflect.DeepEqual(candidate.Sources, current.Sources), "appearance_changed": !reflect.DeepEqual(candidate.Appearance, current.Appearance)}
		writeJSON(w, map[string]any{"candidate": candidate, "summary": summary}, settingsTag(current))
	}))
	mux.HandleFunc("/api/settings/import", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		condition := r.Header.Get("If-Match")
		if condition == "" {
			http.Error(w, "preview required", 428)
			return
		}
		candidate, ok := readCandidate(w, r)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		cfg, ok := readCurrent(w)
		if !ok {
			return
		}
		if condition != settingsTag(settingsFromConfig(cfg)) {
			http.Error(w, "settings changed; preview again", 412)
			return
		}
		candidate.apply(cfg)
		if err := save(cfg, d.CfgPath); err != nil {
			http.Error(w, "could not save settings", 500)
			return
		}
		if _, err := d.Cfg.Reload(); err != nil {
			http.Error(w, "settings saved but could not reload; preview again", 500)
			return
		}
		s := settingsFromConfig(d.Cfg.Get())
		writeJSON(w, s, settingsTag(s))
	}))
}
