# Security model

The core has no ambient process access, package initialization, production
panic, or unowned goroutine. An executable architecture test enforces those
boundaries and rejects upstream prompt-engine dependencies in the core module.

Secret wrappers redact formatting, JSON, text marshaling, structured logging,
metadata, captured semantic output, and validation failures. Interactive
secret entry requests echo disablement and always attempts restoration and
terminal release. Cleanup failure is returned even when another operation also
failed.

Byte-mode terminal decoding clears consumed decoder and adapter buffers.
Byte-secret execution clears redacting event payloads, grapheme-editor cells,
validation intermediates, and failed result wrappers. It is opt-in because a
normal string paste event has already crossed an immutable string boundary.

Redaction is not access control. `Reveal` is explicit but returns sensitive
data to the caller. Go strings cannot be erased. `SecretBytes.Destroy` and
`FormResult.DestroySecrets` overwrite only owned byte slices; runtime copies,
caller copies, swap, crash dumps, terminals, and hardware remain outside the
guarantee.

Untrusted labels, options, messages, table cells, errors, and event formatting
neutralize terminal and bidi control characters. Input and option work is
bounded. Vulnerability and secret scans are local and CI gates, but no scanner
proves absence of a vulnerability.

Semantic hyperlinks accept only absolute HTTP, HTTPS, and mailto targets,
reject credentials and control or bidi characters, and emit owned OSC 8
controls only under explicit hyperlink capability. Plain fallback prints the
target textually.
