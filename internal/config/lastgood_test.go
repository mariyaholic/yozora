//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadKeepLastGoodCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	c := Defaults()
	c.UI.Theme = "pine"
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	lg, err := os.ReadFile(LastGoodPath(path))
	if err != nil || string(lg) != string(saved) {
		t.Fatalf("save did not keep a last-good copy: %v", err)
	}

	// A hand edit that parses becomes the new last-good copy on load, comments and all.
	edited := "# mine\n" + string(saved)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.Restored() != nil {
		t.Fatal("a valid config was reported as restored")
	}
	if lg, _ := os.ReadFile(LastGoodPath(path)); string(lg) != edited {
		t.Fatalf("load did not refresh last-good copy:\n%s", lg)
	}

	// A broken edit while running is rejected and leaves last-good alone.
	if err := os.WriteFile(path, []byte("[ui\ntheme = "), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Reload(); err == nil {
		t.Fatal("broken config reloaded without error")
	}
	if lg, _ := os.ReadFile(LastGoodPath(path)); string(lg) != edited {
		t.Fatal("failed reload overwrote the last-good copy")
	}
}

func TestStartupRestoresLastGoodAndKeepsBrokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cadence.toml")
	c := Defaults()
	c.UI.Theme = "kurisu"
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(path)
	broken := "[ui]\ntheme = \"pine\"\npoll_ms = = 3\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := NewHolder(path)
	if err != nil {
		t.Fatalf("startup with last-good copy failed: %v", err)
	}
	if got := h.Get().UI.Theme; got != "kurisu" {
		t.Fatalf("theme = %q, want the last-good value", got)
	}
	r := h.Restored()
	if r == nil || r.Err == "" || r.At.IsZero() {
		t.Fatalf("restore not reported: %+v", r)
	}
	if !strings.HasPrefix(filepath.Base(r.BrokenPath), "cadence.toml.broken-") {
		t.Fatalf("broken path = %q", r.BrokenPath)
	}
	if b, err := os.ReadFile(r.BrokenPath); err != nil || string(b) != broken {
		t.Fatalf("broken file not kept verbatim: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(good) {
		t.Fatal("config path does not hold the restored copy")
	}
}

func TestStartupWithoutLastGoodStillFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	if err := os.WriteFile(path, []byte("not = = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHolder(path); err == nil {
		t.Fatal("broken config without a last-good copy loaded")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("broken config was moved although nothing replaced it")
	}

	// An invalid last-good copy is not used either.
	if err := os.WriteFile(LastGoodPath(path), []byte("also = = broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHolder(path); err == nil {
		t.Fatal("invalid last-good copy was used")
	}
	if b, _ := os.ReadFile(path); string(b) != "not = = toml" {
		t.Fatal("broken config was changed although nothing replaced it")
	}
}

func TestRestoreNamesBrokenFileByTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cadence.toml")
	if err := Save(Defaults(), path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	_, r, err := restoreLastGood(path, os.ErrInvalid, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := path + ".broken-20260304-050607"; r.BrokenPath != want {
		t.Fatalf("broken path = %q, want %q", r.BrokenPath, want)
	}
}
