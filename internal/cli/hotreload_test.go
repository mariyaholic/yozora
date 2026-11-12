//go:build windows

package cli

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"uika-resonance/internal/config"
)

func TestLogLevelReappliedOnReload(t *testing.T) {
	flags, out := log.Flags(), log.Writer()
	t.Cleanup(func() { log.SetFlags(flags); log.SetOutput(out) })
	oldFile := logOut
	logOut = nil
	t.Cleanup(func() { logOut = oldFile })

	path := filepath.Join(t.TempDir(), "cadence.toml")
	c := config.Defaults()
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	h, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	applyLogLevel(h.Get().Log.Level)
	log.SetOutput(io.Discard)
	watchLogLevel(h)
	if log.Flags()&log.Lshortfile != 0 {
		t.Fatal("info level must not add source locations")
	}
	c.Log.Level = "debug"
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if log.Flags()&log.Lshortfile == 0 {
		t.Fatal("debug level was not applied on reload")
	}
	c.Log.Level = "info"
	if err := config.Save(c, path); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if log.Flags()&log.Lshortfile != 0 {
		t.Fatal("info level was not restored on reload")
	}
	_ = os.Remove(path)
}

func TestArtCountryPrefersConfig(t *testing.T) {
	c := config.Defaults()
	c.Art.Country = "jp"
	if got := artCountry(c); got != "JP" {
		t.Fatalf("got %q", got)
	}
}
