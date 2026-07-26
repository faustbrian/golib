# FAQ

## Does this package authorize requests?

No. It establishes an identity. Pass the principal to `authorization` or
application policy.

## Does it manage users or passwords?

No. Static Basic is for configured credentials; user password hashing,
registration, reset, and directory lifecycle are out of scope.

## Why are query and cookie credentials disabled by default?

They have different leakage and CSRF properties and should never appear by
accident. Query constructors remain for compatibility but are deprecated for
new designs: a URL may already have reached browser history, proxies, and
access logs before extraction. Prefer a header. Enable a named cookie source
only when its CSRF and transport policy is explicit.

## Why reject duplicate credentials instead of choosing the first?

Different proxies and components can choose different values. Rejecting
ambiguity prevents credential smuggling and keeps precedence deterministic.

## Can optional routes ignore a bad credential?

No. Optional means anonymous only when absent. A supplied credential must be
valid and accepted.

## Are scopes permissions?

No. They are credential assertions. Authorization decides their meaning for a
specific operation and resource.

## Does OIDC start refresh goroutines?

No. It refreshes synchronously for unknown keys and caches successful sets.
The JWT remote provider does own cache goroutines and must be closed.

## When does OIDC discover a rotated key?

After bounded response freshness and the minimum refresh interval permit a
network attempt. One caller owns that refresh; other callers wait within a
fixed capacity and can cancel. Known cached keys remain usable during a
transient issuer outage, while an unknown key fails unavailable. Coordinate
issuer cache headers with the rotation overlap.

## Can logs or traces include subjects?

The provided adapters intentionally do not. Applications may add bounded
identity attributes after a privacy and cardinality review, but never tokens,
claims, headers, query strings, or cookies.
