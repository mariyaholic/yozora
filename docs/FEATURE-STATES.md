# Feature state machines: panel presets, settings backup, and diagnostics download

**Scope:** the three panel additions on `feat/panel-presets-backup`, compared with baseline `819d6aa`. These flows use the authenticated loopback panel API and the existing card, source, and appearance controls. This is a source-based account of expected states, with UI evidence indexed in [feature evidence](feature-evidence/README.md).

## Built-in card presets

`GET /api/card` returns the saved card form, defaults, hidden rules, limits, and the built-in preset definitions. The four presets are complete forms. They are not written to configuration by the read or selection operation.

| State / transition | Behavior and recovery |
| --- | --- |
| Loading → ready | The panel populates the preset selector and card form from the response. On GET failure the form stays unavailable, an error is shown, and Retry repeats the load. |
| Choose preset → load into form | Load replaces the editable fields and refreshes the preview. The form becomes dirty and says the preset must be applied to save. Apply remains the existing `POST /api/card` path. |
| Dirty card → choose preset | Loading over dirty values asks for confirmation. Confirm replaces the form; cancel leaves the user's edited values in place and does not save. |
| Preview after load | A preview request renders against the current activity or the existing sample when idle. Preview validation/render errors use the card form's existing error state; edit the template and retry preview. |
| Apply success / failure | Success updates the saved baseline. Failure retains form values and allows correction or retry through Apply. Discard restores the most recently loaded/saved card; Reset fills defaults in the form and remains unsaved until Apply. |

The preset list is fixed in code: Default follows reset defaults; Compact omits the large and small text fields; Album emphasizes album/title; Detailed includes conditional album and track-number text. Preset selection is not an API write and does not update the daemon until Apply succeeds.

## Portable settings export

`GET /api/settings` serializes current persisted card, source-switch/order, and appearance values as format `yozora-panel-settings`, version `1`, with `Cache-Control: no-store` and an attachment filename. The client starts a download named `yozora-panel-settings.json` after building a Blob; it refuses a Blob larger than 64 KiB. Export does not include in-progress card or source form edits. A browser download status means the download started; it does not prove the user kept the file.

| State / transition | Behavior and recovery |
| --- | --- |
| Ready → exporting | Disable export while the GET is pending. On success, start the JSON download and announce that it started. |
| Export request / size error | Show an error and re-enable the button. Network/body reads have a five-second deadline; click Export again to retry after a timeout or fetch error. If the current saved card is too large or fails portable-schema validation, the endpoint returns `422` without exposing its private contents. Repair the card settings or import a valid backup, then try Export again. The endpoint is read-only. |

## Portable settings import

Import has a separate preview phase and an explicit Apply phase. Selecting a file starts preview automatically; the Preview selected file button can repeat it. Server validation is authoritative even though the browser first parses JSON syntax.

| State / transition | Behavior and recovery |
| --- | --- |
| No file → selected | Enable Preview. Cancel clears the selected file, review candidate, and status. Selecting a different file invalidates the old review and starts a new preview. |
| Selected → previewing | Disable Preview while a request is pending. File selection, Cancel, and Export remain available; changing the file starts a newer preview. Reject a file over 64 KiB in the browser. Send the file bytes to `POST /api/settings/preview`; the server independently bounds and validates it. |
| Preview valid → review ready | Show the candidate card, source switches/order, and appearance values as text, plus a three-part changed summary. Save the returned ETag with the candidate. No configuration is written yet. |
| Preview invalid / request failure | Hide any stale review and report a size, syntax, schema, unsupported version, handler, or read error. The file remains selected; Preview can retry. No setting is applied. |
| Review ready → apply requested | If card or source controls contain unsaved edits, ask whether to import while keeping those form edits. Canceling the prompt preserves the review and sends no POST. Confirming continues. |
| Applying | If card, source, or appearance settings are still loading/saving, show a wait message and send no request. Otherwise invalidate the one-use review and disable card/source/appearance fields, file selection, Cancel, and export. Send the candidate to `POST /api/settings/import` with `If-Match` set to the preview ETag. The server validates the document again, compares the current ETag under its lock, saves the config, and reloads the holder. |
| Import success | Use the server's reloaded settings response to update baselines and the panel. Preserve per-field card edits that differ from the prior baseline and reconcile source drafts; imported values become the new saved baseline. The response confirms config save and holder reload returned, not that every config callback or Discord publication has finished. |
| Conditional conflict (`412`) | No import write is performed. Report that settings changed and that the file must be previewed again. The prior candidate/tag were invalidated before Apply; click Preview selected file to compare against current settings. |
| Save, timeout, or response failure | Report that the import may have been saved but could not be confirmed and require a fresh preview before another Apply. Network and response-body work has a five-second client deadline. A save error does not confirm a write. A reload error is different: the handler reports that settings were saved but could not reload, so the persisted file may already contain the candidate. Inspect current settings before retrying. |

### Late results and cancellation

Every preview attempt advances a sequence number. Changing the file or pressing Cancel invalidates the sequence and hides the review. The previous preview controller is aborted, and a file-read result is checked against the sequence before sending bytes; an obsolete local file is never uploaded. At most one preview transport is active. While preview is active, file selection and Cancel remain available. Selecting another file starts a newer preview; Cancel clears the selection. A late response is ignored and cannot replace a newer candidate. Preview network/body reads have a five-second deadline. Errors retain the file so the user can retry.

There is no user Cancel control once Apply starts. The review is invalidated, file selection and Cancel are disabled, and the forms are locked until the request path finishes or times out. After five seconds the browser aborts its request wait, but the server may still be processing it; an import could commit after the client stops waiting. A response/reload error or timeout therefore leaves commit status uncertain. Retry always requires a fresh preview, and the user should inspect saved settings before retrying. No rollback is promised.

### Validation and version boundary

The JSON payload limit is 64 KiB for preview and import requests, independent of total process memory. The server rejects duplicate object keys (including escaped spelling variants), unknown or missing keys, null required values, invalid UTF-8, trailing JSON, unsupported format/version, incomplete/invalid card data, an invalid source switch/order set, and unsupported theme/motion/language values. Version 1 has an exact schema; forward compatibility is not implied. A future format version must be explicitly supported in code.

The ETag is derived incrementally from the current values in the portable settings projection, rather than file timestamps. It can be produced even when the export endpoint rejects an unportable current card, allowing a valid backup to be previewed and imported to repair it. A preview can be applied only while the projected values still match. Changes outside the three exported groups do not invalidate the tag because the import cannot overwrite them.

## Diagnostics text download

The existing Copy report action and new Save report action independently fetch `GET /api/diagnostics` on demand. The endpoint returns UTF-8 plain text with no cache. Save report starts a download named `yozora-diagnostics.txt` using the shared bounded Blob helper; network/body reads have a five-second deadline. The button is disabled until the request completes, then re-enabled. The helper keeps at most one Blob URL, revokes it when replaced, and cleans it up on `pagehide`.

| State / transition | Behavior and recovery |
| --- | --- |
| Ready → fetching report | User clicks Save report. No background polling or automatic download occurs. |
| Fetch succeeds → download started | Start the generated report download and announce that it started. The report includes version/Windows build, source/artwork route, Discord connectivity, sanitized last error, enabled sources, and theme. |
| Fetch or download-size failure | Show a status error and re-enable Save report. Click again to retry. A failed download does not affect settings or the running daemon. |
| Copy path | Copy still attempts clipboard access. If permission is denied, the panel exposes selectable report text for manual copy. A failed report fetch leaves an error status. |

Diagnostics are generated per request. The report builder intentionally has no track title/artist/album fields and sanitizes known sensitive substrings in the last-error field. This is a scoped redaction policy, not a promise that all arbitrary future error messages are free of identifying text.
