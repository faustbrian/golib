# Errata decisions

Pinned source identities are in `sources.lock.json`. Normative RFC prose takes
precedence over examples.

## RFC 8941

- **No errata recorded:** the RFC Editor search returned no matching records at
  the pinned refresh. RFC 8941 parsing algorithms take precedence over its ABNF
  if they disagree, as required by Section 1.2.

## RFC 9421

- **8102, verified editorial:** apply the Section 7.2.8 example correction. The
  final signature-base line is `"@signature-params"`; `@signature-input` is not
  a derived component and is never accepted as an alias.
- **8103, verified technical:** apply both Section 7.5.3 security-prose
  corrections. Verification parses the `@signature-params` value represented
  by the `Signature-Input` dictionary member. The erroneous
  `@signature-input` spelling is not a component name, parser entry point, or
  compatibility alias. This changes the threat-model wording, not the wire
  format already defined normatively in Sections 2.3, 2.5, and 4.1.

## RFC 9530

- **8158, verified editorial:** apply the corrected Figure 14 caption. The
  second field is `Repr-Digest`, not the obsolete `Digest` field.
- **8273, verified editorial:** apply the grammatical correction only; it does
  not change implementation behavior.
- **8890, reported technical:** do not change normative behavior based on an
  unverified erratum. The Brotli bytes in examples B.4 and B.6 are not treated
  as independent conformance vectors. Tests retain both the published bytes
  and the proposed corrected bytes, identify their provenance and errata
  status, and derive digest expectations from the exact selected byte sequence.

## Refresh policy

Any source digest, IANA `Last-Modified` value, erratum status, or registry entry
change requires a reviewed lock refresh, conformance-matrix audit, and explicit
compatibility and security decision before implementation behavior changes.
The pinned HTTPWG corpus is the last revision explicitly targeting RFC 8941
before RFC 9651-only type additions. Run `make spec-sources` to fetch each
authoritative URL, compare its SHA-256 identity, verify the complete errata
inventory, and compare the machine-readable IANA records with their XML
registries.
