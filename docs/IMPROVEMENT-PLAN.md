# Yozora build improvement plan

Baseline: `main` at `1c4659fdce23d78e19b061111cfd57c584614a28`, checked out 14 November 2026. Work branch: `fix/assessment-state-machines`.

## Assessment and acceptance

Keep the single executable, separate short-lived panel, bounded history/artwork caches and existing polling architecture. Add no runtime dependencies, resident workers or persistent telemetry. Memory neutrality is an implementation constraint; unchanged Windows process memory cannot be established on a Linux host.

1. Record capabilities, strengths and limitations against implementation evidence. Inventory every implicit state machine, including error, cancellation, retry and shutdown transitions.
2. Repair confirmed interaction lapses first. Initial inspection found cancelled/replaced Spotify authorization can still commit, failed updater downloads can leak temporary directories, installer state transitions can admit duplicate work, and malformed autostart JSON can disable startup.
3. Add focused regression coverage for each repair, including deterministic concurrent/cancelled operations and malformed requests. Consolidate duplicated validation/lifecycle logic where this reduces code without weakening its contracts.
4. Inspect UI failure/recovery, dirty saves, unavailable services, onboarding, privacy controls, updater terminal states and narrow layouts using the existing synthetic browser harness. Capture screenshots and distinguish them from native Windows execution.
5. Research lightweight quality-of-life options. Favor existing-surface improvements, explicit errors, safe retries, bounded data and on-demand work over background features. Document deferred ideas and memory tradeoffs.
6. Run baseline and repaired checks, cross-build the Windows target, package a working candidate and record hashes. Execute native Windows tests only if a suitable runtime exists; otherwise record that validation boundary explicitly.
7. Have Astra review findings and learnings. Transfer only repository-independent, evidence-backed practices to Uika's register, without changing estate governance or deploying rules.

## Verification gates

- Focused tests fail for the original defect and pass after repair where the available platform permits execution.
- Existing browser interactions pass with no uncaught JavaScript errors; evidence uses synthetic data.
- Explicit Linux logic stubs exercise portable decisions and race handling. They do not prove Windows APIs, Credential Manager, Discord pipes or WebView2 integration.
- Windows application and test executables compile with the module's requested Go version. Record toolchain/platform blockers rather than substituting a claimed pass.
- No main-branch writes, release publication, automatic install or estate-wide rollout.

## Planned delivery

`BUILD-ASSESSMENT.md`, `INTERACTION-STATES.md`, `LEARNINGS.md`, verification logs, screenshots, a Windows build candidate and a reviewable feature-branch diff. Remaining native integration and memory measurements stay visible in the assessment.

## Completion record

Implementation is committed as `d03089e`. The candidate passes the extended Linux logic/race checks, all 53 synthetic browser checks and Windows application/test cross-compilation. The updater cleanup and cancellation regressions fail against baseline and pass after repair. Astra accepted the static review and approved seven general learnings for Uika. Remaining work is native Windows/live-provider execution, process-tree memory comparison and review of four unchanged native interop vet diagnostics. The assessment and interaction inventory contain the evidence and recovery boundaries.
