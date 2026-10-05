# Yozora

_Made with love by [Maria](https://github.com/mariyaholic)._

A Windows music detector that publishes Discord Rich Presence. This folder is a self-contained Go module: move the entire folder into the planned standalone repository and the build commands below still work. Internal config/storage names and the executable remain `uika-resonance` for compatibility.

## Public repository placeholder — change at release

The second Discord button is **Yozora**. Its current destination is deliberately a placeholder:

`https://github.com/mariyaholic/yozora`

**The repository has not been published by this work.** Create that public repository, or replace the URL before releasing the app. Do not mistake a working Discord button for a published destination.

The built-in URL lives in `internal/config/config.go`, in `Defaults()` as `Buttons.YozoraURL`. Existing installations can override it in `%APPDATA%\uika-resonance\cadence.toml`:

```toml
[buttons]
  enabled = true
  label = "Listen along"
  spotify_search_fallback = true
  yozora_url = "https://github.com/mariyaholic/yozora"
```

No local drive path or enclosing repository name is used to build the URL. A folder move therefore needs no code change. If the final repository has a different owner/name, update the default and any existing local override.

## Build and run

Requires Windows and Go 1.27.1 or newer, matching `go.mod`.

```sh
go mod download
go test ./... -count=1
go build -o uika-resonance.exe .
go build -ldflags=-H=windowsgui -o Yozora.exe ./cmd/yozora
./uika-resonance.exe serve
```

Commands: `serve`, `status`, `doctor`, `setup`, `install`, `uninstall`. `install` opts into Windows login startup; building and serving do not install autostart.

`Yozora.exe` with no arguments opens the control panel (below); with any argument it behaves like `uika-resonance.exe` (`Yozora.exe status`, `Yozora.exe serve`, ...). The `-H=windowsgui` linker flag keeps double-click launches free of a console window; the executable's icon comes from `cmd/yozora/rsrc_windows_amd64.syso`, generated from `assets/yozora.ico` with `go-winres simply --arch amd64 --icon ../../assets/yozora.ico --manifest gui`.

## Source switches (Yozora.exe)

Double-click **Yozora.exe** to open the control panel in its own native window (WebView2; falls back to your default browser if WebView2 is unavailable). The "Detected sources" checkboxes save on Apply — no config file editing.

- New installations default to **Browser media / videos** and **Other media players** off; the music sources are on. Existing configs keep their explicit choices until you change them.
- Disabling a source stops new detections and clears an activity that source was driving at the next rate step.
- Saved through the existing `sources.blocked` list in `cadence.toml`; custom per-app filters are preserved.

The bundled Discord application ID is public, not a credential. For this local Rich Presence path, desktop Discord must be running; no Discord client secret is required. A custom application ID is supported by `discord.client_id`.

## Card layout

- Header: **Listening to Spotify**, **Listening to Apple Music**, or the known player name. Unknown browser sessions are shown as Browser rather than guessed to be a specific streaming service.
- Large image: album artwork, resolved asynchronously. A slow artwork service must not delay the song text or playback timestamps.
- Small image: Yozora's application icon. Override `discord.small_image` with a Discord asset key or a publicly accessible image URL if the app icon changes.
- First button: **Listen on [service]**. A custom `buttons.label` is preserved; the legacy/default `Listen along` label becomes the service-specific label.
- Second button: **Yozora**, using `buttons.yozora_url`.

Spotify's Windows media controls expose song text but not the canonical Spotify URL. In system mode the button opens a Spotify search for title + artist; optional Spotify API mode supplies the exact track URL. Apple Music uses the resolved Apple Music track link when available and an Apple Music search before lookup completes. Music links never use an Apple Music lookup result for Spotify. Unknown sources need a genuine source-provided URL; the app does not fabricate a platform link.

Browser videos use only the thumbnail the browser itself supplies through Windows media controls, and browser titles are never matched against music catalogs. YouTube pages usually expose no public thumbnail URL to this API, and Discord's servers cannot fetch the local dashboard's `127.0.0.1` art cache — so a browser card shows whatever artwork is available in the local dashboard, while its Discord presence may appear without a large image (text, timestamps, and the Yozora button still show).

## Responsive polling

```toml
[sources]
  poll_ms = 1000

[presence]
  type = "listening"
  status_display = "name"
  min_update_secs = 5
  track_debounce_ms = 500
```

The Windows detector reads once at startup, then polls every second by default. Its one-slot queue retains the newest snapshot, not a stale queued one. The presence engine checks every 250 ms; the brief debounce suppresses rapid skip bursts. Discord sends remain spaced by at least five seconds, so fast detection is not a promise of an immediate Discord-visible update. Artwork can arrive as a later enriched update.

`poll_ms` is read when the daemon starts; restart after changing it. Local polling is separate from `spotify.poll_secs`, which applies only to the optional network/API provider.

## Verification

Private Windows named-pipe regression tests exercise simultaneous read/write using a unique `yozora-test-*` pipe, not the live Discord client. Ordinary `go test ./...` never publishes experimental activity to a Discord profile. Tests also cover slow artwork, session gaps and out-of-order lookups, coherent track/art transitions, paused clear/resume and visibility-policy reloads under the send-rate gate, label-only Discord button readback, loopback-art filtering, source-toggle API validation and concurrency, launcher endpoint validation, service names, icon placement, platform-specific links, button ordering, display enums, config bounds, and newest-snapshot retention.

## Publishing

A local git repository is prepared for the first public push (branch `main`, initial commit included).

```sh
gh auth switch --user mariyaholic
gh repo create mariyaholic/yozora --public --source . --remote origin --push
```

If the account is already active, the second command alone is enough. After the push, the in-app Yozora button's default URL (`buttons.yozora_url`) already points at this repository.
