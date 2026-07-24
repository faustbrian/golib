# Troubleshooting

## Every request returns credentials absent

Confirm the intended source was explicitly added. Query and cookie sources are
off by default. Header names are canonicalized by `net/http`, while query and
cookie names are exact.

## Requests are ambiguous

Inspect proxies and clients for repeated Authorization fields, duplicate query
parameters or cookies, partial API-key pairs, or the same credential in two
enabled sources. Ambiguity is rejected intentionally; do not choose one value.

## A callback returns unavailable instead of rejected

Return `authentication.NewFailure(FailureRejected)` for a well-formed unknown
credential. Unclassified callback errors are treated as dependency failures
and wrapped as unavailable.

## JWTs are rejected

Check compact structure, token size, duplicate JSON fields, exact `alg`
allow-list, `kid`, JWK `alg` and `use`, issuer, audience, required claims,
expiry, issued-at, not-before, skew, and key rotation state. Error messages are
deliberately secret-safe and do not echo the token.

## OIDC discovery or validation is unavailable

Check exact issuer equality, HTTPS, discovery timeout, redirect attempts, body
size, `jwks_uri`, JWK status and metadata, context cancellation, and key count.
Known cached keys can survive an outage; an unknown key cannot.

## Optional routes reject malformed credentials

This is expected. Optional policy permits only absence. Remove the malformed
credential or make the client authenticate successfully.

## Coverage falls below 100%

Run `./scripts/check-coverage.sh` to identify the module and function. Add a
behavioral test or remove a branch that is proven unreachable by an upstream
contract; do not exclude production files or round the percentage.
