# Compatibility

## Go and modules

The supported floor is Go 1.26. The repository contains independent modules:

| Module | Runtime dependency policy |
| --- | --- |
| root | Standard library only |
| `/jwt` | JWX v3 and its owned HTTP resource cache |
| `/oidc` | coreos/go-oidc v3 and go-jose v4 |
| `/authotel` | Stable OpenTelemetry API; SDK only in tests |

CI tests the minimum Go line and stable Go. Optional-module matrices test their
declared dependency versions and the latest compatible minor versions without
changing the root dependency graph.

## API stability

Before v1, incompatible API changes may occur in a minor release and must be
documented with migration guidance. At v1, exported identifiers and behavioral
contracts follow Semantic Versioning. Adding a new failure kind, credential
kind, algorithm requirement, default bound, or stricter parser behavior is a
user-visible change even if Go signatures remain compatible.

API baselines are maintained separately for the root and each optional module.
Generated interfaces from dependencies are not part of this project’s API.

## Protocol compatibility

Basic follows RFC 7617 extraction shape without negotiating a charset. Bearer
header grammar follows RFC 6750 `b64token`. Challenges use safely quoted and
escaped auth parameters. JWT validation requires compact signed JWS and strict
registered claims. OIDC accepts asymmetric algorithms explicitly listed in
configuration and supported by the package.

## Audited dependency lines

The July 2026 audit used Go 1.26.5 with JWX v3.1.1, `httprc` v3.0.5,
coreos/go-oidc v3.20.0, go-jose v4.1.4, and OpenTelemetry v1.44.0. CI's
optional-module matrices test the declared versions and latest compatible
minor versions. This list records the audited baseline; module files and CI
remain the authoritative current inputs.
