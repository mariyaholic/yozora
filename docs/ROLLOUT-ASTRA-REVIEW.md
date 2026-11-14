# PR16 rollout static review

Reviewed the current rollout diff on 2026-11-14: updater service/API/UI, `scripts/check-installed-upgrade.ps1`, and the release/verify workflows. This is a static review, not evidence that Windows installation or live updating has passed. Native CI is still required.

## Acceptance status

Static acceptance: no remaining blocking findings in the reviewed diff. The safeguards below have been applied and re-reviewed. Native CI success remains a release gate, not an assumption. The approach is acceptable for Setup-installed Yozora 0.2.1 upgrading to the next patch. Do not overwrite or retag published 0.2.1. The draft-first release sequence correctly separates validation of uploaded bytes from making the release latest.

The prepublication test runs the real baseline executable installed by its published installer, then invokes the candidate installer locally. It does not claim to exercise live update discovery. The separate postpublication job runs the authentic baseline's automatic check and authenticated install endpoint against the real release. The distinction is accurate.

## Safeguards applied and re-reviewed

1. The smoke script now requires `GITHUB_ACTIONS=true`, `RUNNER_ENVIRONMENT=github-hosted`, `RUNNER_OS=Windows`, and nonempty `RUNNER_TEMP` before mutation; it also rejects an already running Yozora process. This script must not run on a user's desktop or persistent self-hosted runner. Temporary app/config paths do not isolate Inno's global fallback `taskkill /IM Yozora.exe`, the tray class, singleton mutex or HKCU installer state.
2. Native smoke steps and jobs use `timeout-minutes`; network downloads and HTTP requests are also bounded. Both Setup launches wait only for the installer process with a 180-second deadline. PowerShell's process-tree wait would also await the daemon that Setup intentionally restarts, so the fixture uses `Process.WaitForExit` and retains explicit exit-code and restart assertions.
3. Signature gates now check baseline, candidate executable and installer. Any signature state other than Valid or NotSigned fails closed. If any is validly signed, candidate executable and installer must both have matching valid subjects; a signed baseline also requires the same subject. This covers unsigned-to-signed transitions as well as existing signer continuity.

## Reviewed correctness

- Actual published baseline Setup/exe/checksums are downloaded and validated; baseline install bytes are compared with the published executable.
- The executable's effective version is read through its running `/api/update` endpoint. This is stronger than binary string search and avoids GUI-subsystem stdout capture pitfalls.
- `dashboard.json` is discovered in the test's isolated data path, its PID is mapped to the installed executable, and the session token travels only in an HTTP header. The script does not print endpoint JSON or token-bearing URLs. The hidden parent console aims to prevent the daemon's console dashboard URL from entering CI output; verify that in the first native run.
- The tray is explicitly awaited before upgrading, so Setup's restart decision is exercised. Tests require old PID exit, matching installed candidate bytes, a newly running `serve` process at the install path, and a new endpoint reporting the intended version.
- Exact config-file hash plus a non-default fixture value checks settings preservation. The fixture avoids Spotify account credentials and automatic update traffic in the prepublication test.
- The live phase waits for actual automatic discovery in a fresh profile, then sends the same authenticated POST used by Update now. It does not inject `update.json`, change updater URLs, or substitute a test build for the baseline.
- PR verify remains nonpublishing. Release is guarded to `main`, selects a monotonically increasing patch, runs the installed upgrade/signing check before publication, uploads a draft, downloads and compares the exact three assets, and only then publishes as latest. Failed drafts block an accidental new release until reviewed.
- The manual-check route requires POST and authentication, preserves disabled/dev behavior, bounds checks by a deadline, and does not install. Service locking prevents concurrent checks/installs and avoids the former unlocked available-version selection. The scheduler is notified after a manual check so its next check follows the new success/failure timestamp.
- The panel prevents duplicate check/install actions, ignores stale polls after a newer action, exposes check errors, and reveals a previously dismissed update after an explicit successful check. Old 0.2.1 binaries cannot gain this new button before upgrading.

## Execution evidence

Before merge, final implementation `a0f3451` passed [native verify run 43590940646](https://github.com/mariyaholic/yozora/actions/runs/43590940646), including real baseline install, candidate replacement/restart, running version, byte identity and exact config preservation. Relevant updater/API/race tests and the 55-check browser suite passed. Astra accepted the final bounded installer-wait helper and approved merging the verified head.

After merge, [release run 43591163144](https://github.com/mariyaholic/yozora/actions/runs/43591163144) passed the applicable signature/checksum gates, verified staged assets and published `v0.2.2` targeting `1871ac6` as latest. Independent public downloads matched all published hashes. Its separate live-update job passed authentic `0.2.1 → 0.2.2` natural discovery, real download/install/restart and exact settings preservation at 19:45:58 Asia/Manila on 14 November 2026.

The CI smoke proves the native HTTP action and installer handoff. It is not a native WebView mouse/rendering test; browser UI checks provide separate button coverage. Native execution may reveal Windows runner or Inno environment assumptions and must be treated as authoritative.

## User-facing limits

An existing 0.2.1 installation can take up to the daily interval to notice the new release; restarting preserves its remembered check time. A fresh profile checks after approximately two minutes plus up to 90 seconds of jitter. A dev zip never checks, and a standalone portable executable outside the installer directory is not guaranteed to be replaced in place. Present the rollout as an update for Setup-installed users.
