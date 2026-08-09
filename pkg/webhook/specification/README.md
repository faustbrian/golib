# Specification sources

`manifest.tsv` pins every external contract used to classify `webhook`
behavior. The generic `v1` signature grammar itself is a local protocol defined
in [`../docs/signatures.md`](../docs/signatures.md); it is not an implementation
of RFC 9421 or a vendor webhook scheme.

The Go source archive fixes the exact standard-library behavior on which URL,
HTTP, encoding, time, address, and cryptographic operations rely. RFC and W3C
rows are immutable published versions. The IANA rows deliberately pin dated
registry snapshots: a changed registry digest requires review of the default
SSRF address policy rather than silently changing accepted endpoints.

Run `make conformance` to validate the manifest structure, decision register,
local interoperability fixture, and executable decision evidence. Source
digest integrity is checked by repository conformance automation. Updating a
source requires reviewing every affected decision, fixture, public contract,
compatibility statement, and changelog entry.
