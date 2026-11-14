# Panel feature engineering learnings

This increment starts at `819d6aa`, the reviewed PR #15 head. It adds fixed card presets, a portable panel-settings backup/restore flow and on-demand diagnostic downloads. The feature PR is separate from the reliability PR. Final check results are indexed in `feature-evidence/`.

## Portable data

A backup needs an explicit contract for both included and preserved settings. The new format projects supported card fields, visible source switches/order and appearance. It never serializes the runtime Config and subtracts private fields afterward. Import loads fresh typed configuration, replaces only those supported fields and preserves defined account, system and custom-source settings. Unknown TOML keys/comments retain the existing encoder's limitations.

User-written card text remains personal content even when credential settings are excluded. This file is a personal preferences backup. The downloaded diagnostic report follows its own redacted shape and should not be confused with settings export.

Preview and import use the same strict decoder and normalization. Duplicate JSON names, case aliases, missing/null required fields, invalid templates, wrong versions and over-limit input are rejected before mutation. Export also validates saved settings: a hand-edited invalid or oversized template must not produce a file that restore would reject. Preview/import can still repair such current settings using a valid backup.

## Commit and draft ownership

A preview is a side-effect-free comparison, not a reservation. Import requires a strong tag computed from current portable state under the shared mutation lock. Excluded-setting changes do not create false conflicts, and the fresh typed load preserves them. An external editor is outside the panel/tray lock and the protocol does not claim cross-process atomicity.

A successful import response establishes that file save and holder reload/publication returned. The existing coalescing dispatcher means it does not establish that every runtime consumer or external Discord effect finished. The first UI version waited for extra GETs before updating baselines; a follow-up failure could conceal a committed save and keep stale forms usable. The final UI updates from the successful response directly, without an extra prerequisite read. A transport timeout is different: the server may have saved, so retry requires fresh preview.

Preserving a whole dirty card form also preserves untouched stale values. The repaired UI merges each field against the former saved baseline: keep user-edited values, adopt imported values for untouched fields, and set the imported state as the new saved baseline. A later Apply therefore preserves both the imported changes and the deliberate local edits. Source switches follow the same principle; ordered source lists remain one atomic edited value.

## Bounded resources and observable completion

Cleanup scheduled through animation frames can stop in a hidden tab. A per-download cleanup callback therefore does not by itself bound outstanding Blob URLs. One retained URL, replaced on the next download and released on page exit, bounds retention independently of callback cadence. Download contents remain capped at 64 KiB.

The browser can request a download and verify fixture bytes. It cannot establish that a file reached the user's chosen disk location. Product copy reports that a download started. Native WebView2 download handling remains a separate Windows smoke test.

New actions share a five-second ephemeral request deadline, including response body consumption. Preview cancellation/replacement also revokes the active transport and checks identity after the local file read, before uploading. Errors restore controls; obsolete preview results cannot restore import eligibility after cancellation or file replacement. The deadline adds no resident timer or polling loop. An import timeout revokes the UI's certainty about completion, not a write that the server already committed. The 64 KiB payload and retained-Blob limit is separate from total transient/process memory.

## Code reuse and verification

Preset forms reuse server defaults and existing card validation/rendering/save. Backup preview and commit share validation/projection, and exports and reports share one download helper. The changes add feature code and tests while avoiding a new dependency, background process, polling loop or profile database. Source line count and executable size remain separate from process memory use.

Tests cover actual schema roundtrips and failure non-effects, competing writes, excluded-field preservation, imported/draft rebasing, stale preview replacement, timed-out actions, repeated downloads, filenames and bytes. Fixture tests, Linux shadow execution, Windows cross-compilation and native/live integration remain separate evidence classes. Windows process-tree memory parity is unmeasured.

## General practices for Uika review

1. Define portable backups as allowlisted projections and test both exported scope and preserved state. Validate exports against the same contract as imports.
2. Share preview/commit normalization, check concurrency at the write boundary and use the commit response as the authoritative receipt for that operation.
3. Rebase drafts per field against their prior saved baseline; preserving an entire stale form can lose unrelated imported changes on the next save.
4. Bound retained resources independently of a callback's scheduling, and bound waits without claiming that client cancellation rolls back a server commit.
5. Report the completion layer observed. A download request, persisted file, applied runtime configuration and native integration pass establish different facts.

These are candidates for Astra review and a documentation-only Uika transfer. They do not change estate governance or authorize deployment.
