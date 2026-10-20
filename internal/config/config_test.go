//go:build windows

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDefaultsUseYozoraAsDiscordAppName(t *testing.T) {
	cfg := Defaults()
	if cfg.Discord.AppName != "Yozora" {
		t.Fatalf("Discord.AppName = %q, want %q", cfg.Discord.AppName, "Yozora")
	}
}

func TestDefaultsUseBundledDiscordClientID(t *testing.T) {
	cfg := Defaults()
	if cfg.Discord.ClientID != "1556599139436077056" {
		t.Fatalf("Discord.ClientID = %q, want bundled Yozora app ID", cfg.Discord.ClientID)
	}
}

func TestLoadMigratesEmptyDiscordClientIDToBundledDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	data := []byte("[discord]\nclient_id = \"\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Discord.ClientID != "1556599139436077056" {
		t.Fatalf("Discord.ClientID = %q, want bundled Yozora app ID", cfg.Discord.ClientID)
	}
}

func TestLoadPreservesCustomDiscordClientID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	data := []byte("[discord]\nclient_id = \"987654321098765432\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Discord.ClientID != "987654321098765432" {
		t.Fatalf("Discord.ClientID = %q, want custom ID", cfg.Discord.ClientID)
	}
}

func TestLoadMigratesLegacyDiscordAppName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	data := []byte("[discord]\napp_name = \"Uika Resonance\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Discord.AppName != "Yozora" {
		t.Fatalf("Discord.AppName = %q, want %q", cfg.Discord.AppName, "Yozora")
	}
}

func TestLoadPreservesCustomDiscordAppName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	data := []byte("[discord]\napp_name = \"My Music App\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Discord.AppName != "My Music App" {
		t.Fatalf("Discord.AppName = %q, want %q", cfg.Discord.AppName, "My Music App")
	}
}

func TestDefaultsUseResponsiveCadence(t *testing.T) {
	cfg := Defaults()
	if cfg.Sources.PollMS != 1000 || cfg.Presence.TrackDebounceMS != 500 || cfg.Presence.MinUpdateSecs != 5 {
		t.Fatalf("cadence = poll %d ms, debounce %d ms, update %d s; want 1000/500/5", cfg.Sources.PollMS, cfg.Presence.TrackDebounceMS, cfg.Presence.MinUpdateSecs)
	}
	cfg.Presence.MinUpdateSecs = 1
	cfg.Sources.PollMS = 10
	cfg.normalize()
	if cfg.Presence.MinUpdateSecs != 5 || cfg.Sources.PollMS != 500 {
		t.Fatalf("unsafe cadence not bounded: %+v / %+v", cfg.Presence, cfg.Sources)
	}
	if cfg.Discord.SmallImage == "" || cfg.Buttons.YozoraURL != "https://github.com/mariyaholic/yozora" {
		t.Fatalf("portable presentation defaults missing: %+v / %+v", cfg.Discord, cfg.Buttons)
	}
}

func TestNewConfigBlocksNonMusicButExistingChoicesSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sources.Blocked) != 2 || c.Sources.Blocked[0] != "browser" || c.Sources.Blocked[1] != "generic" {
		t.Fatalf("new blocks = %v", c.Sources.Blocked)
	}

	for _, fixture := range []string{"[sources]\nblocked = []\n", "[sources]\nblocked = [\"SPOTIFY\", \"custom.app\"]\n"} {
		if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Sources.Blocked) > 0 && c.Sources.Blocked[0] != "SPOTIFY" {
			t.Fatalf("existing choices overwritten: %v", c.Sources.Blocked)
		}
	}

	if err := os.WriteFile(path, []byte("[sources]\npoll_ms = 2000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sources.Blocked) != 0 {
		t.Fatalf("pre-existing config was re-blocked: %v", c.Sources.Blocked)
	}

	c.Sources.Blocked = []string{}
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Sources.Blocked) != 0 {
		t.Fatalf("explicit empty choice lost on save: %v", reloaded.Sources.Blocked)
	}
}

func TestReloadReportsUnchangedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	if err := Save(Defaults(), path); err != nil {
		t.Fatal(err)
	}
	holder, err := NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := holder.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("Reload() reported an unchanged config as changed")
	}
}

func TestSourceMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.toml")
	if err := os.WriteFile(path, []byte("[sources]\norder = [\"generic\", \"spotifyapi\", \"browser\", \"spotify\", \"applemusic\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"generic", "spotifyapi", "youtubemusic", "browser", "spotify", "applemusic"}
	if !reflect.DeepEqual(c.Sources.Order, want) || !reflect.DeepEqual(c.Sources.Known, ToggleKeys) || len(c.Sources.Blocked) != 0 {
		t.Fatalf("migration: %+v", c.Sources)
	}
	before := c.Sources
	c.normalize()
	c.normalize()
	if !reflect.DeepEqual(before, c.Sources) {
		t.Fatalf("not idempotent: %+v", c.Sources)
	}
	c.Sources.Blocked = nil
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil || len(c.Sources.Blocked) != 0 {
		t.Fatalf("re-enabled source blocked: %v %v", c, err)
	}
	if !reflect.DeepEqual(Defaults().Sources.Known, ToggleKeys) {
		t.Fatal("defaults missing known keys")
	}
	for _, tc := range []struct{ order, want []string }{
		{[]string{"spotify", "generic", "custom"}, []string{"spotify", "generic", "applemusic", "spotifyapi", "youtubemusic", "browser"}}, // unknown entries are dropped
		{[]string{"youtubemusic", "browser"}, []string{"youtubemusic", "browser", "applemusic", "spotify", "spotifyapi", "generic"}},
	} {
		c := Defaults()
		c.Sources.Known = nil
		c.Sources.Order = tc.order
		c.normalize()
		if !reflect.DeepEqual(c.Sources.Order, tc.want) {
			t.Fatalf("insertion without browser: %v", c.Sources.Order)
		}
	}
}

func TestSourceMigrationReviewFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, fixture  string
		order, blocked []string
	}{
		{
			name: "legacy custom order and explicit blocks",
			fixture: `order = ["generic", "spotifyapi", "browser", "spotify", "applemusic"]
blocked = ["SPOTIFY", "custom.app"]`,
			order:   []string{"generic", "spotifyapi", "youtubemusic", "browser", "spotify", "applemusic"},
			blocked: []string{"SPOTIFY", "custom.app"},
		},
		{
			name: "existing duplicates are not multiplied",
			fixture: `order = ["applemusic", "spotify", "spotifyapi", "youtubemusic", "youtubemusic", "browser", "generic"]
blocked = ["browser", "browser"]
known = ["applemusic", "spotify", "spotifyapi", "youtubemusic", "youtubemusic", "browser", "generic"]`,
			order:   []string{"applemusic", "spotify", "spotifyapi", "youtubemusic", "browser", "generic"},
			blocked: []string{"browser", "browser"},
		},
		{
			name: "unknown order entry",
			fixture: `order = ["generic", "spotifyapi", "custom", "browser", "spotify", "applemusic"]
blocked = []`,
			order:   []string{"generic", "spotifyapi", "youtubemusic", "browser", "spotify", "applemusic"},
			blocked: []string{},
		},
		{
			name: "case and space order variant",
			fixture: `order = ["generic", " Spotify ", "browser", "spotify", "applemusic", "spotifyapi"]
blocked = []`,
			order:   []string{"generic", "spotify", "youtubemusic", "browser", "applemusic", "spotifyapi"},
			blocked: []string{},
		},
		{
			name: "only unknown order entries",
			fixture: `order = ["custom", "other"]
blocked = []`,
			order:   OrderSources,
			blocked: []string{},
		},
		{
			name: "known music missing from order",
			fixture: `order = ["generic", "spotifyapi", "browser", "spotify", "applemusic"]
blocked = []
known = ["applemusic", "spotify", "spotifyapi", "youtubemusic", "browser", "generic"]`,
			order:   []string{"generic", "spotifyapi", "youtubemusic", "browser", "spotify", "applemusic"},
			blocked: []string{},
		},
		{
			name: "case variant legacy blocks",
			fixture: `order = ["applemusic", "spotify", "spotifyapi", "browser", "generic"]
blocked = ["YouTube", " Browser ", "GENERIC"]
known = []`,
			order:   []string{"applemusic", "spotify", "spotifyapi", "youtubemusic", "browser", "generic"},
			blocked: []string{"YouTube", " Browser ", "GENERIC"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.toml")
			if err := os.WriteFile(path, []byte("[sources]\n"+tc.fixture+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				seen := make(map[string]bool, len(OrderSources))
				if len(c.Sources.Order) != len(OrderSources) {
					t.Fatalf("normalization %d: order length = %d", i, len(c.Sources.Order))
				}
				for _, source := range c.Sources.Order {
					if !slices.Contains(OrderSources, source) || seen[source] {
						t.Fatalf("normalization %d: invalid order %v", i, c.Sources.Order)
					}
					seen[source] = true
				}
				if !reflect.DeepEqual(c.Sources.Order, tc.order) || !reflect.DeepEqual(c.Sources.Blocked, tc.blocked) || !reflect.DeepEqual(c.Sources.Known, ToggleKeys) {
					t.Fatalf("normalization %d: %+v", i, c.Sources)
				}
				c.normalize()
			}
		})
	}
}
