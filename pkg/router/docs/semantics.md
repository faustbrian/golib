# Routing Semantics

This document freezes the v1 behavior contract before implementation. Terms
such as MUST and SHOULD are used as described by RFC 2119 and RFC 8174.

## Registration and compilation

Registration is explicit and single-owner. Builders accept route descriptors
until compilation succeeds. A successful compile freezes the builder; later
registration and compilation return a typed compile-state error. A failed
compile does not freeze or partially publish a router, so the caller may append
new descriptors and compile again. Registered descriptors cannot be removed or
replaced; rebuild to repair an existing invalid or conflicting descriptor.
The complete standard-library pattern set is validated before any middleware
constructor runs, so a conflict failure cannot partially construct the handler
graph.

Descriptors are copied at registration, and option collections are copied
after all construction options have established the final limits. Compiled
route tables, metadata, middleware identifiers, names, methods, and parameter
lists are returned as copies. Handler and middleware lifecycle remains
caller-owned.

All limits are checked before publication. Defaults bound routes, groups,
nesting, methods, wildcards, pattern and name bytes, metadata, middleware,
parameters, query values, and generated URL bytes. Invalid configuration never
needs a recovery handler at request time.

CONNECT is rejected during registration because v1 does not dispatch
authority-form targets. Exact default budgets are listed in
[Resource Limits](limits.md).

## Patterns and precedence

The matching core is Go 1.26 `http.ServeMux`. Supported path syntax is its
literal, `{name}`, `{name...}`, and `{$}` grammar. Method and optional host are
composed into a standard-library pattern. Registration order never resolves a
conflict; `ServeMux` specificity and conflict rules do.

Route methods are explicit uppercase RFC 9110 tokens. A `GET` route also
matches `HEAD` unless an explicit `HEAD` route is more specific. A handler sees
path values through `Request.PathValue`. The router does not provide a second
mandatory parameter store.

The default policy follows `ServeMux` canonical-path and subtree redirects.
An exact trailing slash is expressed with `{$}`. Redirects are relative and
never use forwarding headers. Encoded slashes stay within one wildcard value
because matching and redirect rejection both use the request's escaped path
semantics. Encoded separators or dot text inside one wildcard are data, not
structural canonicalization input.
Rejected subtree roots are matched with standard-library patterns, so literal,
Unicode, percent-escaped, wildcard, remainder, `{$}`, GET, and implied HEAD
semantics remain aligned.

## Dispatch outcomes

An explicit route wins for its method, including explicit `OPTIONS`. Otherwise
automatic `OPTIONS` responds with status 204 and a sorted `Allow` value when a
route matches the authority and path. A known path with another method responds
405. A syntactically invalid method or request target responds 400. A method
that is valid but unsupported by the compiled table responds 501 only when no
known path match requires 405. A request target over the configured byte budget
responds 414 before matching. All other misses respond 404.

`Allow` is deterministic, deduplicated, bounded, includes `HEAD` whenever
`GET` is allowed, and includes automatic `OPTIONS`. Default error bodies are
minimal and contain no route inventory. With automatic OPTIONS disabled, the
default 404 and 405 responses match `ServeMux`, including body and `Allow`.

The asterisk-form target is supported only for `OPTIONS *`; disabling automatic
OPTIONS sends it to the explicit not-found handler. CONNECT authority form is
rejected in v1. Origin-form and absolute-form requests are accepted only when
`net/http` supplies a valid URL and authority.

## Groups and middleware

Groups flatten at registration. Hosts must be identical when more than one
layer supplies one. Path prefixes join without `path.Clean`; empty segments,
dot segments, encoded separators, wildcards in prefixes, and missing leading
slashes are rejected. Name prefixes concatenate literally. Metadata collisions
are rejected instead of silently overriding values.

Middleware executes router-wide, outer group, inner group, then route order on
the request path; response unwinding is the reverse. Named inherited
router-wide or group middleware may be excluded only by an explicit route
descriptor. Nil and duplicate resolved middleware are registration errors.

Middleware is the ordinary `func(http.Handler) http.Handler` shape. The router
does not recover handler or middleware panics and does not wrap the response
writer, preserving optional interfaces implemented by the original writer.

## Mounts

Mounts are explicit routes at a host and path boundary. Strip-prefix behavior
is an option and operates on a cloned request and URL; the caller's request URL
is not mutated. Encoded literal prefixes are compared in decoded path space,
while the escaped suffix remains in `URL.RawPath`. The original request target
remains in `RequestURI`. Mounting does not imply authentication, authorization,
middleware, or lifecycle ownership. A compiled router is mounted as an
ordinary `http.Handler`.
Nested compiled routers preserve non-conflicting outer `Request.PathValue`
entries; the innermost route wins when a wildcard name is reused.

## URL generation

Named route parameters are supplied explicitly. Every wildcard is required
exactly once; unknown, duplicate, and unused values fail. Segment values are
escaped with `url.PathEscape`. Remainder wildcards accept a non-empty list of
segments, never an interpolated raw path. Query encoding follows `url.Values`
and is deterministic.

Absolute generation accepts an explicit trusted scheme and authority. It does
not inspect requests or forwarding headers. Schemes are limited to `http` and
`https`; authorities are validated, contain no user information or control
characters, and must satisfy the configured trusted-host policy.
