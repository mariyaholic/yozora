//go:build windows

package config

import (
	"os"
	"path/filepath"
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
	// Explicit choices (including an explicit empty list from the dashboard) survive.
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
	// A pre-existing file without the key keeps its legacy all-enabled behaviour.
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
	// The dashboard's explicit empty choice round-trips instead of re-defaulting.
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
