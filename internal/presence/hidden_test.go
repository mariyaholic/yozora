//go:build windows

package presence

import (
	"path/filepath"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
)

func hiddenFixture(t *testing.T, rules []player.HiddenRule) (*Engine, *recordingActivityClient, func([]player.HiddenRule)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.toml")
	cfg := config.Defaults()
	cfg.Sources.Hidden = rules
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingActivityClient{}
	set := func(next []player.HiddenRule) {
		cfg.Sources.Hidden = next
		if err := config.Save(cfg, path); err != nil {
			t.Fatal(err)
		}
		if _, err := holder.Reload(); err != nil {
			t.Fatal(err)
		}
	}
	return &Engine{Cfg: holder, ipc: client}, client, set
}

func TestHiddenTrackIsNeverSent(t *testing.T) {
	now := time.Now()
	track := player.Track{Source: "spotify", Title: "Night Walk", Artist: "Mira", Playing: true, LastUpdated: now}
	engine, client, _ := hiddenFixture(t, []player.HiddenRule{{Artist: " mira "}})
	engine.lastSMTCSessions = []player.Track{track}
	for i := 0; i < 40; i++ {
		engine.step(now.Add(time.Duration(i) * time.Second))
	}
	if len(client.activities) != 0 {
		t.Fatalf("hidden track produced %d frames", len(client.activities))
	}
	s := engine.Status()
	if !s.Hidden || s.Title != "Night Walk" {
		t.Fatalf("panel should still see the hidden track: %+v", s)
	}
	if got, ok := engine.CurrentTrack(); !ok || got.Title != "Night Walk" {
		t.Fatalf("CurrentTrack = %+v, %v", got, ok)
	}
	// send itself refuses too, whatever path reaches it.
	engine.send(&track, now.Add(time.Minute))
	if len(client.activities) != 0 {
		t.Fatal("send published a hidden track")
	}
}

func TestShownCardIsClearedWhenItsTrackBecomesHidden(t *testing.T) {
	now := time.Now()
	track := player.Track{Source: "spotify", Title: "Night Walk", Artist: "Mira", Playing: true, LastUpdated: now}
	engine, client, set := hiddenFixture(t, nil)
	engine.lastSMTCSessions = []player.Track{track}
	engine.step(now)
	engine.step(now.Add(time.Second))
	if len(client.activities) != 1 || client.activities[0] == nil {
		t.Fatal("fixture not published")
	}

	set([]player.HiddenRule{{Title: "night walk"}})
	engine.step(now.Add(2 * time.Second))
	if len(client.activities) != 1 {
		t.Fatal("cleared before the rate gate")
	}
	clearedAt := now.Add(6 * time.Second)
	engine.step(clearedAt)
	if len(client.activities) != 2 || client.activities[1] != nil || engine.Status().Published != nil {
		t.Fatalf("hidden track's card was not cleared: %d frames", len(client.activities))
	}
	for i := 1; i <= 20; i++ {
		engine.step(clearedAt.Add(time.Duration(i) * time.Second))
	}
	if len(client.activities) != 2 {
		t.Fatalf("hidden track sent %d extra frames", len(client.activities)-2)
	}

	set(nil)
	engine.step(now.Add(40 * time.Second))
	engine.step(now.Add(41 * time.Second))
	if len(client.activities) != 3 || client.activities[2] == nil {
		t.Fatal("removing the rule did not bring the card back")
	}
}

func TestHiddenReplacementClearsThePreviousCard(t *testing.T) {
	now := time.Now()
	first := player.Track{Source: "spotify", Title: "Fine", Artist: "Okay", Playing: true, LastUpdated: now}
	engine, client, _ := hiddenFixture(t, []player.HiddenRule{{Artist: "Secret *"}})
	engine.lastSMTCSessions = []player.Track{first}
	engine.step(now)
	engine.step(now.Add(time.Second))
	if len(client.activities) != 1 {
		t.Fatal("fixture not published")
	}
	engine.lastSMTCSessions = []player.Track{{Source: "spotify", Title: "Hush", Artist: "Secret Band", Playing: true, LastUpdated: now}}
	for i := 2; i < 20; i++ {
		engine.step(now.Add(time.Duration(i) * time.Second))
	}
	if len(client.activities) != 2 || client.activities[1] != nil {
		t.Fatalf("expected one card then one clear, got %d frames", len(client.activities))
	}
}
