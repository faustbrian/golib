# Specification provenance

`manifest.tsv` pins the OpenID Connect Core 1.0 Section 2 ID-token claims
vector used by the conformance test. The local JSON fixture preserves the
claims from the final specification incorporating errata set 2; its digest and
byte count detect accidental drift.

Provider interoperability tests use minimal standards-compliant metadata
shapes representative of Google, Keycloak, and Dex. They are compatibility
profiles, not copied provider snapshots or claims of certification against a
live provider version.
