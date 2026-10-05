// Yozora — your music, live on your Discord profile.
// Windows daemon bridging SMTC (Spotify, Apple Music, browsers) and the
// optional Spotify Web API into Discord Rich Presence.
package main

import (
	"os"

	"uika-resonance/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
