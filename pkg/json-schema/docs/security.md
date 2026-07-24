# Security and threat model

Schemas and instances are treated as hostile. The design prevents implicit
network access, precision loss, duplicate-member ambiguity, invalid UTF-8,
unbounded owned caches, global extension mutation, path traversal in the
provided filesystem adapter, and credential-bearing resolution diagnostics.
Malformed resource identifiers and ambiguous duplicate resource or anchor
identities fail closed rather than changing resolution by traversal order.
RFC-equivalent identifier aliases collapse to one cache key, preventing case,
default-port, dot-segment, or unreserved-percent spellings from bypassing
resource identity checks.

Owned work is bounded as documented in [limits.md](limits.md). Evaluation
patterns use an ECMAScript-compatible engine so lookaround and backreferences
retain schema semantics. Because those constructs can backtrack, every match
has an explicit stack bound and timeout in addition to pattern count and byte
limits. Timeout errors discard the engine's input-bearing error and return a
typed, redacted `LimitError`. IDNA validation uses registration, lookup
mapping, contextual, bidi, canonical Punycode, and DNS length rules.
Asserted `regex` format values use the same configured byte bound before
compilation.

Compiled schemas are immutable and race-tested. Validation state is local to
one call. Resource and annotation bytes are copied across ownership
boundaries. Exact number comparison never converts arbitrary JSON numbers to
`float64`.
Large `uniqueItems` arrays use canonical hashes with exact equality fallback;
hash collisions cannot change results and remain subject to the comparison
work limit.

Custom keyword, format, loader, filesystem, and JSON marshaling panics are
contained and returned as the redacted `ErrCallbackPanic` classification. A
callback that cancels its context before panicking still returns the context
error, so containment does not mask cancellation.
Callback error causes remain programmatically inspectable, but their error text
is redacted at the package boundary.

Callers remain responsible for:

- policy and authentication in custom loaders, especially HTTP;
- lifecycle and confinement properties of supplied `fs.FS` values;
- cancellation and bounded behavior inside custom callbacks;
- choosing limits appropriate for request size and service concurrency;
- keeping sensitive schema or instance contents out of application logs;
- sandboxing untrusted native plugins, which is outside this Go API.

Report vulnerabilities privately through the repository security channel.
Do not include production schemas, credentials, or customer instances in a
public report.
