//go:build windows

package cli

import (
	"path/filepath"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
)

func newMenuTray(t *testing.T) (*daemonTray, *config.Holder) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	c := config.Defaults()
	c.Sources.Blocked = []string{"twitch"}
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	h, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	return &daemonTray{cfg: h, eng: presence.New(h, nil, nil, nil, "")}, h
}

func TestTrayMenuStateFollowsSavedSwitchesAndPause(t *testing.T) {
	d, _ := newMenuTray(t)
	st := d.MenuState()
	if !st.Sources["youtube"] || st.Sources["twitch"] || !st.Sources["youtube_tabs"] || st.Paused {
		t.Fatalf("state = %+v", st)
	}
	d.PauseFor(time.Hour)
	if !d.MenuState().Paused {
		t.Fatal("pause not reflected")
	}
	d.Resume()
	d.PauseUntilResumed()
	if !d.MenuState().Paused {
		t.Fatal("open pause not reflected")
	}
	d.Resume()
	if d.MenuState().Paused {
		t.Fatal("resume not reflected")
	}
}

func TestTraySwitchSavesThroughConfig(t *testing.T) {
	d, h := newMenuTray(t)
	d.SetSource("twitch", true)
	d.SetSource("youtube_tabs", false)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := d.MenuState()
		if s.Sources["twitch"] && !s.Sources["youtube_tabs"] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("switches never saved: %+v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	loaded, err := config.Load(h.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources.Blocked) != 1 || loaded.Sources.Blocked[0] != "youtube_tabs" {
		t.Fatalf("saved blocked = %v", loaded.Sources.Blocked)
	}
}
