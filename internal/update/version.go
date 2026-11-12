// Package update checks GitHub for a newer Yozora release and, only when the
// user asks, downloads, verifies and launches its installer. Nothing here
// installs on its own.
package update

import (
	"strconv"
	"strings"
)

// Repo is the GitHub repository releases are read from.
const Repo = "mariyaholic/yozora"

// ParseVersion reads "0.2.N" or "v0.2.N". Anything else, including "dev",
// is not a release version.
func ParseVersion(s string) (v [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		// Digits only and short enough that Atoi cannot overflow: no signs, no spaces.
		if p == "" || len(p) > 9 || strings.Trim(p, "0123456789") != "" {
			return v, false
		}
		v[i], _ = strconv.Atoi(p)
	}
	return v, true
}

// Newer reports whether latest is a higher release than current. A build that
// is not a release (a "dev" build) is never told to update.
func Newer(current, latest string) bool {
	c, okC := ParseVersion(current)
	l, okL := ParseVersion(latest)
	if !okC || !okL {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// ReleaseURL is the release page for a version, built here rather than taken
// from the API response so the panel only ever links to this repository.
func ReleaseURL(version string) string {
	return "https://github.com/" + Repo + "/releases/tag/v" + strings.TrimPrefix(version, "v")
}
