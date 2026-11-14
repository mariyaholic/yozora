# Yozora build and capability assessment

**Assessment date:** 14 November 2026  
**Repository baseline:** `1c4659fd` (`main`)  
**Review branch:** `fix/assessment-state-machines`

This assessment covers the Windows application source, portable decision logic and browser panel. Candidate implementation `d03089e` is complete and cross-built. The Linux workspace cannot establish native detector, tray, WebView2, Credential Manager, installer or live Discord behavior. The verification record separates those boundaries.

## Summary

Yozora is a single-executable Windows music detector that turns Windows media sessions into Discord Rich Presence. It includes a short-lived local control panel, a tray-resident daemon, per-source enablement and priority, optional Spotify Web API enrichment, card customization, share-pause and hide rules, diagnostics, and update controls. The main product value is the integrated lifecycle and precise controls: the user can select which source wins, shape the card, and stop selected tracks reaching Discord.

The repository has a broad failure-aware panel and regression harness. The final synthetic browser run for this assessment passed 53 checks, with no browser page errors and no content-security-policy violations. It used Linux Chromium, a local fixture server, and synthetic track/configuration values. It did not contact Discord, Spotify, Microsoft, or artwork services. The Windows application and test binaries cross-compiled with Go 1.27.1; they were not executed on Windows in this workspace. The candidate binary is 12,468,736 bytes versus the baseline 12,448,768 bytes, a 19,968-byte on-disk increase. This is a file-size comparison, not a RAM measurement. See [evidence README](evidence/README.md) and [interaction/state inventory](INTERACTION-STATES.md).

The Windows-only integration boundary is the largest validation gap. No Windows runtime, live Discord connection, live Spotify authorization, real media session, or installer run is established by the browser evidence. The README's low-memory statement is a product claim; this assessment contains no measured RAM or CPU trace.

## Capability inventory

| Area | Implemented capability | Evidence boundary / limitation |
| --- | --- | --- |
| Install and lifecycle | Per-user setup, Start menu entry, one executable for daemon and panel, tray open/quit actions, optional sign-in start, portable executable | Windows installer, tray behavior, and process hand-off need a Windows run. The installer is not code-signed; SmartScreen may warn. |
| Media sources | Apple Music, Spotify desktop, Spotify Web API, YouTube Music, browser media, and generic players; six-source priority; separate YouTube, Twitch, and background Edge-tab switches | Session detection, player identity and UI Automation are Windows-specific. Site toggles depend on Browser media and browser title/tab evidence. |
| Presence card | Listening / Watching / Playing modes, templates, status display, buttons, image tooltips, playback progress, paused mode, artwork resolution | Discord is required for the local Rich Presence path. Discord does not give Yozora a reliable view of the user's public profile, so a test card can be accepted without confirmed readback. |
| Optional Spotify Web API | PKCE authorization, exact artwork and track links, token storage in Windows Credential Manager, polling and disconnect | README says the token flow is tested against a local fake endpoint, not live Spotify. Real browser callback and Credential Manager behavior remain Windows integration checks. |
| Privacy controls | Pause sharing temporarily or until resumed; hide one track or artist; up to 200 hidden rules; diagnostics report avoids track text and token/client identifiers | Pause is in memory and clears on restart. Hide and block rules affect the chosen highest-priority source, not fallback selection. Validate the actual publication behavior on Windows/Discord. |
| Appearance and accessibility | Eleven themes, English/Japanese UI, Follow Windows/Reduced/Full motion, keyboard and pointer/touch priority reorder, contrast styling, responsive narrow layouts | Browser checks test rendering and interaction. Bundled theme fonts cover Latin; Japanese falls back to Windows fonts, so native glyph rendering needs Windows review. |
| Recent and diagnostics | In-memory list of up to 20 distinct tracks published this run; sanitized text report; clipboard fallback | Recent is not persisted and starts empty after restart. Report clipboard handling is browser permission-dependent. |
| Configuration and recovery | TOML settings, last-good copy, broken-file restore notice, config reload, control-panel save and retry flows | File operations, Windows path behavior and concurrent native setting changes require runtime validation. |
| Updates | Release check, per-version dismissal, explicit install action with progress and retryable error states | Download verification, replacement and restart behavior require Windows integration tests. No release was published in this assessment. |

## Strengths

- **A cohesive control surface.** Source enablement and priority share one list. The panel also exposes appearance, startup, card templates, track hiding, setup, updates and diagnostics rather than requiring routine TOML edits.
- **Explicit recovery paths.** Source reads and saves, appearance changes, Spotify connect, updater actions, card templates and first-run setup surface loading/error states and offer retries, rollback, cancellation or a saved-state restore where applicable.
- **Policy is visible.** The README explains source order, blocked sources, hidden tracks, pause duration and Discord confirmation limits. The one-second UI state poll is also reused by setup and Spotify status instead of adding separate loops.
- **Local panel hardening.** The Windows server binds to loopback, authenticates panel/API requests with a per-run token, uses a per-response script nonce and limits page connections to the local origin. Theme fonts are embedded. The synthetic browser harness verifies the panel's production CSP and blocks requests outside its fixture origin.
- **Good interaction coverage.** The synthetic suite exercises English and Japanese, 320–960 px layouts, keyboard/pointer/touch reorder, dirty saves, conflict handling, offline recovery, theme/motion behavior, onboarding, Spotify state changes, updater states, card templates and hidden rules.

## Limitations and risks

1. **Platform proof is incomplete here.** The program depends on Windows media controls, UI Automation, tray APIs, WebView2/native window hosting, Credential Manager, autostart integration, and Discord IPC. Linux HTML rendering cannot substitute for those integrations.
2. **External services are not end-to-end verified.** The README describes local-fake Spotify token tests; the browser harness stubs Spotify, Discord, and updater responses. Art/catalog behavior also depends on external services and network availability.
3. **Discord confirmation is inherently partial.** Yozora can observe its local Rich Presence publication/readback path, not whether a remote profile is visible to every viewer. The checklist states this limitation.
4. **Video classification is best-effort.** YouTube/Twitch identification uses browser session and window/tab evidence. A cold background YouTube tab has no verified video URL, so it cannot provide the verified-video artwork/link. Matching metadata cannot distinguish two videos from the same browser with identical metadata.
5. **Performance claims lack measurement in this review.** No RAM/CPU benchmark was captured. The README's “as low as 10MB” claim should be treated as a reported goal/observation until reproducible Windows measurements are recorded under a stated workload and duration.
6. **Release trust.** The README says the installer is unsigned. Checksums are published with releases, but checksum availability does not replace a trusted code signature.
7. **No durable listening history.** Recent is intentionally memory-only and limited to this process lifetime; diagnostics avoid detailed track history by design.

## Lapses repaired in the candidate

| Baseline lapse | Implemented repair | Regression evidence |
| --- | --- | --- |
| Cancelled/replaced Spotify exchange could still commit | Validate operation generation and order token/config/poller commit against cancel/disconnect | Blocked-exchange and commit-order tests in `pkce_test.go` and `setup_test.go` |
| Old poller could rotate a token after a new authorization | Stop and wait for the old manager before storing its replacement | Injected old-refresh ordering and real-manager lifecycle fixture |
| Credential deletion errors were suppressed | Treat missing credentials as removed; surface real failure while stopping API polling and attempting system mode; permit retry | Errno classification and disconnect failure/retry tests |
| Open panel could overwrite tray settings | Reconcile clean/dirty controls and conditionally save using ETag/If-Match under the shared config lock | Go conflict regression and browser conflict/retry cases |
| Concurrent reloads could publish/apply stale config | Serialize loading/publication and coalesce callbacks through one bounded dispatcher | Stale-load, blocked-hook and reentrant-change regressions |
| Earlier-run thumbnails escaped disk accounting; self-eviction yielded dead URLs | Scan owned files in batches, bound bookkeeping, preserve default art and reject oversize/self-evicted results | Restart, default, oversize and eviction-failure regressions |
| Update failures leaked directories and could launch after cancellation | Retain cleanup path independently, commit install phase/time atomically and check cancellation before launch | Cleanup and cancellation tests fail on baseline, pass on candidate |
| Missing autostart boolean decoded as false | Reuse strict decoder with a required boolean; restrict read methods and unknown root paths | Malformed-body and method/path regressions |
| Hidden-rule deletion lost keyboard focus | Focus live replacement controls or a stable empty-list fallback | English/Japanese success and failure browser checks |

The [repair plan](IMPROVEMENT-PLAN.md), [lightweight feature research](QOL-RESEARCH.md), [learnings](LEARNINGS.md) and [Astra review](ASTRA-REVIEW.md) document scope and tradeoffs. No runtime dependency, resident worker or background timer was added. Whole-process memory parity remains unmeasured. The optimized candidate executable grew by 19,968 bytes in the initial matched cross-build; executable size is a disk metric.

Code consolidation reused strict JSON validation and existing panel polling, centralized authorization commit ownership and replaced nested quadratic artwork sorting with the standard sorter. Tests and lifecycle guards increased source size. Line reduction was not used as a substitute for correctness or memory measurement.

## Verification record

| Check | Result | What it establishes |
| --- | --- | --- |
| `scripts/check-ui.cjs` on Linux, Node 24, system Chromium | **53 checks passed**; `errors: []`; `violations: []` | Synthetic rendering and client interaction behavior at the captured UI revision. Not a native Windows or service test. |
| Fixture-origin isolation in browser suite | Passed | Fixture browser requests to any non-local origin are aborted; the suite does not contact Discord/Spotify or artwork services. |
| Baseline Linux logic and Windows-target build | Passed at `1c4659f` | The original seven-package shadow and optimized Windows build establish the baseline. |
| Candidate extended Linux shadow | Nine packages pass 10 repetitions; presence/Spotify race 100 repetitions; config/server/update race 10 repetitions | Tests execute portable logic and handlers with explicit browser/WinRT/credential/pipe/registry/OS-version/signature substitutions. |
| Candidate Windows application and package test cross-compilation | Passed with Go 1.27.1, Windows amd64, CGO disabled | Produces a PE32+ GUI executable and test executables. Windows binaries were not executed here. |
| Updater regression comparison | Cleanup and pre-launch cancellation fail on baseline and pass on candidate | Tests exercise the original bugs and repaired behavior against the same fixtures. |
| Real Windows media, tray, WebView2, Discord, Spotify, and installer integration | **Not run in this workspace** | Requires a Windows host and real services/accounts. |
| RAM/CPU measurement | **Not run** | No quantitative performance conclusion is made. |
| Windows-target `go vet ./...` | Baseline and candidate both report the same four unsafe-pointer diagnostics in `internal/credman/credman.go` (two), `internal/smtc/thumbnail_test.go`, and `internal/wrt/wrt.go`; output is byte-identical | The command exits 1 on both revisions. These native interop diagnostics remain unsuppressed and require separate Windows review. |

The UI screenshot files under `docs/evidence/` are selected fixture captures. The complete 53-check screenshot set and full `results.json` are retained locally in the ignored `dist/ui-evidence/` directory for this workspace; they are not part of the tracked deliverable. Re-run instructions, hashes and capture provenance are in [evidence README](evidence/README.md).

## Source references

- Product and setup claims: `README.md`
- Panel behavior and API use: `internal/server/ui.html`, `internal/server/server.go`, `internal/server/setup.go`, `internal/server/card.go`, `internal/server/pause.go`, `internal/server/update.go`
- Native media and player identity: `internal/smtc/`, `internal/player/`
- State-machine review: `docs/STATE-MACHINE-AUDIT.md`
- Browser regression harness: `scripts/check-ui.cjs`
