package smtc

import (
	"path"
	"strings"
	"unicode"
)

// Canonical sources an SMTC session can classify into. Every player that is
// not one of the dedicated music sources stays in "generic" (or "browser")
// and differs only by its display name.
const (
	SourceAppleMusic   = "applemusic"
	SourceSpotify      = "spotify"
	SourceYouTubeMusic = "youtubemusic"
	SourceBrowser      = "browser"
	SourceGeneric      = "generic"
)

// knownPlayer maps SMTC source app IDs to a canonical source and a name.
//
// ids are compared case-insensitively in three forms:
//   - an exe name ("foobar2000.exe") matches the base name of the app ID;
//   - a packaged app's family ("SpotifyAB.SpotifyMusic_zpdnekdrzrea0")
//     matches any app ID inside that package, whatever follows the "!";
//   - any other ID matches the whole app ID, or the app ID followed by a
//     "." or "!" suffix (Chromium appends ".<profile>" or "._crx_<app>").
//
// contains is reserved for long, unique tokens such as Chromium extension
// IDs, which sit in the middle of a browser's AUMID.
//
// IDs come from the music-presence/media-players catalogue, package
// family names published by the Microsoft Store, and the AUMIDs that
// Chromium and Firefox register on Windows.
type knownPlayer struct {
	source   string
	name     string
	ids      []string
	contains []string
}

var knownPlayers = []knownPlayer{
	// YouTube Music comes first: its PWA lives inside a browser's AUMID.
	{source: SourceYouTubeMusic, name: "YouTube Music",
		contains: []string{"_crx_cinhimbnkkaeohfgghhklpknlkffjgod"},
		ids: []string{
			"music.youtube.com-5929f88e_vezhnr0wkvrcy",
			"com.github.th-ch.youtube-music",
			"com.squirrel.youtube_music_desktop_app.youtube-music-desktop-app",
			"youtube music.exe",
			"youtube-music-desktop-app.exe",
		}},
	{source: SourceAppleMusic, name: "Apple Music", ids: []string{
		"appleinc.applemusicwin_nzyj5cx40ttqa",
		"applemusic.exe",
		// Edge's installed-site package for music.apple.com.
		"music.apple.com-a5f611c_7vh1tm7h3g5s0",
	}},
	// Apple's video app shares the publisher prefix with Apple Music, so it
	// is listed explicitly to keep it out of the Apple Music source.
	{source: SourceGeneric, name: "Apple TV", ids: []string{
		"appleinc.appletvwin_nzyj5cx40ttqa",
	}},
	// iTunes plays local libraries, so it is not treated as Apple Music.
	{source: SourceGeneric, name: "iTunes", ids: []string{
		"itunes.exe",
	}},
	{source: SourceSpotify, name: "Spotify", ids: []string{
		"spotify.exe",
		"spotifyab.spotifymusic_zpdnekdrzrea0",
	}},

	{source: SourceGeneric, name: "Tidal", ids: []string{
		"tidal.exe",
		"com.squirrel.tidal.tidal",
		// WiMP was Tidal's original name; the Store package kept it.
		"wimpmusic.27241e05630ea_kn85bz84x7te4",
	}},
	{source: SourceGeneric, name: "Deezer", ids: []string{
		"deezer.exe",
		"com.deezer.deezer-desktop",
		"deezer.62021768415af_q7m17pa7q8kj0",
	}},
	{source: SourceGeneric, name: "Qobuz", ids: []string{
		"qobuz.exe",
		"com.squirrel.qobuz.qobuz",
	}},
	{source: SourceGeneric, name: "Amazon Music", ids: []string{
		"amazon music.exe",
		"amazonmobilellc.amazonmusic_kc6t79cpj4tp0",
		"music.amazon.com-a4232aae_pwn81ww419gp8",
	}},

	{source: SourceGeneric, name: "foobar2000", ids: []string{
		"foobar2000.exe",
		"resolute.foobar2000_cg7j1awqsza28",
		"resolute.foobar2000modern_cg7j1awqsza28",
	}},
	{source: SourceGeneric, name: "MusicBee", ids: []string{
		"musicbee.exe",
		"50072stevenmayall.musicbee_kcr266et74avj",
	}},
	{source: SourceGeneric, name: "AIMP", ids: []string{
		"aimp.exe",
		// AIMP reports through a separate SMTC helper.
		"aimpsmtc.exe",
		"nimigames68.aimpsmtc",
	}},
	{source: SourceGeneric, name: "VLC", ids: []string{
		"vlc.exe",
		"videolan.vlc_paz6r1rewnh0a",
	}},
	// The new Media Player still ships in the Groove (ZuneMusic) package.
	{source: SourceGeneric, name: "Media Player", ids: []string{
		"microsoft.zunemusic_8wekyb3d8bbwe",
	}},
	{source: SourceGeneric, name: "Films & TV", ids: []string{
		"microsoft.zunevideo_8wekyb3d8bbwe",
	}},
	// Legacy Windows Media Player has no SMTC session of its own on most
	// builds. Its exe and registered AUMID are matched exactly in case a
	// plugin or shell host reports them; nothing broader is guessed.
	{source: SourceGeneric, name: "Windows Media Player", ids: []string{
		"wmplayer.exe",
		"microsoft.windows.mediaplayer32",
	}},

	// Chromium browsers report a bare AUMID ("MSEdge", "Chrome") plus a
	// ".<profile>" suffix for other profiles and "._crx_<app>" for PWAs.
	{source: SourceBrowser, name: "Microsoft Edge", ids: []string{
		"msedge", "msedgebeta", "msedgedev", "msedgecanary",
		"msedge.exe",
		"microsoft.microsoftedge_8wekyb3d8bbwe",
	}},
	{source: SourceBrowser, name: "Google Chrome", ids: []string{
		"chrome", "chromebeta", "chromedev", "chromecanary",
		"chrome.exe",
	}},
	{source: SourceBrowser, name: "Chromium", ids: []string{
		"chromium", "chromium.exe",
	}},
	{source: SourceBrowser, name: "Brave", ids: []string{
		"brave", "brave.exe",
	}},
	// Opera's AUMID varies between versions and channels; only the plain
	// names and the exe (shared by Opera and Opera GX) are matched.
	{source: SourceBrowser, name: "Opera", ids: []string{
		"opera", "operagx", "opera.exe",
	}},
	{source: SourceBrowser, name: "Vivaldi", ids: []string{
		"vivaldi", "vivaldi.exe",
	}},
	{source: SourceBrowser, name: "Arc", ids: []string{
		"thebrowsercompany.arc_ttt1ap7aakyb4",
		"arc.exe",
	}},
	// Firefox's AUMID is a hash of its install directory. The two below are
	// the default Program Files paths of release and Nightly; Firefox in any
	// other folder falls back to a generic player rather than a guess.
	{source: SourceBrowser, name: "Firefox", ids: []string{
		"firefox.exe",
		"308046b0af4a39cb",
		"6f193ccc56814779",
		"mozilla.firefox_n80bbvh6b1yt2",
	}},
}

// lookup returns the table entry for appID, or nil for an unknown app.
func lookup(appID string) *knownPlayer {
	a := strings.ToLower(strings.TrimSpace(appID))
	if a == "" {
		return nil
	}
	base := exeBase(a)
	family, _, packaged := strings.Cut(a, "!")
	for i := range knownPlayers {
		p := &knownPlayers[i]
		for _, c := range p.contains {
			if strings.Contains(a, c) {
				return p
			}
		}
		for _, id := range p.ids {
			switch {
			case strings.HasSuffix(id, ".exe"):
				if base == id {
					return p
				}
			case a == id, packaged && family == id:
				return p
			case len(a) > len(id) && strings.HasPrefix(a, id) && (a[len(id)] == '.' || a[len(id)] == '!'):
				return p
			}
		}
	}
	return nil
}

// exeBase returns the file name of an exe app ID ("c:\x\vlc.exe" -> "vlc.exe").
func exeBase(a string) string {
	if !strings.HasSuffix(a, ".exe") {
		return ""
	}
	return path.Base(strings.ReplaceAll(a, `\`, "/"))
}

// Classify maps an SMTC source app ID to one of the canonical sources.
func Classify(appID string) string {
	if p := lookup(appID); p != nil {
		return p.source
	}
	return SourceGeneric
}

// FriendlyName returns the name shown on the card for an SMTC app ID.
func FriendlyName(appID string) string {
	if p := lookup(appID); p != nil {
		return p.name
	}
	return fallbackName(appID)
}

// fallbackName derives a readable name for an app missing from the table:
// "My Player.exe" -> "My Player", "Vendor.CoolPlayer_hash!App" ->
// "CoolPlayer", "com.example.tune-box" -> "Tune Box".
func fallbackName(appID string) string {
	id := strings.TrimSpace(appID)
	var name string
	if strings.HasSuffix(strings.ToLower(id), ".exe") {
		name = path.Base(strings.ReplaceAll(id, `\`, "/"))
		name = name[:len(name)-len(".exe")]
	} else {
		family, app, _ := strings.Cut(id, "!")
		pkg, _, _ := strings.Cut(family, "_")
		name = pkg
		if i := strings.LastIndex(pkg, "."); i >= 0 {
			name = pkg[i+1:]
		}
		// Store package names often end in a publisher number
		// ("WiMPMusic.27241E05630EA"); the app part reads better then.
		if (opaque(name) || name == "") && app != "" && !strings.EqualFold(app, "app") {
			name = app
			if i := strings.LastIndex(app, "."); i >= 0 && i+1 < len(app) {
				name = app[i+1:]
			}
		}
	}
	name = tidy(name)
	if name == "" || opaque(name) {
		return "Media Player"
	}
	return name
}

// opaque reports whether s looks like a hash or number rather than a name.
func opaque(s string) bool {
	if len(s) < 6 {
		return false
	}
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'):
		default:
			return false
		}
	}
	return digits > 0
}

// tidy turns separators into spaces and capitalises all-lowercase words,
// leaving deliberate casing such as "TIDAL" or "MusicBee" alone.
func tidy(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return ' '
		}
		return r
	}, s)
	words := strings.Fields(s)
	for i, w := range words {
		if w == strings.ToLower(w) {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}
