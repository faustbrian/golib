# authentication

`authentication` is a production-oriented authentication library for Go
services. It turns Basic credentials, opaque bearer tokens, API keys, JWTs, or
OIDC ID tokens into an immutable principal. It does not decide whether that
principal may perform an action.

The root, Basic, bearer, API-key, HTTP, logging, and test packages use only the
Go standard library plus the narrow `clock` capability contracts. JWT,
OIDC, and OpenTelemetry live in separate modules so their larger dependency
graphs are opt-in.

## Requirements

- Go 1.26 or newer.
- `clock` v1 for deterministic time seams.
- `jwt`: lestrrat-go/jwx v3.
- `oidc`: coreos/go-oidc v3 and go-jose v4.
- `authotel`: OpenTelemetry API v1.44.

## Install

```sh
go get github.com/faustbrian/golib/pkg/authentication
```

Add an optional module only when needed:

```sh
go get github.com/faustbrian/golib/pkg/authentication/jwt
go get github.com/faustbrian/golib/pkg/authentication/oidc
go get github.com/faustbrian/golib/pkg/authentication/authotel
```

## Five-minute quickstart

```go
extractor, err := authhttp.NewExtractor(authhttp.BearerAuthorization())
if err != nil {
	return err
}

authenticator, err := bearer.New(bearer.ValidatorFunc(
	func(ctx context.Context, token string) (authentication.Principal, error) {
		if token != configuredToken {
			return authentication.Principal{},
				authentication.NewFailure(authentication.FailureRejected)
		}
		return authentication.NewPrincipal(authentication.PrincipalSpec{
			Subject: "orders-worker",
			Method:  "bearer",
		})
	},
))
if err != nil {
	return err
}

middleware, err := authhttp.NewMiddleware(extractor, authenticator)
if err != nil {
	return err
}

handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	principal, ok := authentication.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication invariant failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, principal.Subject())
}))
```

The middleware is fail-closed. It authenticates and stores the principal, but
it deliberately performs no role, permission, ownership, or policy checks.

## Packages

| Package | Purpose |
| --- | --- |
| root | Immutable principals, typed credentials, failures, composition, instrumentation |
| `basic` | Constant-work static Basic authentication |
| `bearer` | Callback and interface adapters for opaque tokens |
| `apikey` | Callback and atomically rotatable static API keys |
| `authhttp` | Strict extraction, challenges, and authentication-only middleware |
| `authlog` | Secret-safe standard `log/slog` instrumentation |
| `authtest` | Deterministic principals, clocks, authenticators, HTTP fixtures, assertions |
| `jwt` | Optional strict JWT/JWK validation and owned remote cache |
| `oidc` | Optional OIDC discovery and ID-token validation without background refresh |
| `authotel` | Optional OpenTelemetry traces and bounded metrics |

## Security defaults

- Credential values always format as redacted.
- Static secrets are compared through fixed-size SHA-256 digests with
  constant-time comparison.
- Multiple credential sources are rejected as ambiguous.
- Query and cookie credentials are disabled unless explicitly configured.
- Query credential constructors are deprecated for new designs because URLs
  can be retained before the extractor sees them.
- Claims, tokens, keys, HTTP bodies, and cache work have explicit bounds.
- JWT algorithms, issuer, audience, key ID, key metadata, and time claims are
  validated explicitly.
- OIDC uses upstream protocol verification with bounded synchronous JWK
  refresh and stale known-key availability during issuer outages.
- Instrumentation receives only credential kind, outcome, failure kind, and
  duration; it never receives credential or principal contents.

## Documentation

Start with the [quickstart](docs/quickstart.md). Protocol-specific guides are
under [docs/guides](docs/guides), including HTTP, JSON-RPC, service accounts,
credential rotation, and anonymous routes. Operational and compatibility
material is in [docs/operations.md](docs/operations.md),
[docs/troubleshooting.md](docs/troubleshooting.md), and
[docs/compatibility.md](docs/compatibility.md).

For a security review or rollout, use the [adoption checklist](docs/adoption.md),
[threat model](docs/security/threat-model.md),
[findings](docs/security/findings.md), and
[test matrices](docs/security/test-matrices.md).

The authentication-versus-authorization boundary is documented in
[docs/authentication-vs-authorization.md](docs/authentication-vs-authorization.md).
Security reports follow [SECURITY.md](SECURITY.md), and contributions follow
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
