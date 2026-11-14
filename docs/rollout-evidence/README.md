# v0.2.2 rollout evidence

This index is for observed rollout evidence only. It distinguishes synthetic
browser assertions, hosted Windows execution, and human Windows observations.
Times are recorded in Asia/Manila (UTC+08:00). Do not infer a passing result
from workflow configuration or a local test double.

## Evidence status

| Check | Result | Evidence |
| --- | --- | --- |
| Local UI regression suite, including English/Japanese manual-check states | Pending final rollout UI source | To be recorded after the rollout UI change is final. |
| Windows pre-publish installed upgrade from published `v0.2.1` | Pending run | Record Actions run URL, job/step, version, result, and completion time. |
| Windows post-publish live updater from published `v0.2.1` | Pending release | Record Actions run URL, job/step, public tag/assets, version transition, result, and completion time. |
| Human `v0.2.1` Setup to `v0.2.2` WebView2 **Update now** check | Pending published `v0.2.2` and disposable Windows test | Record Windows/build context, timestamps, settings before/after, and reported version. |

## Evidence files

Selected browser captures, if produced, are limited to four PNGs and live under
the ignored workspace directory `dist/rollout-ui-evidence/`; they are not
tracked in this folder. Tracked logs or JSON records added here must preserve
the distinction between mock UI/API behavior and real Windows/update behavior.
