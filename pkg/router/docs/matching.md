# Matching Reference

Patterns use Go 1.26 `ServeMux` path grammar: literals, `{name}`, `{name...}`,
and `{$}`. Path specificity and conflicts are delegated to the standard
library. Hosts add literal or single-label `{name}` matching; exact hosts win
over wildcard hosts, and hostless routes are fallback candidates. Request
ports are removed before matching.

`GET` includes `HEAD` unless an explicit `HEAD` route wins. Explicit `OPTIONS`
wins over automatic OPTIONS. Automatic OPTIONS returns 204; 405 and automatic
responses receive a sorted `Allow`, with `HEAD` implied by `GET`. Unsupported
valid methods return 501 only when no known path establishes 405.

The default follows canonical and subtree redirects from `ServeMux`.
`RejectRedirects` turns those request paths into 404. `{$}` expresses an exact
trailing slash. Encoded slash remains within a wildcard and its decoded value
is available through `Request.PathValue`.

Only `OPTIONS *` supports asterisk-form. CONNECT authority-form is rejected in
v1. Invalid methods, URLs, and authorities receive 400. Matching uses a derived
request only to attach package-owned route context; it does not mutate the
caller's URL or replace parameter access.
