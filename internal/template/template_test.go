//go:build windows

package template

import (
	"strings"
	"testing"
	"time"

	"uika-resonance/internal/player"
)

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("no-trunc: %q", got)
	}
	got := Truncate(strings.Repeat("a", 200), MaxTextField)
	if len([]rune(got)) != MaxTextField || !strings.HasSuffix(got, "…") {
		t.Fatalf("trunc: len=%d suffix=%q", len([]rune(got)), got[len(got)-3:])
	}
	if got := Truncate("日本語テスト日本語テスト", 6); got != "日本語テス…" {
		t.Fatalf("cjk trunc: %q", got)
	}
}

func TestBar(t *testing.T) {
	d := Data{Elapsed: 50, Duration: 100}
	if got := d.Bar(10, "▰", "▱"); got != "▰▰▰▰▰▱▱▱▱▱" {
		t.Fatalf("bar: %q", got)
	}
	d.Elapsed = 100
	if got := d.Bar(10, "▰", "▱"); got != "▰▰▰▰▰▰▰▰▰▰" {
		t.Fatalf("bar full: %q", got)
	}
	zero := Data{}
	if got := zero.Bar(4, "x", "y"); got != "yyyy" {
		t.Fatalf("bar zero dur: %q", got)
	}
}

func TestRender(t *testing.T) {
	e := New("{{.Title}}", "{{.Artist}} — {{.Album}}", "{{.Album}}", "{{.Player}}", "⏸ {{.Title}}")
	tr := player.Track{
		Title: "Way Back Into Love", Artist: "Hugh Grant",
		Album: "Music And Lyrics", Player: "Spotify", Playing: true,
		PositionSec: 60, DurationSec: 278, LastUpdated: time.Now(),
	}
	details, state, large, _ := e.Render(tr, time.Now())
	if details != "Way Back Into Love" {
		t.Fatalf("details: %q", details)
	}
	if state != "Hugh Grant — Music And Lyrics" {
		t.Fatalf("state: %q", state)
	}
	if large != "Music And Lyrics" {
		t.Fatalf("large: %q", large)
	}
	tr.Playing = false
	details, _, _, _ = e.Render(tr, time.Now())
	if details != "⏸ Way Back Into Love" {
		t.Fatalf("paused: %q", details)
	}
}

func TestRenderBadTemplate(t *testing.T) {
	e := New("{{.Nope", "", "", "", "")
	tr := player.Track{Title: "x", Playing: true}
	details, _, _, _ := e.Render(tr, time.Now())
	if details == "" {
		t.Fatalf("bad template should fall back to raw text")
	}
}

func TestLongFieldsTruncated(t *testing.T) {
	long := strings.Repeat("あ", 300)
	e := New("{{.Title}}", "", "", "", "")
	tr := player.Track{Title: long, Playing: true}
	details, _, _, _ := e.Render(tr, time.Now())
	if len([]rune(details)) > MaxTextField {
		t.Fatalf("details overflow: %d", len([]rune(details)))
	}
}
