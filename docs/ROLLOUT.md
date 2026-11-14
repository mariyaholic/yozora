# v0.2.2 rollout and verification

**Baseline:** the published `v0.2.1` release. **Candidate:** the next patch, `v0.2.2`.
Run the human update check only after `v0.2.2` is published as the latest release.
The dated CI and browser evidence is in [rollout-evidence](rollout-evidence/README.md).

## Supported upgrade path

This rollout covers an installation made with the official per-user
`Yozora-Setup.exe` from `v0.2.1`. The one-click path replaces that installation
in place, retains `%APPDATA%\uika-resonance\cadence.toml`, and restarts Yozora
if it was running. A standalone `Yozora.exe`, a development build, or a ZIP with
the executable is not a setup-installed upgrade baseline; use the published
Setup installer manually for those cases.

The published `v0.2.1` build has scheduled discovery only. With no prior update
state, it checks about two minutes after startup plus up to 90 seconds of
jitter. A successful check is remembered in
`%LOCALAPPDATA%\uika-resonance\update.json`, so an existing check can defer the
next scheduled discovery for up to 24 hours. Restarting does not clear that
schedule. `v0.2.1` has no **Check for updates** button. That control is available
only after upgrading to `v0.2.2` or later.

For a predictable human check, use a disposable Windows profile or VM with a
fresh `v0.2.1` Setup install and update checks enabled. Do not remove the user's
existing app data to accelerate the check. If a test profile already has update
state, let its scheduled check become due or create a fresh disposable profile.

## Human update check

After `v0.2.2` is the latest published release:

1. In a disposable Windows profile or VM, install the pinned `v0.2.1`
   `Yozora-Setup.exe` from the `v0.2.1` release. Do not use the portable EXE.
2. Start Yozora and confirm the panel reports `0.2.1`. Record a harmless
   settings choice (for example, the selected appearance) so it can be checked
   after replacement. Keep update checks enabled.
3. Since this baseline has no manual check control, leave Yozora running until
   scheduled discovery finds `0.2.2`. A fresh profile normally checks in about
   2–3.5 minutes. A prior successful check can make it take up to 24 hours.
4. When the panel reports that `0.2.2` is available, press **Update now** and
   allow Setup to finish. The panel closes during replacement; reopen it from
   the tray after Yozora restarts.
5. Confirm the reported version is `0.2.2` and the recorded setting is still
   selected. The update is an in-place Setup upgrade; user settings should
   remain in place.
6. On `0.2.2`, open the control panel's **Startup** section and press **Check for updates** once to confirm
   the new manual check reports that Yozora is up to date. This control did not
   exist on `0.2.1`.

If discovery or installation fails, record the displayed error and the version
shown by the panel. Do not uninstall or purge app data as a troubleshooting
step; use the release's Setup installer manually if an update cannot complete.

## Asset verification modes

Yozora downloads the installer and `SHA256SUMS.txt` over HTTPS from GitHub's
allowed release hosts and checks the installer hash against that release's
checksum file. For an unsigned running executable, the mode is `checksum`: this
detects corruption or truncation, but the checksum and installer come from the
same release and do not establish publisher authenticity. The published
`v0.2.1` release is unsigned, so this is the expected mode for the baseline
upgrade. If the running executable has a valid Authenticode signature, the mode
is `signature`: the downloaded installer must also have a valid signature from
the same signer, in addition to the checksum match. CI checks this continuity
when the baseline is signed. See the `verification` field from `/api/update`
and the release's `SHA256SUMS.txt` when recording evidence.

## CI coverage

The Windows verification job downloads and hash-checks the published `v0.2.1`
assets, installs that exact Setup into an isolated runner profile, writes a
settings marker, replaces it with the version-stamped candidate Setup, and
checks the installed binary version, marker retention, daemon restart and
local service version. The release job repeats the installed replacement and
signing-continuity checks before publishing. A post-publish Windows job uses
the actual `v0.2.1` updater to discover the public candidate and sends the
local updater API's authenticated `POST /api/update/install`; it then checks
the HTTPS asset download, checksum/signature validation, install, restart and
settings retention. That HTTP request is a native CI trigger. The post-publish
smoke runs after the release becomes public, so it does not gate initial
publication. It does not exercise a human click in the WebView2 panel; the
human procedure above covers the visible **Update now** control.

## Observed results

On 14 November 2026, the 55-check Chromium suite passed on revision `12b766f`
at 19:26:36 Asia/Manila (UTC+08:00), with no page errors or CSP violations.
English and Japanese manual-check states were captured separately at 19:29:24
in the same synthetic fixture; the captures do not establish native WebView2 or
live updater behavior. Hosted Windows runs [43589962089](https://github.com/mariyaholic/yozora/actions/runs/43589962089)
and [43590379422](https://github.com/mariyaholic/yozora/actions/runs/43590379422)
passed native package tests, race stress, candidate build/installer build and
Linux logic. Both installed-upgrade smokes failed before candidate replacement:
the first reported `Baseline tray host did not start` at 19:28:19; the follow-up
reported `Baseline tray host did not start on the runner desktop` at 19:33:29.
The follow-up saw runner and daemon in session 2 on the `Default` desktop and
none of three tray startup error strings in the tray log. Post-publish and
human update checks remain pending.
