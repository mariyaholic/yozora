# Feature assessment: presets, settings backup, and diagnostics download

**Review date:** 14 November 2026  
**Branch:** `feat/panel-presets-backup`  
**Baseline:** `819d6aa` (`docs: record build assessment state inventory and reviewed learnings`)

This note assesses the feature work on this branch against the baseline. It supplements the earlier build assessment and state inventory; it does not replace or rewrite the existing baseline evidence under `docs/evidence/`.

## Assessment

The three additions address separate panel tasks with small, user-driven flows. Card presets give new users a starting layout without changing saved settings. The portable backup endpoint moves a deliberately limited set of panel preferences between installations. Diagnostics can now be saved as a text file as well as copied. Settings import has meaningful safeguards: bounded input, exact versioned schema validation, a review step, and a conditional write tied to the settings observed during preview.

The implementation is suitable for review as a local panel feature. It does not add a resident worker, periodic network activity, or a new runtime dependency. Confidence is strongest for schema and panel behavior exercised by automated tests; Linux Chromium fixtures and Windows-target compilation do not establish behavior in a real Windows panel or daemon. See the evidence note for the exact run and its limits.

| Feature | Observed behavior | Assessment |
| --- | --- | --- |
| Built-in card presets | Default, Compact, Album, and Detailed presets load a complete card form. Selecting one does not save it. The user can preview and edit it, then use the existing Apply action. | A low-risk way to make card setup approachable. The user retains control over the final save. Presets are fixed examples, not a user-managed preset library. |
| Portable panel settings | On demand, export starts a JSON download. Import previews and validates a selected file before an explicit Apply, and rejects a stale preview if saved settings have changed. | Good guardrails for a local configuration transfer. It is intentionally a partial settings backup, not a full application backup. |
| Diagnostics download | The diagnostics report can be downloaded as `yozora-diagnostics.txt`; the existing copy action remains available. | Useful for attaching a compact report to a support request. The report omits track fields and redacts known private text in its error line, but users should still review it before sharing. |

## Portable backup contents and exclusions

Version 1 contains exactly these three groups:

- **Card:** pause behavior, Discord activity type and status display, button settings, Spotify search fallback, and all card template fields.
- **Sources:** enabled/disabled values for the panel's source switches and the order of its canonical sources.
- **Appearance:** theme, motion, and language.

The backup does not include hidden-track rules; Spotify authorization, refresh token, or client ID; Discord client ID; startup/autostart choice; setup completion state; update state; recent tracks; machine-specific paths; or unrelated configuration and secrets. Import changes only the three included groups. Existing non-panel source-block entries are retained when applying the switch values.

This scope keeps the file focused on portable panel preferences and avoids moving account credentials or machine-specific state. Card template text and button labels are user-editable and could contain personal text, so review the JSON before sharing it. A user who expects a full machine restore would need to preserve the application's configuration and credentials separately.

## Validation and write safeguards

The JSON document is capped at 64 KiB both before browser upload/download and at the HTTP handler. The server accepts only `format: "yozora-panel-settings"` with integer `version: 1`, the exact required root and nested fields, no duplicate keys, no unknown fields, complete card values, a valid source order/switch set, and supported appearance values. Invalid schema or values fail during preview; the current settings remain unchanged.

Preview displays the candidate settings and a changed/not-changed summary for card, sources, and appearance. The server returns an ETag derived from its current exported settings. Apply submits the reviewed candidate with that ETag. The handler reloads the persisted settings while holding the settings lock and returns `412` if the current settings no longer match the preview tag. A stale preview therefore must be reviewed again before it can be applied.

The panel warns when card or source forms contain unsaved edits. If the user proceeds, those form edits are retained while imported values become the saved baseline; the user may then Apply or Discard them through the existing controls. Canceling that warning does not send the import. The apply request disables the editable forms until it completes.

There is no transaction rollback claim for an error after the configuration file has been saved. In particular, a reload failure is reported as “settings saved but could not reload”; the persisted file may already contain the import even though the UI did not confirm a successful active reload. Users should re-open/review the panel state before retrying.

## Diagnostics contents and privacy boundary

The report includes Yozora version, Windows version, selected source, artwork route, Discord connection state, a sanitized last-error line, enabled source names, and theme. Track title, artist, and album are not report fields. The error sanitizer replaces known user-profile paths, the current Windows username, current track text, URL query strings, long token-like strings, and 15-or-more-digit values; it also bounds the error length.

This is deliberate redaction for the report shape covered by source and unit checks, not a general guarantee that arbitrary future error messages cannot contain identifying text. Read the downloaded text before posting it. Diagnostics are fetched only after the user clicks Copy or Save report; the Save report button downloads plain text and can be retried after an HTTP failure.

## Evidence strength and limits

The complete browser result contains 55 checks, with no page errors or CSP violations. Local extended shadow checks pass nine packages ten times, presence/Spotify race checks one hundred times, and config/server/update race checks ten times. All 17 Windows package test executables and the application cross-compile. The Windows-target vet result retains the same four native interop diagnostics as baseline; their output order differs.

[GitHub verification run 43584211089](https://github.com/mariyaholic/yozora/actions/runs/43584211089) also passed on `d3de539`: native Windows package tests, selected race checks and the Windows app build, plus Linux logic verification. This is native unit execution on a hosted runner. It does not establish interactive WebView2 downloads, live media/accounts, installer behavior or process-tree memory parity. The receipt and selected execution lines are in `feature-evidence/`.

The review combines source inspection, feature-specific server/unit checks, and the final synthetic browser run recorded in [feature evidence](feature-evidence/README.md). The browser fixture supplies local fake API responses and synthetic track/artwork/Discord data. It checks rendering and interactions, not the native service, real user files, real Discord or Spotify accounts, Windows credential storage, or an installed Windows build. Cross-compilation is not execution.

The 64 KiB cap, accepted JSON shape, conflict guard, and redaction behavior can be stated from the implementation and its targeted tests. A successful import response means the config save and holder reload returned; it does not prove every coalesced configuration callback or Discord publication has finished. UI screenshots establish only the captured fixture state. Whole-process memory parity and runtime resource effects were not measured. The features are on-demand; no claim is made about quantitative performance improvement.

## Related notes

- [Feature state machines](FEATURE-STATES.md) describes normal transitions, errors, conflict, cancellation, and retry.
- [Feature evidence](feature-evidence/README.md) records screenshots, complete UI results, hashes, and environment metadata.
- [Build assessment](BUILD-ASSESSMENT.md) and [interaction inventory](INTERACTION-STATES.md) remain the prior assessment of the wider application.
