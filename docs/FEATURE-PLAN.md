# Panel feature plan

Implementation started at PR #15 head `819d6aa6626bf5ead081b8e7007d4a56d439cd71` in the separate `feat/panel-presets-backup` branch and worktree. PR #15 was merged during this work. The feature branch incorporates main `01193b8`, whose additional changes are release documentation only, and the new PR targets main with just the feature increment. This task does not rewrite the earlier PR or main.

## Selected features

1. Add four fixed card presets through existing `/api/card` metadata. Selecting one fills the unsaved card form. Existing preview, Apply, Discard and Reset govern validation and persistence. Replacing dirty text needs a deliberate user action.
2. Export saved portable panel settings as a versioned JSON attachment. The exact schema contains `format: "yozora-panel-settings"`, `version: 1`, validated card settings, all visible source toggles/canonical order, and theme/motion/language.
3. Restore through a bounded file read, strict backend validation, a meaningful preview, and explicit confirmation. Preview has no side effects. Import requires the exact preview ETag, checked against freshly loaded supported settings under the existing config mutex. A stale preview returns 412 and missing condition returns 428. Apply all supported sections through one config save/reload, preserving unrelated defined settings and custom source blocks.
4. Save the existing redacted diagnostic report to a text file on request. Share one short-lived browser download helper with settings export and release Blob URLs after use.

## Data and resource boundaries

Backup excludes account connections, Discord/Spotify identifiers, tokens, hidden-track rules, autostart and network/system settings. It contains user-written card text and visible preferences and is a local personal file. Export reads saved settings; unsaved panel edits are excluded. Import does not silently discard dirty source/card edits. Unknown TOML keys/comments follow the existing typed config encoder behavior and are outside backup preservation guarantees.

The JSON limit is 64 KiB. Unknown, missing, null, trailing, malformed and unsupported-version data is rejected before mutation. No runtime dependency, resident worker, extra timer, unbounded history or retained server-side preview is added. Static preset metadata and short-lived browser/file work remain bounded. Whole-process Windows memory parity remains a measurement task.

## Verification

- Exercise preset validation/rendering, unsaved/dirty/Apply/Discard behavior and EN/JA labels.
- Roundtrip supported settings while preserving excluded fields, source custom blocks and typed configuration values.
- Reject malformed and oversized files, unsupported schema, invalid templates/order/toggles and stale imports with no partial write.
- Exercise preview/cancel/reselection/late response/double-click/conflict/retry/save failure states and actual browser downloads.
- Run the existing browser suite plus new regressions under production CSP, at narrow and ordinary widths; capture feature evidence.
- Run extended Linux logic/race checks and Windows application/test cross-compilation. Compare current and baseline native interop vet warnings without suppressing them.
- Have Astra review the implementation and learnings. Transfer only approved general practices to a separate Uika documentation increment. Keep fixture, cross-build, native and live-provider claims separate.

## Delivery

Create a separate draft feature PR, a Windows review-build archive with hashes, a feature assessment/state inventory, selected screenshots and evidence logs. Record remaining Windows/native/service and memory checks explicitly. Reuse prior tooling and consolidate repeated validation/download logic where it improves clarity.

## Completion record

The feature increment is in draft PR #16, following merged PR #15. Core implementation is `e5dc1d4`; test synchronization and CI target coverage follow in separate commits. Astra accepted the static implementation and five general learnings, transferred to Uika PR #18. The full browser result has 55 passing checks with no page/CSP errors. Local logic/race checks and Windows cross-compilation pass, and GitHub's native Windows unit/race/build and Linux jobs pass on `d3de539`. Interactive/live integration, installer checks and Windows memory parity remain separate follow-ups. Build hashes and source revision are recorded in the delivered archive manifest.
