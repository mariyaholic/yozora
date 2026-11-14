# Lightweight quality-of-life research

Reviewed 14 November 2026 against baseline `1c4659f`. These are implementation opportunities, not measured demand or proof of unchanged Windows memory use.

## Selected for this build

| Opportunity | Why it helps | Resource discipline |
| --- | --- | --- |
| Keep the open panel synchronized with tray source changes | Prevent stale controls and accidental overwrites while preserving unsaved edits | Reuse the existing panel refresh, fixed-size source settings and stateless conditional writes |
| Restore keyboard focus after hidden-rule removal | Make repeated keyboard editing usable and predictable | Reuse rendered DOM nodes and existing controls |
| Make Spotify cancel/disconnect final | Prevent a late network response from reconnecting an account the user disconnected | One operation generation and ordered commit; no worker or token history |
| Keep update errors recoverable and clean | Failed downloads should leave no partial installer behind and a busy installer should remain busy | Existing operation and temporary directory; no extra polling service |
| Enforce artwork limits across restarts | Prevent accumulation hidden by per-process bookkeeping and broken preview URLs after eviction | One bounded startup scan; no image prefetch or duplicate cache |

## Viable follow-ups

| Candidate | Small implementation route | Acceptance gate |
| --- | --- | --- |
| Named Discord-card presets | Small bounded set of template values, loaded on demand through the existing card form | Preserve custom edits, validate templates, do not add a persistent process |
| Settings export/import | Explicit user action over a versioned allowlisted settings object, excluding tokens and local identity | Reject unknown/oversized payloads, show preview, atomically save; bounded temporary allocation |
| Explain why a card is not publishing | Derive a short reason from existing paused, hidden, source, Discord and error state | Reasons agree with engine decisions; no new event log or sampler |
| Spotify rate-limit feedback | Reuse request status and the existing polling timer to display a retry time | Respect `Retry-After`, clear stale errors, do not increase request frequency |
| One-shot diagnostics export | Reuse the current redacted diagnostic report and browser download behavior | No track text, token, user path or continuous telemetry |

Spotify's [rate-limit guidance](https://developer.spotify.com/documentation/web-api/concepts/rate-limits) describes HTTP 429 and a retry delay in seconds. That favors explicit cooldown feedback and fewer requests over richer polling. Its [current playback API](https://developer.spotify.com/documentation/web-api/reference/get-information-about-the-users-current-playback) includes no-content outcomes, so an empty player is an ordinary state, not necessarily an error.

Discord's [activity schema](https://github.com/discord/discord-api-docs/blob/main/developers/events/gateway-events.mdx) supports text, timestamps and up to two custom buttons. Presets can use those existing fields. They do not require a richer rendering engine or a new SDK. Provider support and the existing local IPC route must still be tested independently.

## Deferred

Always-on overlays, unlimited history, album-art prefetch, continuous browser scanning, extra cloud services and a general plugin engine would expand the resident resource or maintenance budget. No such feature is introduced in this build.

## Condensing code

Prefer shared strict request decoding, a single authorization commit boundary, a single source-state merge path and existing timers over parallel special cases. Regression tests necessarily add code. Net line count is not a memory measurement and deleting checks to reduce lines would weaken the build.

Go's [memory-limit documentation](https://go.dev/doc/go1.19) explicitly treats the runtime limit as soft and excludes external memory. Before claiming no increase, measure baseline and candidate on the same Windows host with identical flags and workloads. Include daemon and WebView2 process-tree private bytes/working set, Go heap, handles and CPU through idle, playback, pause, reconnect, artwork churn, panel open and panel close. The existing low-water README claim is not that benchmark.
