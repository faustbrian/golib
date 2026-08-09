# Specification conformance matrix

`manifest.tsv` pins every normative source used by the HTTP client decision
register. RFC sources use immutable RFC Editor text. W3C Trace Context uses a
dated Recommendation snapshot instead of the mutable latest-version URL.
SHA-256 digests make source drift explicit.

The module claims conformance only for behavior exposed by its public policy
surface and linked executable evidence. It does not implement an HTTP wire
stack; framing and protocol-version transport are delegated to Go's `net/http`.

Run the focused map and evidence check with:

```console
make conformance
```

For an update, download the exact manifest URL, verify provenance, calculate
`shasum -a 256`, review errata and successor specifications, update affected
decisions and tests, and then update the manifest. A digest change alone MUST
NOT silently change behavior.
