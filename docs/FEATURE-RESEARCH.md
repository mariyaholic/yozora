# Panel feature selection and design sources

Reviewed 14 November 2026 against PR #15 head `819d6aa`. The earlier `QOL-RESEARCH.md` identified presets, settings transfer and one-shot diagnostics as useful additions that can reuse existing surfaces. This increment implements those three options. More providers, overlays, persistent listening history and user-managed preset libraries remain deferred because they require broader runtime and storage budgets.

## Selected opportunities

| User task | Implemented route | Resource approach |
| --- | --- | --- |
| Start with a readable Discord card | Four fixed presets populate the unsaved form and use the current preview/Apply/Discard flow | Generate small metadata only on card requests; add no saved profile database |
| Move supported preferences between installations | Export saved card/source/appearance settings; validate a bounded file, preview and explicitly import with a conditional write | Limit JSON to 64 KiB, use an explicit projection and one config save, retain no server preview session |
| Attach a diagnostic report without clipboard access | Download the same report used by Copy | Fetch on demand; use a bounded Blob helper and fixed safe filenames |

## Primary sources and resulting choices

[RFC 9110 section 13.1.1](https://www.rfc-editor.org/rfc/rfc9110.html#section-13.1.1) specifies strong comparison for `If-Match`. Import therefore checks a strong tag derived from the current supported settings under the existing mutation lock. Requiring one exact preview tag and rejecting wildcard/weak alternatives is this API's application policy. External editors remain outside the panel/tray lock.

[Go's JSON documentation](https://pkg.go.dev/encoding/json) describes duplicate-member and case-insensitive-field behavior. The new settings parser explicitly checks duplicate names and exact required keys in addition to validating existing card/source/appearance types. It does not change all existing application decoders.

[MDN's download property documentation](https://developer.mozilla.org/en-US/docs/Web/API/HTMLAnchorElement/download) distinguishes requesting a download from knowing its completion. Product messages report that the download started. [MDN's object URL guidance](https://developer.mozilla.org/en-US/docs/Web/API/URL/revokeObjectURL_static) supports releasing file references after use. The helper bounds retained Blob URLs and cleans them up on replacement/page exit. Native WebView2 download handling remains a Windows smoke-test item.

## Data and memory qualifications

The backup excludes account connection settings, identifiers and hidden rules. It includes user-written card text, which can itself contain personal information. The file is a personal settings backup; diagnostics use a separate redacted report shape.

The additions introduce no runtime dependency, resident worker, polling loop or unbounded persistent collection. Bounded, on-demand work can still change transient allocations, static binary size and native browser behavior. Whole-process Windows memory parity is unmeasured and must be established using matched baseline/candidate workloads before making a no-increase claim.
