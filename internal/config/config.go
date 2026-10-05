//go:build windows

// Package config holds the TOML configuration with defaults, load/save and
// cheap mtime-based hot reload.
package config

import (
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
)

const defaultDiscordClientID = "1556599139436077056"

type Discord struct {
	ClientID   string            `toml:"client_id"`
	Apps       map[string]string `toml:"apps"`  // per-source client id override
	Names      map[string]string `toml:"names"` // per-source activity name override
	AppName    string            `toml:"app_name"`
	SmallImage string            `toml:"small_image"`
}

type Presence struct {
	Type            string `toml:"type"`           // playing|listening|watching
	StatusDisplay   string `toml:"status_display"` // app|name|state|details
	MinUpdateSecs   int    `toml:"min_update_secs"`
	TrackDebounceMS int    `toml:"track_debounce_ms"`
	DriftResyncSecs int    `toml:"drift_resync_secs"`
	IdleClearSecs   int    `toml:"idle_clear_secs"`
	PausedMode      string `toml:"paused_mode"` // show|hide|clear
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

type Sources struct {
	PollMS  int      `toml:"poll_ms"`
	Order   []string `toml:"order"`
	Blocked []string `toml:"blocked"`
}

type Spotify struct {
	Mode     string `toml:"mode"` // system|api
	ClientID string `toml:"client_id"`
	PollSecs int    `toml:"poll_secs"`
	Market   string `toml:"market"`
}

type Art struct {
	Prefer  string `toml:"prefer"` // cdn|smtc
	CacheMB int    `toml:"cache_mb"`
	Port    int    `toml:"port"` // 0 = random
}

type Server struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"` // 0 = random
}

type Log struct {
	Level string `toml:"level"`
}

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
}

// Defaults returns the built-in configuration.
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
	// Placeholder for the planned standalone public repository; see README.md.
	c.Buttons.YozoraURL = "https://github.com/mariyaholic/yozora"
	c.Sources.PollMS = 1000
	c.Sources.Order = []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"}
	c.Sources.Blocked = []string{"browser", "generic"}
	c.Spotify.Mode = "system"
	c.Spotify.PollSecs = 12
	c.Spotify.Market = "from_token"
	c.Art.Prefer = "cdn"
	c.Art.CacheMB = 50
	c.Log.Level = "info"
	return c
}

// Paths returns (config path, data dir, cache dir).
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

// Load reads the config, falling back to defaults for missing values.
func Load(path string) (*Config, error) {
	c := Defaults()
	if b, err := os.ReadFile(path); err == nil {
		// Existing files predate the switches: absence of blocked meant all enabled.
		c.Sources.Blocked = nil
		if err := toml.Unmarshal(b, c); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	c.normalize()
	return c, nil
}

// Save writes the config to path (creating parent dirs).
func Save(c *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cfg-*.toml")
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (c *Config) normalize() {
	if c.Discord.ClientID == "" {
		c.Discord.ClientID = defaultDiscordClientID
	}
	if c.Discord.AppName == "Uika Resonance" {
		c.Discord.AppName = "Yozora"
	}
	// A five-second gap remains below five presence updates per 20 seconds.
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
		c.Art.CacheMB = 50
	}
	if c.Spotify.PollSecs < 5 {
		c.Spotify.PollSecs = 12
	}
	if len(c.Sources.Order) == 0 {
		c.Sources.Order = []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"}
	}
}

// Holder provides hot-reloadable access to the current config.
type Holder struct {
	path string
	val  atomic.Pointer[Config]
}

func NewHolder(path string) (*Holder, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	h := &Holder{path: path}
	h.val.Store(c)
	return h, nil
}

func (h *Holder) Get() *Config { return h.val.Load() }
func (h *Holder) Path() string { return h.path }

// Reload re-reads the config; returns true when it changed.
func (h *Holder) Reload() (bool, error) {
	c, err := Load(h.path)
	if err != nil {
		return false, err
	}
	old := h.val.Load()
	changed := !reflect.DeepEqual(old, c)
	if changed {
		h.val.Store(c)
	}
	return changed, nil
}

// Watch polls the file's mtime every 2s and reloads on change.
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
