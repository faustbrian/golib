# Goal: Application-Oriented Authentication

## Objective

Build a production-grade open-source authentication package for Go services.
The package MUST authenticate credentials and produce a stable principal
without owning service lifecycle, business authorization, users, roles, or
permissions.

## Product Boundary

`authentication` owns authentication. `authorization` consumes authenticated
principals to make access decisions. `service` composes authentication
middleware but MUST NOT implement credential semantics.

The package MUST remain usable with plain `net/http`, JSON-RPC, background
commands, and message consumers. It MUST NOT require a framework, service
container, global registry, or database schema.

## First-Version Scope

### Core Model

- Immutable principal with subject ID, authentication method, issuer, audience,
  tenant hints, scopes, claims, and authentication time.
- Typed credentials and authentication results.
- Stable challenge and failure taxonomy with `errors.Is` and `errors.As`.
- Explicit anonymous state; absence of credentials MUST NOT be represented by a
  partially populated principal.
- Context helpers with collision-safe private keys.

### HTTP Authentication

- Strict Basic, bearer, and API-key extraction.
- Header, query, and cookie credential sources only when explicitly enabled.
- Correct `WWW-Authenticate` challenge construction and escaping.
- Middleware that authenticates but does not authorize.
- Multiple-authenticator composition with deterministic precedence and
  distinction between absent, invalid, unavailable, and rejected credentials.
- Body-independent operation and preservation of optional response interfaces.

### Authenticators

- Constant-time static Basic and API-key validation.
- Callback and interface-based bearer-token validation.
- Credential rotation with multiple active keys and deterministic key IDs.
- Optional JWT/JWK package with strict algorithm, issuer, audience, time, and
  key-selection validation using an audited JWT implementation.
- Optional OIDC validation package built on standards-compliant upstream
  components rather than a new OIDC implementation.
- Service-to-service API-key and bearer-token support.

### Operational Integration

- Secret-safe `log` integration.
- Authentication metrics and traces through optional `telemetry` adapters.
- Deterministic test authenticators, principals, clocks, and HTTP fixtures.
- No credential, token, claim set, or raw authorization header in diagnostics by
  default.

## Non-Goals

- No roles, permissions, ACL, RBAC, ABAC, policy evaluation, or ownership rules.
- No user registration, password reset, session UI, identity directory, or
  account lifecycle.
- No OAuth2 authorization server or token issuer in v1.
- No custom cryptographic algorithms, JWT implementation, or password hashing
  primitive.
- No outbound OAuth2 token acquisition; that remains in `http-client`.

## Package Shape

- Root package: principal, credentials, authenticator contracts, errors.
- `authhttp`: extraction, challenges, and middleware.
- `apikey`: static and callback API-key authenticators.
- `basic`: Basic credential validation.
- `bearer`: opaque bearer-token validation.
- `jwt`: optional JWT/JWK validation.
- `oidc`: optional OIDC validation.
- `authtest`: deterministic fixtures and assertions.

Optional packages MUST keep JWT and OIDC dependency weight out of Basic and
API-key users.

## Required Design Properties

- Authentication is fail-closed unless an explicitly documented optional
  policy permits anonymous access.
- Credential-source precedence is deterministic and duplicate credentials are
  rejected unless a documented composition rule says otherwise.
- Comparisons involving static secrets use constant-time primitives where
  applicable.
- Network-backed validation is context-bounded, cache-bounded, and safe during
  key rotation and issuer failure.
- Principal claims are bounded and copied so caller mutation cannot alter an
  authenticated identity.
- Every background goroutine has explicit ownership, cancellation, and join.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
authentication, rejection, ambiguity, cancellation, rotation, challenge,
redaction, and resource behavior rather than merely execute lines.

Required verification includes:

- table-driven protocol and error tests
- hostile header, token, claim, URL, and configuration fuzzing
- race tests for key rotation, caches, refresh, and concurrent authentication
- real HTTP middleware and cancellation integration tests
- JWT/JWK/OIDC interoperability vectors where those packages are enabled
- clock-skew, issuer outage, stale key, oversized token, and algorithm-confusion
  tests
- allocation and latency benchmarks for common authentication paths

## Documentation Deliverables

- README, five-minute quickstart, and complete exported API documentation.
- Guides for Basic, bearer, API key, JWT/JWK, OIDC, HTTP, JSON-RPC, service
  accounts, credential rotation, and anonymous routes.
- Authentication-versus-authorization boundary guide with `authorization`
  composition examples.
- Security, threat model, operations, troubleshooting, compatibility, migration,
  performance, FAQ, and contribution documentation.
- Runnable examples for every user-facing authentication scenario.
- Maintained `CHANGELOG.md` covering every user-visible change.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, unit and integration tests,
race tests, fuzz smoke tests, exact meaningful coverage enforcement,
vulnerability scanning, examples, docs, API compatibility, and supported Go and
JWT/OIDC dependency matrices. Releases MUST be reproducible and SemVer-governed.

## Execution Plan

1. Specify principal, credential, challenge, error, and composition contracts.
2. Implement Basic, bearer, API-key, HTTP, and deterministic test packages.
3. Implement optional JWT/JWK and OIDC validation packages.
4. Complete hostile-input, race, interoperability, benchmark, and security work.
5. Publish full adoption, operations, migration, and API documentation.

## Acceptance Criteria

- Basic, bearer, and API-key scenarios are complete and standards-correct.
- Optional JWT/JWK and OIDC behavior is strict, interoperable, and fail-closed.
- Authentication composes with `service` and `authorization` without
  dependency cycles.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation enables safe adoption without implementation-source reading.
- `CHANGELOG.md` is complete and current.
