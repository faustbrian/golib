# Security

## Threat model

The router treats route definitions, request targets, authorities, wildcard
values, URL-generation inputs, and metadata as hostile until validated. It
defends against path confusion, encoded-slash structural injection, traversal,
open redirects, authority injection, wildcard ambiguity, method confusion,
route shadowing, metadata disclosure, and resource exhaustion.

It does not authenticate callers, authorize actions, terminate TLS, interpret
forwarding headers, recover application panics, limit bodies, or apply security
headers. Those concerns belong to explicit middleware and server policy.

## Paths and escaping

Patterns use the Go 1.26 `ServeMux` grammar. Prefix composition rejects dot
segments, encoded separators, and wildcard syntax. URL generation escapes each
segment independently. Remainders use explicit segment lists, so neither `/`
nor `..` can become an unintended structural path component.

Redirect rejection examines `URL.EscapedPath`, matching `ServeMux`'s structural
view. A percent-encoded slash or dot sequence captured by one wildcard is not
reinterpreted as a separator or traversal instruction.

## Hosts and proxies

Route authorities are validated at compile time. Request matching uses Go's
authority parsing, including bounded ASCII input, numeric port validation, and
port removal for host patterns. Malformed brackets, user information, control
characters, path separators, and non-ASCII authorities are rejected before
matching. Absolute URLs require a caller-provided trusted base; forwarding
headers are never consulted. Applications behind proxies must validate their
proxy boundary before choosing that base.

Unicode host names are not normalized implicitly. Supply an ASCII IDNA form
chosen by application policy. This avoids silent normalization and look-alike
policy changes in the routing core.

## Disclosure and bounds

Default errors expose no patterns, names, metadata, handlers, or parameter
values. Error diagnostics contain bounded source labels and pattern summaries
only. Diagnostics normalize invalid UTF-8, replace control characters, remain
single-line, and cannot split a UTF-8 encoding at a byte limit. Input is
truncated before normalization and sanitization, so diagnostic work is bounded
by output size. The router never logs. Introspection excludes handler pointers
and function names. All
caller-controlled collections and output sizes are bounded.

Dispatch rejects oversized method tokens before token scanning and oversized
raw or escaped request targets before route matching. Middleware identifiers,
exclusions, group names, mount prefixes, schemes, and generation route names
are length-checked before parsing, normalization, or map lookup.

The production package starts no goroutines and never registers handlers on
the process-global `http.DefaultServeMux`. The blocking safety gate rejects
both mechanisms, along with unsafe, cgo, linkname, initialization hooks, and
reflection-driven discovery.

Report vulnerabilities according to [SECURITY.md](../SECURITY.md).
