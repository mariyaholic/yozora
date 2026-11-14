# Yozora v0.2.2

Planned next patch after the published `v0.2.1` baseline. These notes describe
the candidate and do not publish it.

## Updates

- Add **Check for updates** in the control panel’s Startup section. It asks for the latest release on
  demand and reports whether an update is available or the check failed. This
  control is available in `v0.2.2` and later; `v0.2.1` continues to discover
  releases on its existing schedule.
- Keep **Update now** as an explicit action. Yozora does not install a release
  until the user chooses to update.

## Panel features

- Add Default, Compact, Album and Detailed card presets. Loading a preset fills the unsaved form; the existing preview, Apply and Discard actions control persistence.
- Export supported saved card, source and appearance preferences to a bounded personal backup. Restore through validation, preview and an explicit conditional import while preserving deliberately edited fields and excluded settings.
- Download the existing redacted diagnostic report as a text file on request. No new resident worker, periodic feature polling or runtime dependency is introduced.

## Rollout

The intended baseline is the published, unsigned `v0.2.1` per-user Setup
installation. Windows CI verifies an isolated in-place Setup upgrade, settings
retention, version stamping and restart before publication, then exercises the
published `v0.2.1` updater against public release assets. The post-publish CI
install is initiated through the local updater API, not through a WebView2
button click. A human check of the visible flow is documented in
[the rollout guide](ROLLOUT.md).

For unsigned running builds, updater verification checks the installer hash
against `SHA256SUMS.txt` from the same GitHub release; this detects accidental
corruption but does not independently authenticate the publisher. If a running
build has a valid Authenticode signature, the updater also requires the
installer to have a valid signature from the same signer.

## Validation record

See [rollout evidence](rollout-evidence/README.md) for completed CI and browser
results, including their run identifiers and observed times in Asia/Manila.
Pending checks are identified there rather than described as passing.
