# JWT and remote JWK validation

Install the optional module:

```sh
go get github.com/faustbrian/golib/pkg/authentication/jwt
```

`jwt.New` requires one issuer, one audience, an explicit algorithm allow-list,
a clock, and exactly one static `jwk.Set` or `KeyProvider`. Compact tokens must
contain a permitted `alg`, non-empty `kid`, signature, subject, issuer,
audience, issued-at time, and expiration. Duplicate JSON members, unsupported
critical headers, excessive claims or nesting, unknown keys, and algorithm/JWK
metadata mismatches are rejected.

`jwt.NewRemote` owns its JWX cache. Supply a lifecycle context, HTTPS JWK URL,
bounded refresh intervals and response size as needed, and always call
`Remote.Close` during shutdown. `Refresh` is synchronous and preserves the
previous cached set on failure. Plain HTTP requires the explicit development
option.

See `jwt.ExampleNew`. Production services should normally use asymmetric keys;
the example uses HMAC only to remain compact and self-contained.
