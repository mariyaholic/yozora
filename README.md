# Yozora

_Made with love by [Maria](https://github.com/mariyaholic)._

A Windows music detector that publishes Discord Rich Presence. This repository contains the standalone Go module; internal config and executable names remain `uika-resonance` for compatibility. Yozora competes with existing Rich Presence applications by completely culling every single negligible element that causes RAM and CPU bloat; on a good runtime it can go to as low as 10MB of RAM with barely any CPU seconds.

## Installing (no terminal needed)

**[Download Yozora-Setup.exe](https://github.com/mariyaholic/yozora/releases/latest/download/Yozora-Setup.exe)** (download this one), then open it. It installs for your user only (no admin prompt) and adds a Start menu entry. In the panel, tick "Start Yozora when I sign in to Windows" if you want it to run on its own.

Yozora is not code-signed yet, so Windows SmartScreen may say "Windows protected your PC": choose **More info**, then **Run anyway**. Checksums are in `SHA256SUMS.txt` on the [release page](https://github.com/mariyaholic/yozora/releases/latest).

Coming from v0.1.1? Its separate `yozora-daemon-...exe` and `yozora-panel-...exe` files are replaced by the installer; the old daemon file opens a console window when double-clicked, so delete it.

Advanced: a portable [Yozora.exe](https://github.com/mariyaholic/yozora/releases/latest/download/Yozora.exe) is attached to the same release.

Yozora keeps an icon in the notification area for as long as it runs, including after it starts on its own at sign-in. Closing the control panel ends the panel completely while Yozora keeps running in the tray. Use **Open control panel** from the tray (or click the icon) or launch Yozora again to reopen it; choose **Quit Yozora** to stop Yozora and close the panel if it is open.

Installing a newer `Yozora-Setup.exe` over an existing install upgrades it in place: setup quits the running Yozora first (the same way **Quit Yozora** does) and keeps your settings. If autostart is on and Yozora now runs from a different folder than when you turned it on, Yozora points the sign-in entry at its current location the next time it starts.

### Uninstalling

Use **Settings > Apps > Installed apps > Yozora > Uninstall**. The uninstaller quits Yozora, removes the sign-in autostart entry and asks **Also remove my settings and cached data?** (default No). Choosing Yes also deletes:

- `%APPDATA%\uika-resonance` (settings, `cadence.toml`)
- `%LOCALAPPDATA%\uika-resonance` (artwork cache, logs, the control panel's WebView2 profile, status files)
- `%TEMP%\uika-probe` (left by `doctor`)
- the `uika-resonance:spotify-refresh` and `uika-resonance:spotify-secret` entries in Windows Credential Manager

A silent uninstall (`unins000.exe /VERYSILENT`) keeps all of this unless you add `/PURGE=1`.

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

Commands: `serve`, `status`, `doctor`, `setup`, `install`, `uninstall`, `stop`, `version`. `install` opts into Windows login startup; building and serving do not install autostart. `stop` quits a running Yozora started from the same executable, as the tray's **Quit Yozora** does. `version` prints the version, which is `dev` unless the build sets it with `-ldflags "-X uika-resonance/internal/version.Version=0.1.N"` as the release workflow does.

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

## First-run checklist

Until setup is done, the panel opens with a short checklist above the preview: **Discord found and connected**, **Media detected** and **Card visible on Discord**. It reads the same once-a-second state update as the preview and adds no polling of its own. A check that still needs something says what; a passing one says nothing.

- **Send test card** publishes a card titled "Yozora test card" for 15 seconds, then Yozora clears it and the normal card returns at the next update. The engine's own loop sends it, so it never races a real update, and the card is cleared outright when time is up, so no other rule can leave it standing. Quitting Yozora closes the Discord connection, which removes it too.
- If Discord accepts the card but does not echo it back, the checklist points at Discord's own **Share my activity** setting (Settings, Activity Privacy). Yozora cannot see your profile, so this is the closest it can get to confirming.
- The checklist also offers **Start with Windows**, the same switch as the Startup section.
- Yozora's own Discord application ID is built in, so nothing needs entering. If Discord refuses the handshake (for example a custom `discord.client_id` that does not exist), the checklist shows an application ID field; it accepts 17 to 20 digits, and an empty value goes back to Yozora's app.
- When every check passes, the checklist saves `onboarded = true` under `[ui]` in `cadence.toml` and folds away for good. Its close button does the same. **Show setup checklist** under Diagnostics brings it back.

The terminal `setup` command still exists for scripted installs.

## Spotify Web API

The desktop Spotify app is detected through Windows media controls with no setup. The optional **Spotify Web API** source adds exact artwork and track links. Connect it from the panel:

1. Create an app at the [Spotify developer dashboard](https://developer.spotify.com/dashboard) with the Web API enabled, and add the **Redirect URI** the panel shows (`http://127.0.0.1:<panel port>/spotify/callback`). No client secret is needed.
2. Paste the app's client ID (32 characters, 0-9 and a-f) into the panel and press **Connect**. Your normal browser opens Spotify's sign-in page.
3. After you agree, Spotify returns to the panel's own loopback address, Yozora stores the refresh token in Windows Credential Manager and starts polling at once. No restart is needed. **Disconnect** stops polling, deletes the token and sets `spotify.mode` back to `system`.

The flow is Authorization Code with PKCE (S256). The code verifier stays in memory only and expires after 10 minutes; the `state` value is random, checked in constant time and works once, so a replayed or forged callback is refused without contacting Spotify. Starting again or pressing Cancel invalidates the earlier attempt. Tokens and codes are never logged or shown. The panel picks a free port each run unless `[server] port` is set, and Spotify checks the redirect URI against the ones registered for the app, so set a fixed port if you reconnect often; an existing connection keeps working whatever the port.

This flow is covered by tests against a local fake token endpoint; it has not yet been tried against Spotify itself.

## Card layout

- Header: **Listening to Spotify**, **Listening to Apple Music**, or the player's name. Browser sessions show the browser (Microsoft Edge, Google Chrome, Firefox and so on) rather than a guess at the streaming service playing in it.
- Players are recognised from a fixed table of Windows app IDs and exe names in `internal/smtc/players.go`, matched exactly rather than by part of the name, so the Apple TV app is not mistaken for Apple Music. Tidal, Deezer, Qobuz, Amazon Music, foobar2000, MusicBee, AIMP, VLC, iTunes, Apple TV, Media Player and Windows Media Player are named on the card but stay under **Other media players** (`generic`), with that source's switch, priority and activity type. An app missing from the table is also `generic` and gets a name from its exe or app ID (`strawberry.exe` shows as Strawberry).
- Large image: album artwork, resolved asynchronously. A slow artwork service must not delay the song text or playback timestamps.
- Small image: Yozora's application icon. Override `discord.small_image` with a Discord asset key or a publicly accessible image URL if the app icon changes.
- First button: **Listen on [service]**. A custom `buttons.label` is preserved; the legacy/default `Listen along` label becomes the service-specific label.
- Second button: **Yozora**, using `buttons.yozora_url`.

Background tab detection has its own toggle, off by default. When no window title matches, it reads the tab names of up to 8 browser windows (designed for Edge) through Windows UI Automation at the same moments as the title check, and looks for a `<title> - YouTube - Audio playing` tab; any match, even several, counts as YouTube. A newly detected background video has the YouTube symbol on Discord and keeps any media-session thumbnail on the dashboard. A tab name alone does not provide a verified video URL, so it cannot supply a video-specific Discord thumbnail or Watch button. With YouTube off, matching tracks are hidden.

If Yozora already verified a video's URL while its tab was active, the same continuously selected media item keeps its **Watching YouTube** identity, thumbnail and Watch button when Edge switches to another tab, including across pause/resume. The existing pause-hide and source-blocking policies still apply. Yozora never reads the unrelated foreground tab's address bar to identify a background video. A different media item, browser application or observed selection gap drops the retained evidence; a new foreground match is checked afresh. Edge must continue exposing the media session through Windows media controls. Two videos with identical browser application and media metadata cannot be distinguished by this continuity check.

Browser videos use only the thumbnail the browser itself supplies through Windows media controls, and browser titles are never matched against music catalogs. YouTube pages usually expose no public thumbnail URL to this API, and Discord's servers cannot fetch the local dashboard's `127.0.0.1` art cache — so a browser card shows whatever artwork is available in the local dashboard, while its Discord presence may appear without a large image (text, timestamps, and the Yozora button still show).

The Windows detector reads once at startup, then polls every second by default. Its one-slot queue retains the newest snapshot, not a stale queued one. The presence engine checks every 250 ms; the brief debounce suppresses rapid skip bursts. Discord sends remain spaced by at least five seconds, so fast detection is not a promise of an immediate Discord-visible update. Artwork can arrive as a later enriched update.

## Appearance

The **Appearance** button in the panel's header opens a dialog with 11 themes, the motion setting and the language. Each theme tile previews its colours and title face. Choosing one applies it at once and saves it in `cadence.toml`:

```toml
[ui]
  theme = "yozora"
  motion = "system"
  language = "system"
```

The panel's footer and Diagnostics show the running version, or "dev build" for a local build.

### Language

The panel and the tray menu come in English and Japanese. **Follow Windows** (`system`, the default) uses the Windows display language, **English** (`en`) and **日本語** (`ja`) fix one. The panel applies a change at once; the tray menu picks it up the next time it opens. An unknown value loads as `system`. Product and service names (Yozora, Discord, Spotify, YouTube, Twitch, Apple Music) stay in Latin script, and the Discord card is not translated: its text comes from your own `[template]` settings. The bundled theme faces cover Latin only, so Japanese text uses Windows' own Yu Gothic UI or Meiryo; Mariyaholic-98 uses MS Gothic for the closest match to its bitmap look.

### High contrast

With a Windows contrast theme on, the panel uses the system colours: focus rings, chosen themes, segments and sources, the drop line, status lines and the checklist marks are drawn in the highlight colour or with a heavier edge, so nothing depends on the theme's own colours.

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

## Discord card settings

The panel's **Discord card** section, between Sources and Spotify, edits the settings that used to be TOML-only. **Apply changes** saves them all at once or not at all, **Discard changes** goes back to what is saved, and **Reset to defaults** fills in the defaults for you to Apply. Yozora picks the change up on its next update; no restart is needed.

- Text lines: `template.details` (top line), `template.state` (second line), `template.large_text` (image tooltip), `template.small_text` (icon tooltip) and `template.paused_details` (top line while paused). Each is a Go template of at most 256 characters and must parse and run; the panel refuses to save one that does not. The rendered result is shown under each field, using the track you are playing now (a sample track when nothing is), through the same code that builds the card. **Placeholders** lists what a template can use.
- Choices: `presence.paused_mode` (`show`, or `hide`; `clear` behaves like `hide`), `presence.type` (`listening`, `watching`, `playing`) and `presence.status_display` (`app`, `state`, `details`).
- Buttons: `buttons.enabled`, `buttons.label` (at most 32 characters; empty or `Listen along` gives the service-specific label) and `buttons.spotify_search_fallback`.

The panel saves through `/api/card`, which rejects unknown fields, out-of-range choices and bad templates without changing the file.

## Hiding tracks

Some tracks should never reach your profile. A hidden track is treated like a blocked source: it is never sent to Discord, nothing is looked up for it (no artwork request), and a card already showing for it is cleared at the next rate step. Unlike a blocked source, it still shows in the panel preview, marked **Hidden from Discord**.

- **Hide this track** on the preview offers **Hide this song** (artist and title together) and **Hide this artist**. The **Hidden from Discord** list in the Discord card section removes a rule again.
- Rules live in `cadence.toml`, one table each. A rule names an artist, a title or both; every named field must match:

```toml
[[sources.hidden]]
  artist = "Some Artist"

[[sources.hidden]]
  artist = "Another Artist"
  title = "Song * (Live)"
```

- A field matches the whole value, ignoring case and surrounding spaces. `*` stands for any run of characters (`Song *`, `* (Live)`); there is nothing else, so no regular expressions. A rule made only of `*`, or empty, is ignored so it can never hide everything. A title that really contains `*` also matches more than itself.
- At most 200 rules, each field at most 128 characters. Hiding applies to the track Yozora has chosen: while a hidden track is the one playing from the highest-priority source, a lower-priority source is not used instead.

## Performance

With media playing, Yozora checks Windows media sessions every `sources.poll_ms` (default 1000 ms) and updates its state every 250 ms. After a minute with no media session and nothing on your Discord profile, it slows to one session check every 5 seconds and one update every 2 seconds. It returns to full speed as soon as any session appears. A newly started track therefore shows in Yozora within about 5.25 seconds at worst, and on Discord after the usual `presence.track_debounce_ms` (default 500 ms) on top. While a card is still on your profile, Yozora stays at full speed so the `presence.idle_clear_secs` clear is not delayed. A `poll_ms` of 5000 or more is left as it is.

## Logs

The daemon writes `%LOCALAPPDATA%\uika-resonance\logs\uika-resonance.log`. When the file passes 5 MB it is renamed to `uika-resonance.log.1`, replacing the previous one, and a new file starts, so the logs never take more than about 10 MB however long Yozora runs. `log.level` in `cadence.toml` is read at startup:

```toml
[log]
level = "info"   # debug, info, warn, error or off
```

`debug` adds the source file and line to each entry and `off` writes no log file. Yozora's log lines carry no severity yet, so `warn` and `error` currently write the same lines as `info`. An unknown value is logged and treated as `info`.

## Config file

Settings live in `%APPDATA%\uika-resonance\cadence.toml`. Each time it loads or saves cleanly, Yozora keeps a copy beside it as `cadence.toml.last-good`. If a hand edit breaks the file while Yozora is running, the change is ignored, the previous settings stay in use, and the log says why. If the file is broken when Yozora starts, it starts with the last good copy instead, moves your edited file to `cadence.toml.broken-YYYYMMDD-HHMMSS` so you can fix it, and writes a `config: WARNING` line to the log naming both files. The control panel also shows a line under the preview naming the file it set aside, until you dismiss it. Without a usable last-good copy, startup fails with the parse error as before.

## Verification

Private Windows named-pipe regression tests exercise simultaneous read/write using a unique `yozora-test-*` pipe, not the live Discord client. Ordinary `go test ./...` never publishes experimental activity to a Discord profile. Tests also cover slow artwork, session gaps and out-of-order lookups, coherent track/art transitions, paused clear/resume and visibility-policy reloads under the send-rate gate, label-only Discord button readback, loopback-art filtering, source-toggle API validation and concurrency, launcher endpoint validation, service names, icon placement, platform-specific links, button ordering, display enums, config bounds, and newest-snapshot retention.

The embedded control panel also has a browser regression check using synthetic media and settings. It exercises source toggles, every theme (applied, saved, rolled back on failure, and checked for 4.5:1 text), motion (no animation at idle in any theme, cues fire once per change and never on reconnect, every drag, cancel and reorder ends with no leftover transform or running animation, busy cursors clear after failed saves, Windows reduced motion and the Reduced mode each disable everything, and Full overrides Windows), mouse/touch/keyboard priority ordering (including cancellation and edge scrolling), save failures, the Appearance dialog, that each theme loads its own fonts, the Twitch card, the first-run checklist (test card states, the unconfirmed-card hint, application ID validation, completion, reopening) and the Spotify connect states, Discord-offline and panel-offline notes, the config-restored notice, the version line, Japanese (the saved payload, translated key regions, and no overflow at 320px), Windows High Contrast (visible focus rings, chosen tiles, segments, checks and drop line), responsive layouts, and the production content-security policy without running the daemon or contacting Discord. On Linux with Node.js:

```sh
npm install --prefix /tmp/yozora-ui-tools --no-save --no-package-lock playwright@1.62.1
/tmp/yozora-ui-tools/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/yozora-ui-tools/node_modules node scripts/check-ui.cjs
```

To use an existing Chromium installation, set `CHROMIUM_PATH` to its executable instead of installing Playwright's browser. Screenshots and `results.json` are written to the system temporary directory under `yozora-ui-check`; set `UI_EVIDENCE_DIR` to choose another location. These browser checks complement the Windows Go tests; they do not exercise native WebView2 or media detection.

## Release

A merge to `main` that changes what ships tests, builds and publishes a release automatically (`.github/workflows/release.yml`): Go sources (not `_test.go` files), `go.mod`/`go.sum`, the `.syso` icon resources, `installer/`, `assets/yozora.ico`, the embedded panel (`internal/server/ui.html` and `internal/server/fonts/`), the tray icon, or the workflow itself. Documentation, scripts and test-only merges do not cut a release. The version is the latest `v0.1.N` tag plus one, so the download links above always point at the newest build; it is stamped into the binary (`Yozora.exe version`) and the installer. Use the "Run workflow" button on the release workflow to republish without a code change.

### Code signing

The workflow has signing steps for [SignPath Foundation](https://signpath.org), which signs open-source projects for free. They stay skipped, and releases stay unsigned, until the owner sets them up:

1. Apply at signpath.org for the open-source program (the project must be OSI-licensed, MIT qualifies, and built on GitHub-hosted runners, which it is).
2. In SignPath, create a project (slug `yozora`), a signing policy (`release-signing`) and two artifact configurations: `exe`, a zip file containing a `<pe-file path="Yozora.exe">`, and `setup`, a zip file containing a `<pe-file path="Yozora-Setup.exe">`. Link the GitHub repository as a trusted build system.
3. In this repository's Settings > Secrets and variables > Actions, add the secret `SIGNPATH_API_TOKEN` (a CI user's API token) and the variable `SIGNPATH_ORGANIZATION_ID`. Variables `SIGNPATH_PROJECT_SLUG`, `SIGNPATH_POLICY_SLUG`, `SIGNPATH_EXE_CONFIG_SLUG` and `SIGNPATH_SETUP_CONFIG_SLUG` override the slugs above if you chose others.

Once both exist, the next release signs `Yozora.exe` before it is packed into the installer, then signs `Yozora-Setup.exe`, and fails if either signature is not valid. The uninstaller inside the setup is not signed. SignPath was chosen over Azure Trusted Signing because it is free for open-source projects and accepts individual maintainers wherever they live; Azure Trusted Signing is a paid Azure subscription and, at the time of writing, only verifies individual developers in the US and Canada. With SignPath Foundation the certificate names SignPath Foundation as publisher, not the owner. Remove the SmartScreen note under Installing once releases are signed.

### winget

`packaging/winget/` holds manifest templates (package `Mariyaholic.Yozora`, Inno installer, per-user scope). Each release fills in the version, download URL and installer SHA256 and uploads them as the `winget-manifests-<version>` build artifact on the workflow run; nothing is sent to winget automatically. To publish the first version, download that artifact and open a pull request to [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) with the three files under `manifests/m/Mariyaholic/Yozora/<version>/` (validate locally first with `winget validate` and `winget install --manifest`). Later versions can be submitted the same way or with `wingetcreate update Mariyaholic.Yozora`. Submit after signing is set up if you can: unsigned installers are accepted but may be held for extra review.

## Notes

Yozora is not affiliated with or endorsed by Discord, Spotify, or Apple.

YouTube Music is a dedicated source, enabled by default, for its browser PWA and supported desktop apps. It always uses Listening activity, shows the Windows media thumbnail in the dashboard, and uses a fixed YouTube Music symbol on Discord without catalog lookups or a listen button. Custom app filters such as `msedge` still block the YouTube Music PWA; its source toggle cannot override them.

## License

MIT, see [LICENSE](LICENSE).
