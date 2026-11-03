//go:build windows

package server

import (
	"regexp"
	"strings"
	"testing"
)

func TestFontsServeEmbeddedFilesOnly(t *testing.T) {
	h, _ := fixtureHandler(t)
	w := request(h, "GET", "/fonts/inter.woff2", "", false)
	if w.Code != 200 || w.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(w.Body.String(), "wOF2") {
		t.Fatalf("inter: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", w.Header())
	}
	for _, path := range []string{"/fonts/missing.woff2", "/fonts/inter.OFL.txt", "/fonts/", "/fonts/x/inter.woff2"} {
		if w := request(h, "GET", path, "", false); w.Code != 404 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	// The mux cleans dot segments with a redirect out of /fonts/, where the token check applies.
	if w := request(h, "GET", "/fonts/../ui.html", "", false); w.Code == 200 || w.Header().Get("Content-Type") == "font/woff2" {
		t.Errorf("dot segments escaped /fonts/: %d", w.Code)
	}
	if w := request(h, "POST", "/fonts/inter.woff2", "", false); w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
}

// Every @font-face in the panel must point at a file that ships, and the policy must allow it.
func TestDashboardFontFacesResolve(t *testing.T) {
	h, _ := fixtureHandler(t)
	w := request(h, "GET", "/?t=fixture-token", "", false)
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "font-src 'self'") {
		t.Fatal("CSP blocks the embedded fonts")
	}
	urls := regexp.MustCompile(`url\("(/fonts/[^"]+)"\)`).FindAllStringSubmatch(w.Body.String(), -1)
	if len(urls) < 8 {
		t.Fatalf("only %d font faces", len(urls))
	}
	for _, m := range urls {
		if r := request(h, "GET", m[1], "", false); r.Code != 200 {
			t.Errorf("%s: %d", m[1], r.Code)
		}
	}
}
