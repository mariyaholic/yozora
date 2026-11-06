# State-machine audit and verification

The November 2026 review covers daemon/panel lifecycle, media watcher reconnects,
source arbitration, Discord publication/rate gating, Discord IPC connections,
Spotify token/poll/snapshot delivery, asynchronous artwork/classification, and
configuration/panel save flows. This inventory is not a claim that every OS or
network transition has been exercised.

## Changes

- Republish an unchanged visible track after a Discord connection dies, while
  respecting the existing send-rate gate and connection retry delay.
- Keep Discord application identity across disconnects and failed dials. A source
  with a different application ID gets its own connection instead of inheriting
  the previous application's retry delay. Clears target the active/last attempted
  application rather than blindly using the default application.
- Republish unchanged visible tracks when Discord presentation settings change.
- Disable closed input channels and drop their obsolete snapshots. Use the step
  clock for idle and Spotify snapshot expiry.
- Replace a queued Spotify snapshot with the newest one, including an empty
  snapshot after a successful no-content response. Protect reported errors from
  concurrent reads, never block shutdown on an unread queue, and discard results
  returned by a request after shutdown. An in-flight HTTP request can still take
  up to its existing ten-second timeout to finish.

## Background Edge / YouTube follow-up

A previously verified YouTube video retains its service, video ID and public
thumbnail when the same continuously selected media item moves to a background
tab and pauses/resumes. The retained evidence is invalidated by a different item,
browser application or observed selection gap, and cannot override a current
foreground match or blocking/pause-hide policy. Classification cache keys now
include the source application to prevent cross-browser reuse.

Cold background-tab detection still has no video URL: it can classify YouTube and
keep an SMTC thumbnail on the local dashboard, but Discord gets the symbol until
a URL has been verified. No background address-bar scraping, tab activation or
thumbnail upload was added. The existing Watching YouTube label is unchanged.
Identical media metadata within the same browser cannot identify distinct videos.

Injected regressions cover unchanged background playback, pause/resume, disabled
or failing tab reads, hide-on-pause, source revocation, gaps, changed titles/apps,
ambiguous current windows and newly verified foreground URLs. Live Edge/SMTC
background behavior is still an outstanding manual Windows check.

This branch contains state-machine fixes only, based on main. The separate
shared-theme pass is shelved and excluded; its review branch is retained. The
background-tab control's explanatory text is updated for the behavior above.

## Reproduce

On real Windows, with the Go version in `go.mod`:

```sh
go test ./... -count=1
go test -race ./internal/presence ./internal/spotifyapi -count=20
go build -trimpath -ldflags "-s -w -H=windowsgui" -o Yozora.exe ./cmd/yozora
```

The `verify` workflow runs these checks for pull requests, with read-only
repository permissions. It never merges, tags, deploys or publishes a release.
The existing release workflow remains unchanged and runs on a push to `main`.

On Linux:

```sh
bash scripts/check-state-linux-shadow.sh
```

This creates a disposable test copy and explicitly stubs SMTC, browser OS calls,
credential storage and named-pipe dial. It removes Windows tags in that copy and
runs six logic packages ten times, then all presence/Spotify tests under the race
detector one hundred times. It does not compile or run an unmodified Linux app,
exercise real credentials, contact Discord or prove Windows compatibility.

## Remaining validation

- Real Windows SMTC, source-specific Discord application switches/reconnects,
  Spotify authorization/204/shutdown, WebView2 and installer behavior need manual
  integration testing. Native CI tests use private test pipes, not live Discord.
- Wine 10.0 could not start the existing Windows config test in the audit
  environment: `free(): invalid pointer`, exit 134. No Wine runtime pass is claimed.
- The documented browser regression suite could not start Chromium in the prior
  theme pass because socket creation was prohibited. Browser/UI validation remains
  outstanding; no permission restriction was bypassed.
- Windows-target `go vet ./...` reports four existing unsafe-pointer diagnostics
  in `internal/credman/credman.go` (two), `internal/smtc/thumbnail_test.go` and
  `internal/wrt/wrt.go`. The unchanged theme base has the same diagnostics. These
  are not suppressed and require a separate review.

## Independent review: live artwork settings

Artwork preference and browser-discovery/source-toggle changes now advance the resolver generation for the unchanged item. Late results from a prior configuration are discarded. Cold tab-title-only classification is re-evaluated on `youtube_tabs` changes; verified video continuity for the same continuously selected item survives disabling background discovery. No background address bar is read and disabled tab scanning stays disabled. Regression tests exercise repeated toggles, stale in-flight artwork and retained verified-video presentation. These are injected Windows-decision tests, not a real Edge/UIA/SMTC runtime claim.
