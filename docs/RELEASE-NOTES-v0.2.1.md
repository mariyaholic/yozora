# Yozora v0.2.1

Pending release from [PR #15](https://github.com/mariyaholic/yozora/pull/15).

## Fixes

- Prevent cancelled or replaced Spotify authorization from reconnecting later,
  and stop older token refresh writers before replacing credentials. Report
  credential deletion failures instead of silently treating them as success.
- Keep configuration publication and callbacks ordered. Preserve unsaved source
  settings when tray changes conflict with a panel save, with a notice and retry
  path instead of overwriting the newer settings.
- Clean up failed update downloads and prevent installer launch after
  cancellation.
- Restore artwork cache accounting after restart and keep eviction within its
  configured bounds.
- Tighten startup-setting validation and HTTP route/method handling, and restore
  keyboard focus when editing hidden-track rules.

## Validation

The PR records repeated portable logic and race checks, Windows app/test-binary
cross-compilation, and 53 synthetic Chromium checks. See the
[build assessment](BUILD-ASSESSMENT.md) and [tracked evidence](evidence/README.md)
for results and limitations. Windows-native/live-provider integration, installer
execution and process-tree memory parity remain unverified.

## Versioning

The latest tag at preparation is `v0.2.0`. The existing release workflow uses
series `0.2` and increments the highest matching patch tag, so its next release
is `v0.2.1` if no intervening release occurs. It stamps that version into the
binary, installer and winget manifests; local and PR builds keep `dev`.

These notes prepare the release. They do not publish a tag or release. A merge to
`main` containing the PR's shipping changes will trigger the existing release
workflow.
