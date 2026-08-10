# Specification conformance matrix

`manifest.tsv` pins the normative sources used by the router decision register.
RFC sources use immutable RFC Editor text. The matcher and request contract use
the official Go 1.26.5 source archive. SHA-256 digests make source drift
explicit.

The module claims only its documented routing and URL-generation surface. HTTP
framing remains delegated to Go's server, and deliberate ServeMux differences
remain visible in the decision register and differential tests.

The canonical
[`docs/specification-decisions.md`](../docs/specification-decisions.md)
records every material interpretation, consequence, and condition for
reconsideration behind this conformance matrix.

Run the focused map and evidence check with `make conformance`. For an update,
verify provenance and digest, review Go routing changes and RFC errata, update
decisions and tests, then change the manifest. A digest change alone MUST NOT
silently change behavior.
