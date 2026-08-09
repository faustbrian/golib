# Specification conformance matrix

`manifest.tsv` pins the normative sources used by the wire decision register.
RFC sources use immutable RFC Editor text; W3C publications use dated URLs;
repository-hosted specifications use immutable source commits; CTAP uses the
dated FIDO Alliance publication PDF. The published BSON 1.1 page is versioned
by its content digest. SHA-256 digests make source drift explicit.

The module claims only the documented package profile for each format. A
format specification describes its wire model; Go target conversion,
dependency behavior, finite service limits, and package error classification
remain separate decisions. Passing a dependency's tests is not evidence that
the wrapper implements every optional feature of a format.

Run the focused map and evidence check with `make conformance`. For an update,
verify provenance and digest, review errata and codec changes, update decisions
and behavioral tests, then change the manifest. A digest change alone MUST NOT
silently change behavior.
