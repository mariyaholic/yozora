# Browser evidence

These nine PNGs show the embedded panel rendered by the existing Playwright regression harness. Every track name, artwork image, API response, Discord status, Spotify response and update state is synthetic fixture data. The browser is Linux Chromium; forced-colors is browser emulation. These images are not screenshots of a Windows desktop or a running Yozora daemon.

The full run passed **53 checks** with zero uncaught page errors and zero CSP violations. It exercised panel polling, source reconciliation and save conflict handling, input/reorder paths, startup, appearance, onboarding, Spotify states, update states, diagnostics/recent, Japanese, forced colors, card templates and hidden tracks. The source conflict is asserted in the suite output; this selected set does not contain a separate screenshot of the `412` notice.

The full suite output (108 screenshots and `results.json`) is retained in this workspace under the ignored `dist/ui-evidence/` directory. It is not included in the tracked source tree. Re-run from the repository root with:

```sh
UI_EVIDENCE_DIR=dist/ui-evidence CHROMIUM_PATH=/usr/bin/chromium node scripts/check-ui.cjs
```

The command used for the captured run was the same except its output directory was `docs/evidence/`; those full files were then moved to `dist/ui-evidence/`. Node was v24.19.0, Chromium was 154.0.8037.57 on Debian 13, and Playwright resolved from the preinstalled Codex Node runtime. The full list of all 53 check names is in [browser-results.json](browser-results.json); run provenance, hashes, exact command and selected-file index are in [evidence.json](evidence.json).

| Capture | What it shows |
| --- | --- |
| [playing-560-viewport.png](playing-560-viewport.png) | Playing-state dashboard and source priorities with fixture track “Night Walk.” |
| [paused-560-viewport.png](paused-560-viewport.png) | Paused playback and progress presentation. |
| [save-error-560-viewport.png](save-error-560-viewport.png) | Source save returns fixture HTTP 500; values remain editable and Retry/Discard controls stay available. |
| [phone-320-viewport.png](phone-320-viewport.png) | Responsive panel at 320 px width. |
| [appearance-dialog-560.png](appearance-dialog-560.png) | Appearance dialog with eleven theme choices, motion and language controls. |
| [mouse-drag-560-viewport.png](mouse-drag-560-viewport.png) | Pointer reorder preview and insertion line before release. |
| [forced-colors-appearance-560.png](forced-colors-appearance-560.png) | Appearance selection and focus styling under forced-colors emulation. |
| [card-en-560.png](card-en-560.png) | Full-page card template/settings form with synthetic rendered examples. |
| [hidden-en-560.png](hidden-en-560.png) | Hidden-track marker and card settings for a synthetic track. |

## Build and logic logs

These logs are part of the tracked review evidence:

- [logic.log](logic.log) contains the nine-package portable shadow run, including repeated package and race-detector results.
- [windows-compile.log](windows-compile.log) records the Windows app/test-binary cross-compilation summary. It is compile evidence, not a Windows execution.
- [updater-before.log](updater-before.log) records the two updater regressions failing on the baseline.
- [updater-after.log](updater-after.log) records those regressions passing on the candidate.

For the reviewed capabilities, boundaries and interaction failures/recovery paths, see [BUILD-ASSESSMENT.md](../BUILD-ASSESSMENT.md) and [INTERACTION-STATES.md](../INTERACTION-STATES.md).
