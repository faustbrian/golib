# Errata decisions

Pinned source identities are in `sources.lock.json`. Normative RFC prose takes
precedence over examples.

## RFC 9421

- **8102, verified editorial:** apply the correction. The component generated
  from the signature metadata is `@signature-params`; `@signature-input` is not
  a derived component and is never accepted as an alias.

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
Run `make spec-sources` to fetch each authoritative URL and fail if its bytes no
longer match the pinned SHA-256 identity.
