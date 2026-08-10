# Specification provenance

`manifest.tsv` pins the OpenID Connect Core 1.0 Section 2 ID-token claims
vector used by the conformance test. The local JSON fixture preserves the
claims from the final specification incorporating errata set 2; its digest and
byte count detect accidental drift.

The conformance target runs the package's complete specification-derived
discovery, metadata, JOSE, ID-token, audience, nonce, time, error, and option
matrices in addition to that pinned example. The OpenID Foundation conformance
suite was reviewed at revision `6b8b809dd07df6ca8b4481a9e921bf48b9ffbffe`
on 2026-08-09. Its OpenID Connect RP profiles require a complete HTTP relying
party to perform authorization redirects, callbacks, token exchange, and
optional UserInfo processing. Those profiles are not directly runnable or
certifiable against this discovery/JWKS/ID-token-validation package alone; a
wrapper RP result would be composite evidence, not package-only conformance.

Provider interoperability tests use minimal standards-compliant metadata
shapes representative of Google, Keycloak, and Dex. They are compatibility
profiles, not copied provider snapshots or claims of certification against a
live provider version.

The interoperability gate also starts Keycloak 26.3.2 from the immutable OCI
image digest pinned in `.golib/versions.env`, imports the checksummed realm
fixture, obtains an ephemeral provider-issued ID token, and validates it against
that instance's real discovery document and JWKS. The token is deleted with the
task-owned temporary directory and is not an interoperability fixture.

The dated Google metadata fixture was fetched from Google's public discovery
endpoint on 2026-08-09. It is immutable interoperability evidence for that
observed provider document, not a claim about Google's current or certified
state after that date.

## Fixture maintenance

- `oidc-core-section-2.json` is a minimal manual transcription of the claims in
  OpenID Connect Core 1.0 Section 2 and is governed by the OpenID Foundation's
  specification copyright and IPR policy. Update it only when the pinned final
  specification changes, then validate it with `jq -e .` and refresh the digest
  and byte count in `manifest.tsv` with `shasum -a 256` and `wc -c`.
- `google-openid-configuration-2026-08-09.json` contains factual metadata
  published by the provider; no source-code license is asserted. Re-fetch a
  newly dated snapshot with `curl --fail --silent --show-error --output FILE
  https://accounts.google.com/.well-known/openid-configuration`, retain the old
  dated snapshot, then validate and record its digest and byte count as above.
- `keycloak-26.3.2-realm.json` is repository-authored test configuration under
  this repository's license. Update it by editing the JSON for the newly pinned
  Keycloak image, run `jq -e .`, refresh its manifest digest and byte count, and
  run `scripts/test-oidc-keycloak-interoperability.sh` to prove the imported
  realm still issues a token accepted through real discovery and JWKS.
