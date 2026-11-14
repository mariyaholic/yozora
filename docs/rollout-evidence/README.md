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
| Windows pre-publish installed upgrade from published `v0.2.1` | Failed twice: 19:28:19 and 19:33:29, 14 Nov 2026. Both runs stopped at tray-host discovery; candidate replacement was not reached. | [Run 43589962089](https://github.com/mariyaholic/yozora/actions/runs/43589962089), [first record](windows-prepublish-43589962089.json); [run 43590379422](https://github.com/mariyaholic/yozora/actions/runs/43590379422), [diagnostic record](windows-prepublish-43590379422.json). Native package/race/build/installer and Linux logic steps passed both runs; the isolated baseline reported version `0.2.1`. |
| Windows post-publish live updater from published `v0.2.1` | Pending release | Record Actions run URL, job/step, public tag/assets, version transition, result, and completion time. |
| Human `v0.2.1` Setup to `v0.2.2` WebView2 **Update now** check | Pending published `v0.2.2` and disposable Windows test | Record Windows/build context, timestamps, settings before/after, and reported version. |

All times above are Asia/Manila (UTC+08:00). The two hosted Windows failures
are historical observations, not evidence that the candidate replacement path
works. The corrected probe constructs a true null title inside C# rather than
passing PowerShell's converted empty string. A fresh native attempt must pass
before claiming installed-upgrade success.

## Evidence files

The two selected PNGs live under ignored `dist/rollout-ui-evidence/`; the
directory contains no more than four captures and is not tracked. The full
suite result and selected hosted-run failure record are tracked here. Browser
captures show synthetic interface states only; they do not establish a Windows
installer or updater run.
