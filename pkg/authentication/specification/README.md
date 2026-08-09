# Specification provenance

`manifest.tsv` pins the exact RFC 7617 and RFC 6750 interoperability vectors
executed by `authhttp/interoperability_test.go`. Digests and byte counts apply
to the encoded credential or token string exactly as embedded in the test.

The decision register maps normative and package-owned behavior to focused
tests, hostile-input fuzzing, and security matrices. The manifest does not
claim that three positive vectors replace the complete RFC requirements.

When an RFC erratum or replacement changes a vector, retain the previous
evidence, add a versioned row, copy the exact value from the authoritative
source, and recompute its digest and byte count with `shasum -a 256` and
`wc -c`. Update the decision register and executable evidence in the same
change.

The embedded excerpts are retained only as the minimum interoperability values
needed by the tests and are governed by the IETF Trust's Legal Provisions
Relating to IETF Documents.
