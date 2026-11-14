# Yozora engineering learnings

Review date: 14 November 2026. Baseline: `1c4659f`. Terra implemented the repairs, Luna prepared browser evidence and documentation, and Astra reviewed the candidate and these learnings. Final acceptance and remaining platform checks are recorded in `BUILD-ASSESSMENT.md`.

## Asynchronous operations

A cancelled Spotify exchange previously retained permission to store credentials after its network request completed. A generation check now rejects superseded results. Token storage, config save and manager startup share an ordered commit boundary with disconnect. Stopping the old poller before storing a new credential also prevents an old refresh response from overwriting the new account token. Tests block token responses and callback phases to exercise those interleavings.

Cancellation and rollback are separate contracts. A callback config-save failure can leave a newly stored credential behind while the poller remains stopped. The callback reports the save failure. Reconnect or disconnect is the recovery path. Windows storage and a TOML file do not form a transactional store.

An earlier browser-launch failure must cancel only its own authorization state. A global cancel could otherwise discard a newer connect request. Generation and request identity serve different purposes and both need coverage.

## Configuration and multiple writers

Serializing config loading and pointer publication prevents an older slow load from overwriting newer state. Applying callbacks outside that lock preserves reentrancy but originally allowed older side effects to run after newer ones. The repaired dispatcher runs one callback batch at a time and coalesces pending updates to the newest config. Concurrent follower reloads can return after publication but before their callbacks finish. The blocked-hook regression verifies the final applied value follows the latest publication.

The tray and panel share source settings. The panel now merges backend changes against the last saved baseline while retaining local edits. A stateless ETag and `If-Match` guard closes the read-to-write race between panel/tray writes serialized by the config lock. An external text editor does not participate in that lock. A conflict keeps the edits and offers retry. This uses the existing panel tick, without another timer or daemon worker.

## Error and cleanup boundaries

Mutation payloads need explicit required fields. An absent autostart boolean previously decoded as false and could disable startup. Shared strict decoding now rejects missing/null fields, unknown keys and trailing data before side effects.

Named error return values can erase a path needed by deferred cleanup. The update downloader now retains the created directory independently and removes it after failed downloads. Installer phase and grace timestamp change together, and cancellation is checked before launch.

Credential deletion distinguishes missing credentials from access/storage failure. Disconnect stops Spotify API polling, withdraws its snapshot and attempts to save system mode, then reports a real deletion failure so it can be retried. Other media sources can continue publishing. A successful HTTP response must reflect the operations that actually succeeded.

## Resource bounds

The artwork disk limit previously counted only files written during the current process. Startup accounting now includes owned thumbnails from earlier runs. The index is bounded even when eviction fails; a warning states that the disk budget could not be enforced. Files the OS refuses to delete can remain over budget. The default placeholder stays outside thumbnail eviction, and an oversized/self-evicted thumbnail does not produce a dead URL.

Replacing a nested artwork sort with the standard sorter removes quadratic comparison code. Reusing strict JSON decoding, one source reconciliation path and the existing timers reduces duplicated mechanisms. Tests and lifecycle guards add source lines; total line count is not a memory benchmark.

## Interaction completion and evidence

Removing a hidden rule replaced the list DOM and then focused an obsolete node. Focus now targets a live neighboring control or a stable empty-list fallback. English and Japanese browser regressions cover success and failure.

Inventorying routes exposed state transitions in OAuth, updater grace expiry, config recovery and panel saves that the original engine inventory did not include. `INTERACTION-STATES.md` lists the reviewed machines and residual Windows/live-provider transitions. This is a reviewed inventory rather than proof of exhaustive state discovery.

Linux shadow tests substitute Windows integrations. Cross-compilation produces a Windows executable. Synthetic Chromium tests verify panel behavior using fixtures. Each establishes a different claim. None establishes Windows process-tree memory parity, native media behavior, Credential Manager integration, Discord visibility or installer success. Those measurements remain explicit follow-up work.

## Practices proposed for Uika

1. Track commit authority through the final side effect, including every competing credential writer.
2. Test cancellation, replacement and concurrent commits with barriers around the actual boundary.
3. Define publication and callback ordering together; bound any coalescing queue.
4. Preserve unsaved edits and use conditional writes when more than one surface changes settings.
5. Validate required mutation fields before effects, and distinguish missing resources from real failures.
6. Enforce restart-aware bounds and document filesystem failure behavior separately from bookkeeping bounds.
7. Keep source review, fixture tests, cross-builds, native checks and live integrations distinct in the evidence ledger.

These are repository-independent review candidates. Estate governance and deployment are unchanged.
