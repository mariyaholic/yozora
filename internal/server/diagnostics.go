//go:build windows

package server

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	"uika-resonance/internal/presence"
)

// diagInput is everything the report may draw on. It has no track fields on purpose: the
// report is pasted into public issues, so titles, tokens, client IDs and user paths stay out.
type diagInput struct {
	Version, OSBuild string
	Status           presence.Status
	Cfg              *config.Config
}

var (
	userPath  = regexp.MustCompile(`(?i)([a-z]:)?[\\/]+users[\\/]+[^\\/"']+(?:[\\/][^\s"']*)?`)
	urlQuery  = regexp.MustCompile(`\?[^\s"']*`)
	longToken = regexp.MustCompile(`[A-Za-z0-9_\-.~+/=]{32,}`)
	bigNumber = regexp.MustCompile(`\b\d{15,}\b`)
	spaces    = regexp.MustCompile(`\s+`)
)

// redactError makes an error line safe to share: it drops user folders, URL queries, long
// token-like strings, Discord-sized numbers and the playing track's own text.
func redactError(msg string, secrets ...string) string {
	msg = userPath.ReplaceAllString(msg, `%USERPROFILE%`)
	if name := os.Getenv("USERNAME"); len(name) >= 3 {
		msg = strings.ReplaceAll(msg, name, "<user>")
	}
	for _, s := range secrets {
		if s = strings.TrimSpace(s); len([]rune(s)) >= 2 {
			msg = replaceFold(msg, s, "<track>")
		}
	}
	msg = urlQuery.ReplaceAllString(msg, "?...")
	msg = longToken.ReplaceAllString(msg, "<redacted>")
	msg = bigNumber.ReplaceAllString(msg, "<id>")
	msg = spaces.ReplaceAllString(strings.TrimSpace(msg), " ")
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "..."
	}
	return msg
}

func replaceFold(s, old, repl string) string {
	return regexp.MustCompile(`(?i)`+regexp.QuoteMeta(old)).ReplaceAllLiteralString(s, repl)
}

func buildDiagnostics(in diagInput) string {
	st := in.Status
	discord := "not running"
	if st.Connected {
		discord = "connected"
	}
	lastErr := "none"
	if st.LastErr != "" {
		lastErr = redactError(st.LastErr, st.Title, st.Artist, st.Album)
	}
	var enabled []string
	for _, key := range in.Cfg.Sources.Order {
		if !player.IsBlocked(player.Track{Source: key}, in.Cfg.Sources.Blocked) {
			enabled = append(enabled, key)
		}
	}
	for _, key := range config.ToggleKeys {
		if !contains(in.Cfg.Sources.Order, key) && !player.IsBlocked(player.Track{Source: key}, in.Cfg.Sources.Blocked) {
			enabled = append(enabled, key)
		}
	}
	or := func(s string) string {
		if s == "" {
			return "unknown"
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Yozora diagnostics\n")
	fmt.Fprintf(&b, "Version: %s\n", or(in.Version))
	fmt.Fprintf(&b, "Windows: %s\n", or(in.OSBuild))
	fmt.Fprintf(&b, "Source: %s\n", or(st.Source))
	fmt.Fprintf(&b, "Artwork route: %s\n", or(st.Via))
	fmt.Fprintf(&b, "Discord: %s\n", discord)
	fmt.Fprintf(&b, "Last error: %s\n", lastErr)
	fmt.Fprintf(&b, "Enabled sources: %s\n", or(strings.Join(enabled, ", ")))
	fmt.Fprintf(&b, "Theme: %s\n", or(in.Cfg.UI.Theme))
	return b.String()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// windowsBuild reads the true OS version; GetVersionEx lies to apps without a manifest.
func windowsBuild() string {
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if proc.Find() != nil {
		return ""
	}
	var v struct {
		size, major, minor, build, platform uint32
		csd                                 [128]uint16
	}
	v.size = uint32(unsafe.Sizeof(v))
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.build)
}

var osBuild = windowsBuild
