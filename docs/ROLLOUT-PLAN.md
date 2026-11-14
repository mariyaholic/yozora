# Installed update rollout plan

PR #16 starts from `d5c0a01`. The existing public `v0.2.1` release is the installed test baseline. Its published assets and tag are preserved. The next release from PR #16 is `v0.2.2` under the existing `0.2` series. A same-version replacement would not exercise the updater's newer-version gate.

## Acceptance and scope

- Use actual `Yozora-Setup.exe` release installations. The earlier review ZIPs contain `dev` builds, which intentionally do not check for updates. A standalone portable executable outside the installer directory does not provide the same in-place upgrade contract.
- Add a bounded explicit update-check action for the new version, with authenticated API access, error/retry feedback and serialization against installs. Reuse the existing timer and worker. Existing 0.2.1 installations retain their original scheduling behavior.
- Validate a real installed baseline and candidate on a disposable hosted Windows runner before publishing: exact versions/hashes, installer exit, old process exit, replacement/restart and preservation of settings. Check baseline/new signer compatibility before creating a public latest release.
- Publish only from main through the existing PR/release path. Stage assets in a draft, verify the uploaded files, version and checksums, then publish as latest. Refuse conflicting drafts rather than silently replacing a released version.
- After publication, test actual 0.2.1 discovery and its authenticated install action against the production GitHub release URLs on a fresh hosted Windows runner. Do not seed update results or override production URLs. Verify the new installed version/hash, process handoff and retained configuration.

## Execution

1. Inspect current PR, release and updater state; pin the 0.2.1 baseline assets and preserve them.
2. Implement the check action, release gates, installed-upgrade smoke scripts and release/test instructions on PR #16.
3. Run native/unit/race/browser verification and prepublication installer smoke; have Astra review the candidate.
4. Commit and push the reviewed PR changes, mark PR #16 ready and merge its verified exact head. The user's full-rollout instruction authorizes this release path; no direct main push or tag rewrite is needed.
5. Monitor release build, staged upload verification and public publication, then the real installed-update smoke job. Repair any failure within scope and retain truthful evidence.
6. Independently inspect the latest release metadata and downloaded checksums, and give the user pinned baseline/target installers plus a short one-click test procedure.

## Completion boundaries

Native updater/installer HTTP-action smoke is distinct from manually clicking the update control in WebView2. Published checksums do not replace publisher identity. Unsigned baselines use the existing checksum mode; a signed baseline must retain valid same-publisher compatibility. No claim of interactive UI, live music/provider behavior, or Windows memory parity follows from release automation alone.
