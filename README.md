# Yozora

_Made with love by [Maria](https://github.com/mariyaholic)._

A Windows music detector that publishes Discord Rich Presence. This repository contains the standalone Go module; internal config and executable names remain `uika-resonance` for compatibility. Yozora competes with existing Rich Presence applications by completely culling every single negligible element that causes RAM and CPU bloat; on a good runtime it can go to as low as 10MB of RAM with barely any CPU seconds.

## Installing (no terminal needed)

**[Download Yozora-Setup.exe](https://github.com/mariyaholic/yozora/releases/latest/download/Yozora-Setup.exe)** (download this one), then open it. It installs for your user only (no admin prompt) and adds a Start menu entry. In the panel, tick "Start Yozora when I sign in to Windows" if you want it to run on its own.

Yozora is not code-signed yet, so Windows SmartScreen may say "Windows protected your PC": choose **More info**, then **Run anyway**. Checksums are in `SHA256SUMS.txt` on the [release page](https://github.com/mariyaholic/yozora/releases/latest).

Coming from v0.1.1? Its separate `yozora-daemon-...exe` and `yozora-panel-...exe` files are replaced by the installer; the old daemon file opens a console window when double-clicked, so delete it.

Advanced: a portable [Yozora.exe](https://github.com/mariyaholic/yozora/releases/latest/download/Yozora.exe) is attached to the same release.

Yozora keeps an icon in the notification area for as long as it runs, including after it starts on its own at sign-in. Closing the control panel ends the panel completely while Yozora keeps running in the tray. Use **Open control panel** from the tray (or click the icon) or launch Yozora again to reopen it; choose **Quit Yozora** to stop Yozora and close the panel if it is open.

## Repository

You are looking at it. The second Discord button is **Yozora**, linking here.

The built-in URL lives in `internal/config/config.go`, in `Defaults()` as `Buttons.YozoraURL`. Existing installations can override it in `%APPDATA%\uika-resonance\cadence.toml`:

```toml
[buttons]
  enabled = true
  label = "Listen along"
  spotify_search_fallback = true
  yozora_url = "https://github.com/mariyaholic/yozora"
```

No local drive path or enclosing repository name is used to build the URL. A folder move therefore needs no code change. If the repository ever moves, update the default and any existing local override.

## Build and run

Requires Windows and Go 1.27.1 or newer, matching `go.mod`.

```sh
go mod download
go test ./... -count=1
go build -ldflags=-H=windowsgui -o Yozora.exe ./cmd/yozora
./Yozora.exe serve
```

Commands: `serve`, `status`, `doctor`, `setup`, `install`, `uninstall`. `install` opts into Windows login startup; building and serving do not install autostart.

`Yozora.exe` with no arguments opens the control panel (below); with any argument it acts as the command line (`Yozora.exe status`, `Yozora.exe serve`, ...). It is the only executable: the control panel starts the background daemon by re-launching itself with `serve`. The daemon owns the tray icon; its **Open control panel** re-launches the executable with the internal `panel` command, which opens the panel the same way but never starts a daemon of its own. The panel window runs in its own short-lived process that exits when the window closes or when the daemon stops. The `-H=windowsgui` linker flag keeps double-click launches free of a console window; the executable's icon comes from `cmd/yozora/rsrc_windows_amd64.syso`, generated from `assets/yozora.ico` with `go-winres simply --arch amd64 --icon ../../assets/yozora.ico --manifest gui`.

## Source switches (Yozora.exe)

Double-click **Yozora.exe** to open the control panel in its own native window. The source checkboxes save on Apply. A second press while the panel is open simply brings the existing window to the front.

- New installations default to **Browser media / videos** and **Other media players** off; the music sources are on. Existing configs keep their explicit choices until you change them.
- Disabling a source stops new detections and clears an activity that source was driving at the next rate step.
- Saved through the existing `sources.blocked` list in `cadence.toml`; custom per-app filters are preserved.

YouTube has a separate toggle, off by default, and requires Browser media. Detection matches only an exact Edge or Chrome window title (the active tab), checked when the browser track or its play/pause state changes, and again if a track has not been classified yet. When enabled and a video ID is available, it shows Watching YouTube with the video thumbnail (or symbol) and a Watch on YouTube button; otherwise it shows the symbol on Discord and the SMTC thumbnail (or symbol) on the dashboard, without a video button. When disabled, a browser track matching a YouTube window title is hidden instead of shown as a browser card, even if several windows match. When YouTube is on and exactly one window matches, Yozora reads that window’s address bar once through Windows UI Automation at each of those checks to get the video ID (active tab only, best effort), and keeps only the ID.

Twitch works the same way under its own toggle, also off by default and also needing Browser media. A browser track matches when an Edge or Chrome window is titled `<channel> - Twitch` and that channel is the track's artist or title (Twitch's media session reports the stream title with the channel as artist). With Twitch on and exactly one window matching, Yozora reads that window's address bar and keeps only the channel login from a `twitch.tv/<channel>` page. The card then shows **Watching Twitch** with the stream's live preview from Twitch's CDN (`static-cdn.jtvnw.net/previews-ttv/live_user_<channel>-640x360.jpg`) and a **Watch on Twitch** button. An offline channel's preview redirects to a placeholder, so Yozora treats that as no preview. VODs, clips and directory pages have no channel login, and ambiguous matches have no address-bar read; all of these show the Twitch symbol without a button. With Twitch off, matching tracks are hidden from Discord, as with YouTube.

## Source priority

When more than one detected source is playing at once, the **priority list** decides which one appears. The panel combines source switches and priority in one always-visible list. Drag a grip with a mouse or touch to the gold insertion line; holding near the top or bottom of the window scrolls to off-screen rows. Release to reorder, or press Escape / release outside the list to cancel. Keyboard users can use the ↑/↓ buttons or focus a grip and press Up/Down. The order saves with Apply into `sources.order` in `cadence.toml`, together with the source switches.

- The list is a total order over the six canonical sources (`applemusic`, `spotify`, `spotifyapi`, `youtubemusic`, `browser`, `generic`); the top entry wins whenever it is playing.
- If the top source stops playing or disappears, the next white-listed source takes over automatically.
- API order validation is strict: every source, each exactly once, canonical spelling; unknown or duplicate file entries are dropped on load.

For this local Rich Presence path, desktop Discord must be running; no Discord client secret is required.

## Card layout

- Header: **Listening to Spotify**, **Listening to Apple Music**, or the known player name. Unknown browser sessions are shown as Browser rather than guessed to be a specific streaming service.
- Large image: album artwork, resolved asynchronously. A slow artwork service must not delay the song text or playback timestamps.
- Small image: Yozora's application icon. Override `discord.small_image` with a Discord asset key or a publicly accessible image URL if the app icon changes.
- First button: **Listen on [service]**. A custom `buttons.label` is preserved; the legacy/default `Listen along` label becomes the service-specific label.
- Second button: **Yozora**, using `buttons.yozora_url`.

Background tab detection has its own toggle, off by default. When no window title matches, it reads the tab names of up to 8 browser windows (designed for Edge) through Windows UI Automation at the same moments as the title check, and looks for a `<title> - YouTube - Audio playing` tab; any match, even several, counts as YouTube. A newly detected background video has the YouTube symbol on Discord and keeps any media-session thumbnail on the dashboard. A tab name alone does not provide a verified video URL, so it cannot supply a video-specific Discord thumbnail or Watch button. With YouTube off, matching tracks are hidden.

If Yozora already verified a video's URL while its tab was active, the same continuously selected media item keeps its **Watching YouTube** identity, thumbnail and Watch button when Edge switches to another tab, including across pause/resume. The existing pause-hide and source-blocking policies still apply. Yozora never reads the unrelated foreground tab's address bar to identify a background video. A different media item, browser application or observed selection gap drops the retained evidence; a new foreground match is checked afresh. Edge must continue exposing the media session through Windows media controls. Two videos with identical browser application and media metadata cannot be distinguished by this continuity check.

Browser videos use only the thumbnail the browser itself supplies through Windows media controls, and browser titles are never matched against music catalogs. YouTube pages usually expose no public thumbnail URL to this API, and Discord's servers cannot fetch the local dashboard's `127.0.0.1` art cache — so a browser card shows whatever artwork is available in the local dashboard, while its Discord presence may appear without a large image (text, timestamps, and the Yozora button still show).

The Windows detector reads once at startup, then polls every second by default. Its one-slot queue retains the newest snapshot, not a stale queued one. The presence engine checks every 250 ms; the brief debounce suppresses rapid skip bursts. Discord sends remain spaced by at least five seconds, so fast detection is not a promise of an immediate Discord-visible update. Artwork can arrive as a later enriched update.

## Appearance

The **Appearance** button in the panel's header opens a dialog with 11 themes and the motion setting. Each theme tile previews its colours and title face. Choosing one applies it at once and saves it in `cadence.toml`:

```toml
[ui]
  theme = "yozora"
  motion = "system"
```

| Theme id | Name | Mode | Palette source |
| --- | --- | --- | --- |
| `yozora` (default) | Night sky | Dark | Yozora |
| `uika` | Uika | Dark | Omoi / Uika canon |
| `pine` | Pine | Dark | Argonaut Oak |
| `kurisu` | Kurisu | Dark | Argonaut Oak |
| `ava` | Ava | Dark | Argonaut Oak |
| `astra` | Astra | Dark | Ad Astra (Console) |
| `mariyaholic-98` | Mariyaholic-98 | Dark | Omoi |
| `sumimi` | Sumimi | Light | Omoi / Uika canon |
| `ledger` | Ledger | Light | Argonaut Oak (Paper) |
| `confluence` | Confluence | Light | Omoi |
| `astra-light` | Astra light | Light | Ad Astra (Paper) |

All 11 appear in one grid, dark themes first. Themes that looked nearly identical were merged: Omoi Panel into Night sky, and Residual Night and Paper into Astra and Astra light. Saved ids `panel`, `residual` and `residual-light` load as the theme that absorbed them. Where a source called a theme "Paper", "Night" or "Console", it is renamed so each name is unique. Omoi palettes come from `prototype/tokens.css` and `prototype/inspiration.css`.

Colours are copied exactly from each source. Where a source colour would fall below 4.5:1 as text in this panel, a lighter or darker value is used instead and marked `derived` in `internal/server/ui.html`. This happens for a few error and hover colours. The browser check measures text, muted, link, error and button contrast in every theme. Each theme's faces ship inside the binary (Latin subsets, about 370 KB, all under the SIL Open Font License; see `internal/server/fonts/README.md`), and the panel serves them itself at `/fonts/`. The content-security policy allows fonts only from the panel, so nothing is fetched from the network. Night sky uses Segoe UI and Confluence uses Georgia and Consolas, which Windows already has. The browser check confirms that every other theme loads its own faces. An unknown or missing `theme` value loads as `yozora`.

### Motion

Motion only answers an event: a hover or press, a drag, a reorder, a new track or play state, or a save in progress. Nothing animates while the panel sits idle. The one repeating animation is the playback dot in Pine, Kurisu and Ava, which pulses only while Discord is connected and a track is playing. The **Motion** setting under Appearance chooses how much of this runs:

- **Follow Windows** (`system`, the default) uses Windows' "Show animations" setting (`prefers-reduced-motion`).
- **Reduced** (`reduced`) turns every animation and transition off, whatever Windows says.
- **Full** (`full`) keeps the panel's motion on even when Windows asks for reduced motion.

Like the theme, it applies at once and is saved in `cadence.toml`; an unknown value loads as `system`.

| Theme | On events | Buttons | Drag and reorder |
| --- | --- | --- | --- |
| Night sky | New track: artwork fades in, the crescent's star twinkles | Lift 1px, gold border | Rows part around the drop point; glowing drop line; rows slide 200ms |
| Uika | Wine glow fades in once when the theme loads; new track: artwork fades in | Gold underline sweeps in | Lifted row glows gold; rows glide 300ms on the canon ease-out |
| Pine, Kurisu, Ava | Live playback dot. Kurisu: new text warms up. Ava: readouts flash on track or play-state change | 0.15s colour, 1px accent outline on press | 2px accent edge on the dragged row; rows snap. Kurisu: amber afterglow. Ava: the drop line sweeps |
| Ledger | None | Ink colour | A double rule opens at the drop point; rows snap |
| Astra, Astra light | None (colour changes only, 120ms) | Colour only | Dashed placeholder, ruler-tick drop line; rows snap |
| Mariyaholic-98 | A block cursor blinks while a save is in flight | Keys sink into their hard shadow | Hard-shadow preview on a 4px grid; rows move in four frames |
| Sumimi | New track: the card fades in | A gold wash spreads from the press point | Paper-lift preview; rows ease 300ms |
| Confluence | None | Oxblood underline | Oxblood drop rule; rows slide 200ms |

## Verification

Private Windows named-pipe regression tests exercise simultaneous read/write using a unique `yozora-test-*` pipe, not the live Discord client. Ordinary `go test ./...` never publishes experimental activity to a Discord profile. Tests also cover slow artwork, session gaps and out-of-order lookups, coherent track/art transitions, paused clear/resume and visibility-policy reloads under the send-rate gate, label-only Discord button readback, loopback-art filtering, source-toggle API validation and concurrency, launcher endpoint validation, service names, icon placement, platform-specific links, button ordering, display enums, config bounds, and newest-snapshot retention.

The embedded control panel also has a browser regression check using synthetic media and settings. It exercises source toggles, every theme (applied, saved, rolled back on failure, and checked for 4.5:1 text), motion (no animation at idle in any theme, cues fire once per change and never on reconnect, every drag, cancel and reorder ends with no leftover transform or running animation, busy cursors clear after failed saves, Windows reduced motion and the Reduced mode each disable everything, and Full overrides Windows), mouse/touch/keyboard priority ordering (including cancellation and edge scrolling), save failures, the Appearance dialog, that each theme loads its own fonts, the Twitch card, Discord-offline and panel-offline notes, responsive layouts, and the production content-security policy without running the daemon or contacting Discord. On Linux with Node.js:

```sh
npm install --prefix /tmp/yozora-ui-tools --no-save --no-package-lock playwright@1.62.1
/tmp/yozora-ui-tools/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/yozora-ui-tools/node_modules node scripts/check-ui.cjs
```

To use an existing Chromium installation, set `CHROMIUM_PATH` to its executable instead of installing Playwright's browser. Screenshots and `results.json` are written to the system temporary directory under `yozora-ui-check`; set `UI_EVIDENCE_DIR` to choose another location. These browser checks complement the Windows Go tests; they do not exercise native WebView2 or media detection.

## Release

Every merge to `main` tests, builds and publishes a release automatically (`.github/workflows/release.yml`). The version is the latest `v0.1.N` tag plus one, so the download links above always point at the newest build. Use the "Run workflow" button on the release workflow to republish without a code change.

## Notes

Yozora is not affiliated with or endorsed by Discord, Spotify, or Apple.

YouTube Music is a dedicated source, enabled by default, for its browser PWA and supported desktop apps. It always uses Listening activity, shows the Windows media thumbnail in the dashboard, and uses a fixed YouTube Music symbol on Discord without catalog lookups or a listen button. Custom app filters such as `msedge` still block the YouTube Music PWA; its source toggle cannot override them.

## License

MIT, see [LICENSE](LICENSE).
