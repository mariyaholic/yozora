# v0.2.2 rollout evidence

This index is for observed rollout evidence only. It distinguishes synthetic
browser assertions, hosted Windows execution, and human Windows observations.
Times are recorded in Asia/Manila (UTC+08:00). Do not infer a passing result
from workflow configuration or a local test double.

## Evidence status

| Check | Result | Evidence |
| --- | --- | --- |
| Full Chromium UI regression suite | Passed: 55 checks, no page errors, no CSP violations; revision `12b766f`; 14 Nov 2026, 19:26:36 | [browser-results.json](browser-results.json), [run metadata](rollout-ui.json); Node 24.19.0, Chromium 154.0.8037.57 on Debian 13. Fixture data is synthetic. |
| English/Japanese manual-check captures | Both up-to-date states rendered at 19:29:24, 14 Nov 2026 | Session-local `dist/rollout-ui-evidence/manual-check-en.png` and `manual-check-ja.png`, hashes in [run metadata](rollout-ui.json). Synthetic panel/API fixture, not WebView2. |
| Windows pre-publish installed upgrade from published `v0.2.1` | Passed `0.2.1 → 0.2.2` at 19:39:48, 14 Nov 2026, on final implementation `a0f3451`. | [Run 43590940646](https://github.com/mariyaholic/yozora/actions/runs/43590940646), [success record](windows-prepublish-43590940646.json). Real installed bytes, tray ownership, old-PID exit, replacement, new PID/API version and exact configuration retention passed. Native package/race/build/installer and Linux logic also passed. |
| Earlier Windows fixture diagnosis | Failed at 19:28:19 and 19:33:29, 14 Nov 2026; candidate replacement was not reached. | [First record](windows-prepublish-43589962089.json), [diagnostic record](windows-prepublish-43590379422.json). Corrected PowerShell null-to-empty title conversion in a C# wrapper; retained ownership/restart assertions and added bounded installer-process waiting. |
| Published release and independent downloads | `v0.2.2` published as latest at 19:42:04, 14 Nov 2026; tag targets PR #16 merge `1871ac6`; both public asset checksums passed. | [Release](https://github.com/mariyaholic/yozora/releases/tag/v0.2.2), [metadata and hashes](release-v0.2.2.json). Release Windows gate repeated installed replacement and verified staged bytes before publication. |
| Windows post-publish live updater from published `v0.2.1` | Passed `0.2.1 → 0.2.2` at 19:45:58, 14 Nov 2026. Natural discovery at 19:45:55; actual production download, verification, install, restart and exact settings retention passed. | [Release run 43591163144](https://github.com/mariyaholic/yozora/actions/runs/43591163144), live-update job 132436306176, [live record](windows-live-update-43591163144.json). Fresh authentic baseline; no seeded update state or URL override. |
| Human `v0.2.1` Setup to `v0.2.2` WebView2 **Update now** check | Pending human Windows observation; public target is available. | Record Windows/build context, timestamps, settings before/after, and reported version using [the procedure](../ROLLOUT.md). |

All times above are Asia/Manila (UTC+08:00). The two hosted Windows failures
remain historical evidence. The corrected native smoke passed before PR #16
merged at `1871ac6`. Publication and the separate live updater both passed in
[release run 43591163144](https://github.com/mariyaholic/yozora/actions/runs/43591163144).

## Evidence files

The two selected PNGs live under ignored `dist/rollout-ui-evidence/`; the
directory contains no more than four captures and is not tracked. The full
suite result and selected hosted-run failure record are tracked here. Browser
captures show synthetic interface states only; they do not establish a Windows
installer or updater run.
