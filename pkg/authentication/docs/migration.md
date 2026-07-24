# Migration

## From ad hoc middleware

1. Inventory accepted credential locations and disable accidental query or
   cookie support.
2. Move parsing to `authhttp.Extractor`.
3. Move verification to a protocol authenticator.
4. Replace user or session maps in context with `Principal`.
5. Move role and permission checks to authorization after authentication.
6. Map stable failures to the existing client contract without exposing causes.
7. Add optional anonymous policy only to routes that were intentionally public.

## From primitive identities

Replace string user IDs with `PrincipalSpec`. Use subject for stable identity,
method for the authentication mechanism, issuer and audience for trust
context, and authenticated-at for credential age. Copy only bounded claims
needed by downstream policy. Do not place mutable user records in claims.

## From another JWT or OIDC library

Declare exact issuer, audience/client ID, algorithms, clock, skew, key source,
and bounds. Test existing tokens for required `sub`, `iss`, `aud`, `iat`, and
`exp`; reject tokens relying on implicit algorithm selection or missing key IDs.
For OIDC multi-audience tokens, ensure `azp` names the client. Add nonce
validation for interactive flows.

Before rollout, run old and new validation against a sanitized interoperability
corpus, compare only classifications and principals, then canary unavailable
and rejected rates.
