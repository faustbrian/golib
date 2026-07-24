# OpenID Connect

Install the optional module:

```sh
go get github.com/faustbrian/golib/pkg/authentication/oidc
```

Use `oidc.New` for discovery or `oidc.NewWithKeySet` for a supplied upstream
key set. Configuration requires an issuer, client ID, explicit asymmetric
algorithm allow-list, and clock. The validator delegates protocol verification
to coreos/go-oidc, then enforces bounded claims, issued-at skew, authorized
party rules, optional nonce validation, and immutable principal construction.
The configured clock and skew consistently govern `exp`, `nbf`, `iat`, and
`auth_time`.

Discovery and JWK reads have body, time, key-count, redirect, scheme, userinfo,
and fragment restrictions. Key refresh is synchronous: a known cached key can
continue during an issuer outage, while an unknown key fails unavailable. No
background goroutine is started by this module.

`MinRefreshInterval`, `MaxRefreshInterval`, and `MaxRefreshWaiters` bound
network frequency, cache freshness, and concurrent waiting. Responses honor
bounded `Cache-Control`, `Expires`, and `Age` freshness and use `ETag` or
`Last-Modified` validators. Waiter overflow, cancellation, and cooldown after a
failed refresh return unavailable without unbounded work.

Interactive flows should provide a per-flow `NonceValidator`; reject missing or
mismatched nonce values. See `oidc.ExampleNewWithKeySet`.
