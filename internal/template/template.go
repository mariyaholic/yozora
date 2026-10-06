//go:build windows

package template

import (
	"fmt"
	"strings"
	"text/template"
	"time"

	"uika-resonance/internal/player"
)

const (
	MaxTextField   = 128
	MaxButtonLabel = 32
)

type Data struct {
	Title       string
	Artist      string
	Album       string
	Player      string
	Source      string
	Elapsed     float64
	Duration    float64
	Playing     bool
	TrackNumber int
}

func (d Data) ElapsedFmt() string { return fmtTime(d.Elapsed) }
func (d Data) DurationFmt() string {
	return fmtTime(d.Duration)
}
func (d Data) RemainingFmt() string {
	r := d.Duration - d.Elapsed
	if r < 0 {
		r = 0
	}
	return fmtTime(r)
}

func fmtTime(s float64) string {
	total := int(s + 0.5)
	if total < 0 {
		total = 0
	}
	m, sec := total/60, total%60
	if m >= 60 {
		h, m2 := m/60, m%60
		return fmt.Sprintf("%d:%02d:%02d", h, m2, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

func (d Data) Bar(n int, fill, empty string) string {
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	if d.Duration <= 0 {
		return strings.Repeat(empty, n)
	}
	p := d.Elapsed / d.Duration
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	f := int(p*float64(n) + 0.5)
	if f > n {
		f = n
	}
	return strings.Repeat(fill, f) + strings.Repeat(empty, n-f)
}

func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

type Engine struct {
	details string
	state   string
	large   string
	small   string
	paused  string
}

func New(details, state, large, small, paused string) *Engine {
	if details == "" {
		details = "{{.Title}}"
	}
	if state == "" {
		state = "{{.Artist}}"
	}
	return &Engine{details: details, state: state, large: large, small: small, paused: paused}
}

func render(tpl string, d Data) string {
	if tpl == "" {
		return ""
	}
	t, err := template.New("p").Funcs(template.FuncMap{
		"bar":   d.Bar,
		"t":     fmtTime,
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"strip": func(s string) string { return strings.TrimSpace(s) },
		"default": func(def, v string) string {
			if strings.TrimSpace(v) == "" {
				return def
			}
			return v
		},
	}).Parse(tpl)
	if err != nil {
		return Truncate(tpl, MaxTextField)
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return ""
	}
	return b.String()
}

func (e *Engine) Render(t player.Track, now time.Time) (details, state, large, small string) {
	d := Data{
		Title:       t.Title,
		Artist:      t.Artist,
		Album:       t.Album,
		Player:      t.Player,
		Source:      t.Source,
		Elapsed:     t.ElapsedSec(now),
		Duration:    t.DurationSec,
		Playing:     t.Playing,
		TrackNumber: int(t.TrackNumber),
	}
	renderDetails := e.details
	if !t.Playing && e.paused != "" {
		renderDetails = e.paused
	}
	details = Truncate(strings.TrimSpace(render(renderDetails, d)), MaxTextField)
	state = Truncate(strings.TrimSpace(render(e.state, d)), MaxTextField)
	large = Truncate(strings.TrimSpace(render(e.large, d)), MaxTextField)
	small = Truncate(strings.TrimSpace(render(e.small, d)), MaxTextField)
	return
}
