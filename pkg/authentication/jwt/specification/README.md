# Specification provenance

`manifest.tsv` pins the exact RFC 7515 Appendix A.2 compact RS256 JWS and the
RFC 7520 Figure 5 HMAC JWK used by `interoperability_test.go`. The digests and
byte counts apply to the whitespace-free strings embedded in those tests.

The conformance target also runs the supported algorithm/key matrix,
bidirectional golang-jwt interoperability across every shared HMAC, RSA, PSS,
and ECDSA algorithm, deliberately stricter differential cases, adversarial
serialization and claim cases, and remote fault injection.

The canonical
[`docs/specification-decisions.md`](../docs/specification-decisions.md)
register links every observable interpretation and defensive policy to that
executable evidence. This provenance directory pins source material; the
register owns rationale, consequences, and reconsideration conditions.

When an RFC erratum or replacement algorithm specification changes a vector,
retain the previous evidence, add a new versioned row, update the test from the
official source, and recompute the whitespace-free digest and byte count with
`shasum -a 256` and `wc -c`.

The embedded excerpts are manual transcriptions governed by the IETF Trust's
Legal Provisions Relating to IETF Documents. They are retained only as the
minimum interoperability vectors needed by the tests. To update one, copy the
exact compact value or JWK from the linked RFC, remove formatting whitespace,
run `shasum -a 256` and `wc -c` over that exact byte sequence, update its
manifest row, and rerun `make conformance`.
