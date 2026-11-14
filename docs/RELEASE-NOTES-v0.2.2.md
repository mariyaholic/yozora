# Yozora v0.2.2

Published 14 November 2026 at 19:42 Asia/Manila. The official `v0.2.1` Setup
installation is the upgrade baseline. [Download v0.2.2](https://github.com/mariyaholic/yozora/releases/tag/v0.2.2).

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
installation. Native Windows CI verified the actual baseline installation,
in-place replacement, exact settings retention, running version and restart
before publication. The release workflow repeated that gate and re-downloaded
the staged assets before publishing. The separate post-publish updater smoke
is initiated through the local updater API; its observed outcome is recorded
in [rollout evidence](rollout-evidence/README.md). A human check of the visible
flow is documented in [the rollout guide](ROLLOUT.md).

For unsigned running builds, updater verification checks the installer hash
against `SHA256SUMS.txt` from the same GitHub release; this detects accidental
corruption but does not independently authenticate the publisher. If a running
build has a valid Authenticode signature, the updater also requires the
installer to have a valid signature from the same signer.

## Validation record

See [rollout evidence](rollout-evidence/README.md) for completed CI and browser
results, including their run identifiers and observed times in Asia/Manila.
Pending checks are identified there rather than described as passing.
