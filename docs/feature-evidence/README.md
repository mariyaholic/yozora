# Feature evidence

**Review date:** 14 November 2026  
**Branch:** `feat/panel-presets-backup`  
**Implementation baseline:** `819d6aa`  
**Captured revision:** `d3de539f997252700c6f2614e98d0f121b5c2a77`

This folder holds four selected browser screenshots, the complete UI result record, and the local Go/build logs for the feature review. The pre-existing baseline captures and results under [`docs/evidence/`](../evidence/README.md) were left unchanged.

## Browser UI run

The final run passed **55 checks**, with no page errors and no Content Security Policy violations. Two named checks cover the new flows in English and Japanese; other checks cover the existing panel. The complete check-name list and run metadata are in [results.json](results.json). Its manifest includes SHA-256 and byte counts for all 112 generated PNGs, the four selected screenshots, primary source files, and supporting logs.

Run from the repository root with the same local fixture environment:

```sh
NODE_PATH=/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules \
CHROMIUM_PATH=/usr/bin/chromium \
UI_EVIDENCE_DIR=/workspace/yozora-features/dist/feature-ui-evidence \
node scripts/check-ui.cjs
```

The run used Node v24.19.0 and Chromium 154.0.8037.57 on Debian GNU/Linux 13. Playwright serves the embedded panel from a local synthetic HTTP fixture. Track names, artwork, API responses, Discord state, and diagnostics are fixture data. It does not connect to the daemon, Discord, Spotify, or artwork services, and it is not a Windows desktop capture.

The complete 112-image run remains in the ignored workspace directory `dist/feature-ui-evidence/`; only the four captures below are tracked here. They show rendered form/review states. The assertions and result record establish the tested download, cancellation, conflict, and recovery paths; no screenshot represents a real file transfer or server process.

| Capture | What it shows |
| --- | --- |
| [preset-draft-en-320.png](preset-draft-en-320.png) | Compact preset loaded into the English card form; it remains an unsaved draft. |
| [preset-draft-ja-320.png](preset-draft-ja-320.png) | Same preset draft with Japanese labels. |
| [backup-review-en-320.png](backup-review-en-320.png) | English import review with change summary and candidate JSON before Apply. |
| [backup-review-ja-320.png](backup-review-ja-320.png) | Same review state with Japanese labels. |

The UI fixture covers malformed JSON, a file over 64 KiB, unsupported version, preview errors/retry, delayed local file reads, cancel/reselect before upload, aborting an in-flight preview, timed-out response recovery, stale ETag conflict, import errors, preserved dirty form values, and repeated downloads with Blob URL cleanup. These are browser/client assertions against a local fake API, not proof of filesystem, handler, or Windows runtime behavior.

In the Chromium fixture, a simulated tap immediately after a drag sometimes produced no click. The existing touch assertions were retained; the fixture inserts a 500 ms separation between those simulated gestures after baseline/current comparison and repeated targeted runs. The production drag implementation is unchanged.

## Go and Windows-target evidence

- [go-logic.log](go-logic.log) records the portable Linux shadow run: nine packages repeated ten times, presence/Spotify race checks repeated 100 times, and config/server/update race checks repeated ten times. The substitutions and limits of the shadow environment remain those described in [BUILD-ASSESSMENT.md](../BUILD-ASSESSMENT.md).
- [windows-compile.log](windows-compile.log) is the Windows-target application/test-binary cross-compilation summary. The app and 17 package test binaries compiled; this workspace did not execute those Windows binaries.
- [go-vet-candidate.log](go-vet-candidate.log) and [go-vet-baseline.log](go-vet-baseline.log) both contain four Windows unsafe-pointer diagnostics: two in `internal/credman/credman.go`, one in `internal/wrt/wrt.go`, and one in `internal/smtc/thumbnail_test.go`. Both commands exit nonzero; diagnostic order differs. No claim is made that vet passed.

[Native CI receipt](native-ci.json) records successful hosted Windows unit/race/build and Linux jobs in [run 43584211089](https://github.com/mariyaholic/yozora/actions/runs/43584211089) on `d3de539`. [Selected native execution lines](native-windows-summary.log) support that receipt; the full log remains available through the run URL. These two files were added after the browser hash manifest and are separate evidence records. Native unit execution does not establish interactive GUI downloads or live provider behavior.

The 64 KiB settings cap limits the document payload and retained download Blob. It is not a whole-process memory limit. Whole-process memory parity, RAM/CPU measurements, real WebView2 and Windows panel behavior, live media arbitration, credential storage, Discord/Spotify outcomes, and installer behavior are not established by these captures or logs.
