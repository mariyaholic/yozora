//go:build windows

package server

// canonicalSources is the switch/hierarchy vocabulary shared by the API,
// validation, and UI. Declared once here so Defaults() and the server never
// drift apart.
var canonicalSources = []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"}
