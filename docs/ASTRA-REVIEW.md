# Astra review of the candidate diff

Static review on 2026-11-14 against baseline `1c4659f`; reviewed `IMPROVEMENT-PLAN.md`, `QOL-RESEARCH.md`, OAuth/server/config/artwork/updater/UI diffs and focused test additions. This is a live review record; findings below require reconciliation against the final diff. No production files were edited by this reviewer and no runtime pass is claimed here.

## Resolution update after follow-up edits

The latest reviewed diff resolves the identified code blockers. The server callback now calls `CompleteWithPhases` and stops/waits the old manager before storing the new authorization, removing the old-provider rotation overwrite window. Config publication now uses one bounded coalescing callback dispatcher, including changed reentrant reload coverage and a blocked-hook/newer-publication test. Cache failures now emit a warning and drop failed victims from the index, enforcing the bookkeeping bound while explicitly admitting the disk budget can remain exceeded. Disconnect distinguishes an already-missing credential from actual deletion failure and surfaces the latter while saving system mode. No remaining code blocker was found in this static scope.

The findings below are retained as the review trail, superseded where described above. Follow-up tests establish ordering using injected callbacks; they are not live Windows Credential Manager/provider integration. Follower Reload calls can return after publication but before hooks finish; intermediate callbacks may be coalesced. Credential/config failure is still not rollback-atomic. Memory parity is still unmeasured.

## Original findings and their evidence

1. **High — old Spotify refresh can overwrite a newly authorized token.** `Link.CompleteWith` stores the new refresh token before its server commit calls `Manager.Start`. `Start` then waits for the previous provider to stop. That previous provider may still be in `Provider.Token`, which unconditionally stores a rotated refresh token after its request returns. If its response arrives during that stop wait, the old account/client's token overwrites the new authorization. The Link mutex does not own provider credential writes. Stop/wait the existing provider before storing the new credential, or use shared generation/credential-write ownership. Cover the interleaving with a fake blocked old refresh request and new authorization. A successful callback must leave the new credential authoritative.

2. **Medium — config values are ordered but callback side effects are not.** `Holder.reload` releases `reloadMu` before invoking hooks. Reload A can publish A and pause; B publishes B and applies its hook; A then applies an old log level or artwork country. The new reentrancy test only reloads an unchanged value and does not exercise ordered changed notifications. Either explicitly prohibit reentrant mutation and serialize callbacks with publication, or design a bounded ordered/coalesced dispatcher that supports it. A pointer check immediately before an unlocked callback cannot guarantee ordering. The final test should block A's hook while B arrives and verify final external state matches the latest configuration.

3. **Medium — artwork bookkeeping cap is conditional on deletion success.** `initCache` keeps inserting entries and `trimLocked` retains entries for files it cannot remove. A directory with undeletable thumbnail files can therefore grow the resident map beyond `maxCachedFiles`; the comment claiming a bounded index is too strong. Enforce a separate hard bookkeeping bound and define disk-cap failure behavior, or narrow the claim and retain this as an explicit failure case. Do not silently drop accounting and then claim the disk budget is enforced.

4. **Known remaining partial failure — Disconnect ignores credential deletion errors.** Ordering is now correct relative to Link commits, but a successful API result still cannot prove credential deletion. Missing credentials may be benign; access/storage errors are different. The reported result and documentation should say what actually persisted. Callback config-save failure also leaves a newly stored credential behind: `CompleteWith` is atomic with respect to cancellation, not a rollback transaction across Windows storage and config files.

## Reviewed changes that look sound within their stated scope

- `CompleteWith` checks generation after network exchange and before Store/commit. `CancelWith` orders disconnect against that commit. `CancelState` avoids an old browser-open failure cancelling a newer flow. No current lock inversion was found among Link -> config update and Link -> Manager operations. The callbacks explicitly cannot call Link, and waiting for Manager stop can hold Link's mutex for the existing HTTP timeout; retain this latency limit in the record.
- The updater now retains the created temporary directory independently from named return values, avoiding cleanup against an empty error-return path. Installing phase and its grace timestamp are committed under one lock, and cancellation is checked before launch.
- Autostart requires an explicit boolean and strict payload completion. Root/unknown-path and `/api/state` method behavior is now deliberate.
- UI source reconciliation preserves edits relative to the prior baseline, incorporates untouched external changes, and uses the existing tick. The pre-save read plus `If-Match` protects against tray writes between read and POST. The generation prevents a pre-save poll response from overwriting a completed save. This adds bounded work and an HTTP request to the existing tick; it is not proof of zero CPU/allocation change.
- Hidden-rule focus is now selected from the new rendered nodes, including an empty-list fallback.
- Artwork accounting now includes owned cache files from previous runs, excludes unrelated/default files, refuses an oversized thumbnail before returning its URL, and replaces quadratic sorting. This fixes the ordinary successful-filesystem path subject to finding 3.

## Test and evidence observations

The new fake-token blocking tests exercise meaningful obsolete-result and cancellation interleavings. The callback/disconnect test checks manager and persisted mode, not only HTTP status. UI tests include clean/dirty source reconciliation, stale poll results, 412 retry, read failures and keyboard focus. Cache tests cover previous-run files and oversized-image URLs.

Still needed for the specific findings: old-provider credential rotation vs new authorization, callback side-effect ordering, and deletion-failure bookkeeping behavior. The concurrent reload test's 100 ms observation window should not be described as a fully deterministic proof; explicit barriers or `testing/synctest` can establish that the second contender reached the lock.

Cross-build, shadow logic tests, synthetic browser tests and native/live integration remain separate evidence classes. No claim that all state machines are exhaustively debugged is justified by the available diff. Memory neutrality remains unmeasured on Windows; the research document correctly treats the runtime limit as soft and excludes external/native allocations.

## Documentation review

`QOL-RESEARCH.md` is appropriately scoped as implementation opportunities rather than user demand. It favors existing controls and bounded state, includes provider references, and defers resident features. Its statement about broken URLs after eviction should be read as the oversized/self-eviction path: later legitimate eviction can still make historical URLs unavailable.

`IMPROVEMENT-PLAN.md` is concrete and makes native/runtime limits visible. The phrase “inventory every implicit state machine” should become an enumerated reviewed inventory with named residuals at delivery, rather than an assertion of exhaustive discovery. “Working candidate” should mean a successfully cross-built executable until Windows smoke tests exist.

## Final learnings review and static acceptance

Explicitly reviewed `docs/LEARNINGS.md` after the follow-up code repairs. **Accepted for static review**, with two precise documentation corrections communicated to the root:

1. Replace “Disconnect still stops publication” with “Disconnect stops Spotify API polling and withdraws its snapshot.” Other selected media can still publish, so the original phrase is broader than the implementation.
2. Qualify the ETag claim: it closes the read-to-write race between panel/tray writes serialized by the shared config lock. An external file editor does not participate in the conditional-write protocol.

Keep the old-refresh tests described as injected ordering tests, not a live Credential Manager/provider concurrency pass. The learnings already correctly distinguish cancellation from rollback, follower publication from callback completion, bounded bookkeeping from unenforceable disk deletion, and a reviewed inventory from exhaustive discovery.

Approved universal subset for Uika:

1. Track commit authority through the final side effect, including every competing writer of shared credentials or state.
2. Test cancellation, replacement and concurrent commits with barriers around actual effect boundaries; label synthetic ordering tests accurately.
3. Define publication and callback ordering together, including reentrancy, coalescing and whether return means published or applied; bound pending state.
4. Preserve unsaved edits and use conditional writes for multiple controlled settings writers; state the protocol boundary and remaining external-writer limitations.
5. Validate required mutation fields before effects; distinguish missing resources from real storage failures and report partial outcomes truthfully.
6. Enforce restart-aware bounds; distinguish bookkeeping limits, disk enforcement failures and measured process-memory budgets.
7. Keep source review, fixture tests, cross-builds, native checks and live integrations distinct in the evidence ledger.

These are review practices suitable for transfer to Uika's learning register, not an estate governance change, deployment authorization, universal mandate to introduce new mechanisms, or an assertion that all repositories need identical concurrency designs.

Remaining acceptance boundaries: Windows-native SMTC/UIA/WebView2/Credential Manager/Discord/installer behavior and process-tree memory parity remain outstanding. Root-owned build/test logs must record their actual outcomes separately; this advisor performed static review only. `BUILD-ASSESSMENT.md` and `INTERACTION-STATES.md` were still being assembled when learnings were reviewed, so their final content was not part of this sign-off. No unresolved blocker remains in the code paths reviewed above.
