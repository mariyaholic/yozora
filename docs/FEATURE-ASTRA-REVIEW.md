# Astra review: panel feature increment

Reviewed 14 November 2026 against baseline `819d6aa`, in the separate `feat/panel-presets-backup` worktree. This is static acceptance of the final feature implementation and its general engineering learnings. Final execution results belong to the [feature evidence](feature-evidence/README.md) ledger.

## Verdict

No unresolved blocker was identified in the reviewed implementation. The three additions remain appropriately scoped: four fixed unsaved card presets, portable panel-settings export/preview/import, and an on-demand download of the existing diagnostic report. No new runtime dependency, resident worker, recurring polling loop, profile store or server-side preview history was introduced.

The review covered `internal/server/settings.go`, preset metadata in `card.go`, route registration, the new panel state/download logic in `ui.html`, their server/browser regression tests, and `FEATURE-LEARNINGS.md`. It does not establish native Windows execution or runtime memory parity.

## Accepted contracts

Settings use an explicit versioned projection of card fields, canonical source switches/order and appearance. Exact required fields, duplicate decoded names, case aliases, null values, unsupported versions, trailing data, invalid templates and invalid source/appearance values are rejected. Input and successful downloaded payloads are capped at 64 KiB. Export rejects an invalid or oversized hand-edited card with a safe error; a valid backup can still repair that configuration through preview/import.

Preview and import share validation and normalization. Import requires an exact strong portable-state tag checked against freshly loaded configuration under the shared panel/tray mutation lock. Its incremental length-delimited hash has an unambiguous field layout for the supported canonical projection, without serializing an oversized current template. Import changes only supported fields and preserves excluded defined settings and custom source blocks. The portable tag intentionally permits unrelated settings changes.

Presets derive complete forms from defaults and use the existing validator, renderer and save path. Loading a preset remains unsaved; dirty replacement requires a deliberate choice. Import rebases each card/source switch against the prior saved baseline, preserving deliberate edits while adopting imported values in untouched fields. Source order remains one edited value.

The successful import response updates panel baselines directly. It does not depend on subsequent reads. The forms cannot import while their existing load/save operations are pending. Failure restores usable controls and requires a fresh preview before retrying an import.

Preview cancellation or replacement aborts its retained request controller, checks identity after local file reading before upload, and rejects obsolete responses. The request deadline covers headers and body consumption. Download retention is bounded to one Blob URL, revoked on replacement or page exit; status text reports initiation rather than disk completion.

## Findings resolved during review

- A failed read after a successful import originally concealed the committed save and left stale baselines. The final UI uses the successful response directly and removes the extra read.
- Preserving an entire dirty card originally retained untouched stale values. Per-field rebasing now preserves both imported changes and deliberate local edits on the next Apply.
- Animation-frame cleanup did not bound retained download URLs when callbacks were suspended. A single retained URL provides a scheduling-independent bound.
- Cancellation during local file reading originally allowed an obsolete preview upload. Identity is now checked before sending, and replacement aborts the prior transport.
- Invalid hand-edited saved templates could originally create exports that restore would reject. Export validation now prevents that while retaining repair through valid import.
- Product and documentation completion claims were narrowed to the layer actually observed.

## Evidence and remaining limits

The reviewed tests exercise real preset rendering, portable round trips, preservation of excluded fields, validation failure non-effects, intervening writer conflicts, import save failure, invalid-current-state repair, dirty-field rebasing, preview replacement/cancellation, timeout recovery, repeated downloads, filenames and downloaded fixture bytes. The final targeted English/Japanese browser log inspected during review passed, including production CSP and browser-console checks. The inspected final Linux shadow logic log passed its package checks. The root's complete final suite, race checks, cross-builds and captured screenshots are recorded separately in the evidence ledger; this review does not substitute for them.

The mutation mutex does not serialize an external text editor. Typed TOML serialization retains its existing limitation for unknown keys and comments. File save followed by holder reload is not a transaction with rollback. A successful response establishes that save and holder reload/publication returned; coalesced callbacks and external Discord effects can still complete later. A client timeout cannot retract a server write that already committed.

The 64 KiB limit bounds payloads and the retained download Blob, not all temporary allocations or process memory. Settings files contain arbitrary user-written card text and are personal backups; credential exclusion is not a universal content-redaction guarantee. Diagnostics retain their separate existing sanitization boundary.

Synthetic Chromium, Linux shadow execution and Windows cross-compilation establish different claims. Native WebView2 downloads, real Windows services and credentials, live Discord/Spotify behavior, installer operation and Windows process-tree memory measurements remain separate acceptance work. No native or RSS-parity claim is approved here.

## Learnings approved for Uika

`FEATURE-LEARNINGS.md` was explicitly read and reviewed after the final corrections. All five proposed general practices are approved for a documentation-only Uika transfer:

1. Define portable backups as allowlisted projections; test included and preserved state, and validate export against the import contract.
2. Share preview/commit normalization, check concurrency at the write boundary, and use the commit response as that operation's receipt within its stated completion boundary.
3. Rebase drafts per field against their prior saved baseline; handle an ordered collection as an atomic edited value when that matches its semantics.
4. Bound retained resources independently of callback scheduling; bound waits without equating client cancellation with server rollback.
5. State the completion layer supported by the evidence, keeping request initiation, persistence, runtime application and native integration distinct.

This approval covers those general practices only. It does not change estate governance, authorize deployment, or transfer Yozora-specific provider behavior as a universal guarantee.
