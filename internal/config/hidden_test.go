//go:build windows

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"uika-resonance/internal/player"
)

func TestHiddenRulesRoundTripAndAreTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	raw := "[[sources.hidden]]\nartist = \"  Mira \"\n\n[[sources.hidden]]\ntitle = \"Night *\"\n\n[[sources.hidden]]\nartist = \"*\"\n\n[[sources.hidden]]\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []player.HiddenRule{{Artist: "Mira"}, {Title: "Night *"}}
	if !reflect.DeepEqual(cfg.Sources.Hidden, want) {
		t.Fatalf("Hidden = %+v, want %+v (empty and wildcard-only rules dropped)", cfg.Sources.Hidden, want)
	}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil || !reflect.DeepEqual(again.Sources.Hidden, want) {
		t.Fatalf("round trip = %+v, %v", again.Sources.Hidden, err)
	}
	if Defaults().Sources.Hidden != nil {
		t.Fatal("default hides something")
	}
}

func TestValidateHidden(t *testing.T) {
	long := strings.Repeat("a", MaxHiddenField+1)
	many := make([]player.HiddenRule, MaxHiddenRules+1)
	for i := range many {
		many[i] = player.HiddenRule{Title: "t"}
	}
	for name, rules := range map[string][]player.HiddenRule{
		"empty rule":      {{}},
		"blank rule":      {{Artist: "  ", Title: " "}},
		"only wildcard":   {{Artist: "*"}},
		"long artist":     {{Artist: long}},
		"long title":      {{Title: long}},
		"control char":    {{Title: "a\nb"}},
		"too many":        many,
		"bad after valid": {{Artist: "ok"}, {}},
	} {
		if ValidateHidden(rules) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for name, rules := range map[string][]player.HiddenRule{
		"none":     nil,
		"artist":   {{Artist: "Mira"}},
		"both":     {{Artist: "Mira", Title: "Night *"}},
		"japanese": {{Title: "夜に駆ける"}},
		"limit":    many[:MaxHiddenRules],
	} {
		if err := ValidateHidden(rules); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
}

func TestCardEnums(t *testing.T) {
	for _, ok := range []string{"show", "hide", "clear"} {
		if !ValidPausedMode(ok) {
			t.Errorf("paused mode %q rejected", ok)
		}
	}
	if ValidPausedMode("") || ValidPausedMode("Show") || ValidPausedMode("pause") {
		t.Error("bad paused mode accepted")
	}
	if !ValidActivityType("listening") || !ValidActivityType("watching") || !ValidActivityType("playing") || ValidActivityType("competing") {
		t.Error("activity types wrong")
	}
	if !ValidStatusDisplay("app") || !ValidStatusDisplay("name") || !ValidStatusDisplay("state") || !ValidStatusDisplay("details") || ValidStatusDisplay("title") {
		t.Error("status displays wrong")
	}
}
