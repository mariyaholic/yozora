//go:build windows

package config

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"

	"uika-resonance/internal/player"
)

const defaultDiscordClientID = "1556599139436077056"

type Discord struct {
	ClientID   string            `toml:"client_id"`
	Apps       map[string]string `toml:"apps"`
	Names      map[string]string `toml:"names"`
	AppName    string            `toml:"app_name"`
	SmallImage string            `toml:"small_image"`
}

type Presence struct {
	Type            string `toml:"type"`
	StatusDisplay   string `toml:"status_display"`
	MinUpdateSecs   int    `toml:"min_update_secs"`
	TrackDebounceMS int    `toml:"track_debounce_ms"`
	DriftResyncSecs int    `toml:"drift_resync_secs"`
	IdleClearSecs   int    `toml:"idle_clear_secs"`
	PausedMode      string `toml:"paused_mode"`
}

type Template struct {
	Details   string `toml:"details"`
	State     string `toml:"state"`
	LargeText string `toml:"large_text"`
	SmallText string `toml:"small_text"`
	Paused    string `toml:"paused_details"`
}

type Buttons struct {
	Enabled       bool   `toml:"enabled"`
	Label         string `toml:"label"`
	SpotifySearch bool   `toml:"spotify_search_fallback"`
	YozoraURL     string `toml:"yozora_url"`
}

var OrderSources = []string{"applemusic", "spotify", "spotifyapi", "youtubemusic", "browser", "generic"}

var ToggleKeys = append(append([]string(nil), OrderSources...), "youtube", "youtube_tabs", "twitch")

var defaultBlocked = []string{"browser", "generic", "youtube", "youtube_tabs", "twitch"}

type Sources struct {
	Known   []string `toml:"known"`
	PollMS  int      `toml:"poll_ms"`
	Order   []string `toml:"order"`
	Blocked []string `toml:"blocked"`
	// Hidden keeps matching tracks off Discord; see player.HiddenRule.
	Hidden []player.HiddenRule `toml:"hidden"`
}

// Limits on what the panel accepts for the Discord card and the hidden list.
const (
	MaxHiddenRules = 200
	MaxHiddenField = 128
	MaxTemplate    = 256
)

var (
	pausedModes    = []string{"show", "hide", "clear"}
	activityTypes  = []string{"listening", "watching", "playing"}
	statusDisplays = []string{"app", "name", "state", "details"}
)

// ValidPausedMode, ValidActivityType and ValidStatusDisplay report whether id is a value the
// engine acts on. "name" is the same as "app" (Discord's own word for it); both are kept.
func ValidPausedMode(id string) bool    { return slices.Contains(pausedModes, id) }
func ValidActivityType(id string) bool  { return slices.Contains(activityTypes, id) }
func ValidStatusDisplay(id string) bool { return slices.Contains(statusDisplays, id) }

// ValidateHidden checks a hidden list the panel is about to save: a bounded count, bounded
// fields, and rules that can match something.
func ValidateHidden(rules []player.HiddenRule) error {
	if len(rules) > MaxHiddenRules {
		return fmt.Errorf("at most %d hidden rules", MaxHiddenRules)
	}
	for _, r := range rules {
		if err := ValidateHiddenRule(r); err != nil {
			return err
		}
	}
	return nil
}

// ValidateHiddenRule checks one rule.
func ValidateHiddenRule(r player.HiddenRule) error {
	for _, f := range []string{r.Artist, r.Title} {
		if len([]rune(f)) > MaxHiddenField {
			return fmt.Errorf("a hidden rule field is at most %d characters", MaxHiddenField)
		}
		if strings.ContainsFunc(f, func(c rune) bool { return c < ' ' || c == 0x7f }) {
			return errors.New("a hidden rule field has a control character")
		}
	}
	if !r.Usable() {
		return errors.New("a hidden rule needs an artist or a title")
	}
	return nil
}

type Spotify struct {
	Mode     string `toml:"mode"`
	ClientID string `toml:"client_id"`
	PollSecs int    `toml:"poll_secs"`
	Market   string `toml:"market"`
}

type Art struct {
	Prefer  string `toml:"prefer"`
	CacheMB int    `toml:"cache_mb"`
	Port    int    `toml:"port"`
	// Country is the iTunes storefront (ISO 3166 alpha-2). Empty follows the Windows region.
	Country string `toml:"country"`
}

type Server struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"`
}

type Log struct {
	Level string `toml:"level"`
}

// Update controls the daily check for a newer release. It only looks; installing
// is always the user's own action in the panel or tray.
type Update struct {
	Check bool `toml:"check"`
}

// UI holds control panel preferences. Theme must be one of Themes.
type UI struct {
	Theme  string `toml:"theme"`
	Motion string `toml:"motion"`
	// Language is the panel and tray language; one of Languages.
	Language string `toml:"language"`
	// Onboarded hides the panel's first-run checklist once its checks have passed.
	Onboarded bool `toml:"onboarded"`
}

// DefaultMotion follows the Windows "Show animations" setting.
const DefaultMotion = "system"

// Motions lists the control panel motion modes: follow Windows, always reduced, or always full.
var Motions = []string{DefaultMotion, "reduced", "full"}

// ValidMotion reports whether id names a control panel motion mode.
func ValidMotion(id string) bool { return slices.Contains(Motions, id) }

// DefaultLanguage follows the Windows display language.
const DefaultLanguage = "system"

// Languages lists the panel and tray languages: follow Windows, English, or Japanese.
var Languages = []string{DefaultLanguage, "en", "ja"}

// ValidLanguage reports whether id names a panel language.
func ValidLanguage(id string) bool { return slices.Contains(Languages, id) }

// DefaultTheme is Yozora's own night palette.
const DefaultTheme = "yozora"

// Themes lists the control panel palettes in picker order: dark, then light. The ids match
// the data-theme blocks in internal/server/ui.html.
var Themes = []string{
	DefaultTheme, "uika", "pine", "kurisu", "ava", "astra", "mariyaholic-98",
	"sumimi", "ledger", "confluence", "astra-light",
}

// mergedThemes maps retired theme ids to the theme that absorbed them.
var mergedThemes = map[string]string{"panel": DefaultTheme, "residual": "astra", "residual-light": "astra-light"}

// ValidTheme reports whether id names a control panel theme.
func ValidTheme(id string) bool { return slices.Contains(Themes, id) }

type Config struct {
	Discord  Discord  `toml:"discord"`
	Presence Presence `toml:"presence"`
	Template Template `toml:"template"`
	Buttons  Buttons  `toml:"buttons"`
	Sources  Sources  `toml:"sources"`
	Spotify  Spotify  `toml:"spotify"`
	Art      Art      `toml:"art"`
	Server   Server   `toml:"server"`
	Log      Log      `toml:"log"`
	UI       UI       `toml:"ui"`
	Update   Update   `toml:"update"`
}

func Defaults() *Config {
	c := &Config{}
	c.Discord.ClientID = defaultDiscordClientID
	c.Discord.AppName = "Yozora"
	c.Discord.SmallImage = "https://cdn.discordapp.com/app-icons/1556599139436077056/9216f465d16f648721c7a3ab607dffa3.png"
	c.Presence.Type = "listening"
	c.Presence.StatusDisplay = "app"
	c.Presence.MinUpdateSecs = 5
	c.Presence.TrackDebounceMS = 500
	c.Presence.DriftResyncSecs = 5
	c.Presence.IdleClearSecs = 90
	c.Presence.PausedMode = "show"
	c.Template.Details = "{{.Title}}"
	c.Template.State = "{{.Artist}}"
	c.Template.LargeText = "{{.Album}}"
	c.Template.SmallText = "{{.Player}}"
	c.Template.Paused = "⏸ {{.Title}}"
	c.Buttons.Enabled = true
	c.Buttons.Label = "Listen along"
	c.Buttons.SpotifySearch = true

	c.Buttons.YozoraURL = "https://github.com/mariyaholic/yozora"
	c.Sources.PollMS = 1000
	c.Sources.Order = slices.Clone(OrderSources)
	c.Sources.Blocked = slices.Clone(defaultBlocked)
	c.Sources.Known = slices.Clone(ToggleKeys)
	c.Spotify.Mode = "system"
	c.Spotify.PollSecs = 12
	c.Spotify.Market = "from_token"
	c.Art.Prefer = "cdn"
	c.Art.CacheMB = 10
	c.Log.Level = "info"
	c.Update.Check = true
	c.UI.Theme = DefaultTheme
	c.UI.Motion = DefaultMotion
	c.UI.Language = DefaultLanguage
	return c
}

func Paths() (cfgPath, dataDir, cacheDir string) {
	appdata := os.Getenv("APPDATA")
	local := os.Getenv("LOCALAPPDATA")
	if appdata == "" {
		appdata = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	if local == "" {
		local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	dataDir = filepath.Join(local, "uika-resonance")
	cfgPath = filepath.Join(appdata, "uika-resonance", "cadence.toml")
	cacheDir = filepath.Join(local, "uika-resonance", "art")
	return
}

func Load(path string) (*Config, error) {
	c, _, err := load(path)
	return c, err
}

// decodeError marks a file that was read but is not a valid config, as opposed
// to one that could not be read at all. Only the former is worth restoring over:
// a file another program has locked is not broken. The message is unchanged.
type decodeError struct{ error }

func (e decodeError) Unwrap() error { return e.error }

// load also returns the exact bytes it parsed, so the last-good copy is the file
// that was validated even if it changes on disk a moment later.
func load(path string) (*Config, []byte, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err == nil {
		c.Sources.Blocked = nil
		c.Sources.Known = nil
		if err := toml.Unmarshal(b, c); err != nil {
			return nil, nil, decodeError{err}
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	} else {
		b = nil
	}
	c.normalize()
	return c, b, nil
}

func Save(c *Config, path string) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	if err := writeAtomic(path, buf.Bytes()); err != nil {
		return err
	}
	keepLastGood(path, buf.Bytes())
	return nil
}

// LastGoodPath is where the most recent config that loaded or saved cleanly is kept.
func LastGoodPath(path string) string { return path + ".last-good" }

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cfg-*.toml")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// keepLastGood is best effort: failing to refresh the safety copy must never
// fail the load or save that produced it. Unchanged content is not rewritten.
func keepLastGood(path string, b []byte) {
	if b == nil {
		return
	}
	lg := LastGoodPath(path)
	if old, err := os.ReadFile(lg); err == nil && bytes.Equal(old, b) {
		return
	}
	if err := writeAtomic(lg, b); err != nil {
		log.Printf("config: could not update %s: %v", lg, err)
	}
}

// Restore describes a startup that fell back to the last-good config.
type Restore struct {
	At         time.Time `json:"at"`
	BrokenPath string    `json:"broken_path"`
	Err        string    `json:"error"`
}

// restoreLastGood handles a config file that no longer parses: the broken file
// is moved aside under a timestamped name for the user to fix, and the last-good
// copy takes its place so the app starts with the settings it last ran with.
func restoreLastGood(path string, loadErr error, now time.Time) (*Config, *Restore, error) {
	b, err := os.ReadFile(LastGoodPath(path))
	if err != nil {
		return nil, nil, fmt.Errorf("%w (no usable last-good copy: %v)", loadErr, err)
	}
	c := Defaults()
	c.Sources.Blocked = nil
	c.Sources.Known = nil
	if err := toml.Unmarshal(b, c); err != nil {
		return nil, nil, fmt.Errorf("%w (last-good copy is invalid too: %v)", loadErr, err)
	}
	c.normalize()
	broken := path + ".broken-" + now.Format("20060102-150405")
	if err := os.Rename(path, broken); err != nil {
		return nil, nil, fmt.Errorf("%w (could not move it aside: %v)", loadErr, err)
	}
	if err := writeAtomic(path, b); err != nil {
		// Put the user's file back rather than leave no config at all.
		_ = os.Rename(broken, path)
		return nil, nil, fmt.Errorf("%w (could not restore last-good copy: %v)", loadErr, err)
	}
	return c, &Restore{At: now, BrokenPath: broken, Err: loadErr.Error()}, nil
}

func (c *Config) normalize() {
	if c.Discord.ClientID == "" {
		c.Discord.ClientID = defaultDiscordClientID
	}
	if c.Discord.AppName == "Uika Resonance" {
		c.Discord.AppName = "Yozora"
	}

	if c.Presence.MinUpdateSecs < 5 {
		c.Presence.MinUpdateSecs = 5
	}
	if c.Sources.PollMS <= 0 {
		c.Sources.PollMS = 1000
	} else if c.Sources.PollMS < 500 {
		c.Sources.PollMS = 500
	}
	if c.Presence.TrackDebounceMS <= 0 {
		c.Presence.TrackDebounceMS = 500
	}
	if c.Presence.DriftResyncSecs <= 0 {
		c.Presence.DriftResyncSecs = 5
	}
	if c.Presence.IdleClearSecs <= 0 {
		c.Presence.IdleClearSecs = 90
	}
	if c.Art.CacheMB <= 0 {
		c.Art.CacheMB = 4
	}
	c.UI.Theme = strings.ToLower(strings.TrimSpace(c.UI.Theme))
	if merged, ok := mergedThemes[c.UI.Theme]; ok {
		c.UI.Theme = merged
	}
	if !ValidTheme(c.UI.Theme) {
		c.UI.Theme = DefaultTheme
	}
	c.UI.Motion = strings.ToLower(strings.TrimSpace(c.UI.Motion))
	if !ValidMotion(c.UI.Motion) {
		c.UI.Motion = DefaultMotion
	}
	c.UI.Language = strings.ToLower(strings.TrimSpace(c.UI.Language))
	if !ValidLanguage(c.UI.Language) {
		c.UI.Language = DefaultLanguage
	}
	if c.Spotify.PollSecs < 5 {
		c.Spotify.PollSecs = 12
	}
	order := make([]string, 0, len(OrderSources))
	for _, source := range c.Sources.Order {
		source = strings.ToLower(strings.TrimSpace(source))
		if slices.Contains(OrderSources, source) && !slices.Contains(order, source) {
			order = append(order, source)
		}
	}
	c.Sources.Order = order
	if len(c.Sources.Order) == 0 {
		c.Sources.Order = slices.Clone(OrderSources)
	}
	if c.Sources.Known == nil {
		c.Sources.Known = []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"}
	}
	for _, key := range ToggleKeys {
		if slices.Contains(OrderSources, key) && !slices.Contains(c.Sources.Order, key) {
			index := len(c.Sources.Order)
			if key == "youtubemusic" {
				if i := slices.Index(c.Sources.Order, "browser"); i >= 0 {
					index = i
				}
			}
			c.Sources.Order = slices.Insert(c.Sources.Order, index, key)
		}
		if slices.Contains(c.Sources.Known, key) {
			continue
		}
		if slices.Contains(defaultBlocked, key) && !slices.ContainsFunc(c.Sources.Blocked, func(blocked string) bool {
			return strings.ToLower(strings.TrimSpace(blocked)) == key
		}) {
			c.Sources.Blocked = append(c.Sources.Blocked, key)
		}
	}
	c.Sources.Known = slices.Clone(ToggleKeys)
	// Hand-edited rules: trim them, and drop any that could never match so they stay out of the panel.
	var hidden []player.HiddenRule
	for _, r := range c.Sources.Hidden {
		r.Artist, r.Title = strings.TrimSpace(r.Artist), strings.TrimSpace(r.Title)
		if r.Usable() {
			hidden = append(hidden, r)
		}
	}
	c.Sources.Hidden = hidden
}

type Holder struct {
	path     string
	val      atomic.Pointer[Config]
	restored *Restore

	reloadMu sync.Mutex
	applying bool
	pending  *Config
	hookMu   sync.Mutex
	onApply  []func(*Config)
}

// OnChange registers fn to apply a changed config. Concurrent or reentrant reloads
// coalesce while callbacks run, so callbacks apply the latest config in order.
// The reload dispatching callbacks waits for them; another reload can return
// after publication, before its callbacks run.
func (h *Holder) OnChange(fn func(*Config)) {
	h.hookMu.Lock()
	h.onApply = append(h.onApply, fn)
	h.hookMu.Unlock()
}

// NewHolder loads the config. If the file exists but no longer parses, it falls
// back to the last-good copy (see restoreLastGood) and logs that loudly; without
// a usable copy the original error is returned as before.
func NewHolder(path string) (*Holder, error) {
	h := &Holder{path: path}
	c, b, err := load(path)
	if err != nil {
		var de decodeError
		if !errors.As(err, &de) {
			return nil, err
		}
		var r *Restore
		c, r, err = restoreLastGood(path, err, time.Now())
		if err != nil {
			return nil, err
		}
		h.restored = r
		log.Printf("config: WARNING %s is invalid (%s). Started with the last good copy; your edited file was kept as %s", path, r.Err, r.BrokenPath)
	} else {
		keepLastGood(path, b)
	}
	h.val.Store(c)
	return h, nil
}

func (h *Holder) Get() *Config { return h.val.Load() }
func (h *Holder) Path() string { return h.path }

// Restored reports whether startup fell back to the last-good config, and why.
// It is nil after a normal start.
func (h *Holder) Restored() *Restore { return h.restored }

func (h *Holder) Reload() (bool, error) {
	return h.reload(load)
}

func (h *Holder) reload(loadConfig func(string) (*Config, []byte, error)) (bool, error) {
	// Serialize the read and publication so an older, slower load cannot
	// overwrite a newer one (or its last-good copy).
	h.reloadMu.Lock()
	c, b, err := loadConfig(h.path)
	if err != nil {
		h.reloadMu.Unlock()
		return false, err
	}
	keepLastGood(h.path, b)
	old := h.val.Load()
	changed := !reflect.DeepEqual(old, c)
	dispatch := false
	if changed {
		h.val.Store(c)
		h.pending = c
		if !h.applying {
			h.applying = true
			dispatch = true
		}
	}
	// Hooks can themselves reload, so they must run outside reloadMu. The
	// dispatcher is the only callback runner; intervening reloads replace pending.
	h.reloadMu.Unlock()
	if dispatch {
		h.applyChanges()
	}
	return changed, nil
}

func (h *Holder) applyChanges() {
	for {
		h.reloadMu.Lock()
		c := h.pending
		h.pending = nil
		if c == nil {
			h.applying = false
			h.reloadMu.Unlock()
			return
		}
		h.reloadMu.Unlock()
		h.hookMu.Lock()
		hooks := slices.Clone(h.onApply)
		h.hookMu.Unlock()
		for _, fn := range hooks {
			fn(c)
		}
	}
}

func (h *Holder) Watch(stop <-chan struct{}) {
	var last time.Time
	if st, err := os.Stat(h.path); err == nil {
		last = st.ModTime()
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			st, err := os.Stat(h.path)
			if err != nil {
				continue
			}
			if !st.ModTime().Equal(last) {
				last = st.ModTime()
				changed, err := h.Reload()
				if err != nil {
					log.Printf("config: reload failed: %v", err)
					continue
				}
				if changed {
					log.Printf("config: reloaded %s", h.path)
				}
			}
		}
	}
}
