# Specification conformance matrix

`manifest.tsv` pins every normative source used by the HTTP middleware
decision register. RFC sources use immutable RFC Editor text. Fetch, URL, and
Referrer Policy use exact commits from their official standards repositories
because their published specifications are living documents. The Go runtime
contract uses the official Go 1.26.5 source archive. SHA-256 digests make
source drift explicit.

The module claims conformance only for behavior exposed by its public policy
surface and linked executable evidence. It delegates HTTP framing and protocol
transport to Go's `net/http`, does not implement browser enforcement, and does
not claim complete Fetch compliance.

Run the focused map and evidence check with:

```console
make conformance
```

For an update, download the exact manifest URL, verify provenance, calculate
`shasum -a 256`, review errata and successor specifications, update affected
decisions and tests, and then update the manifest. A digest change alone MUST
NOT silently change behavior.
