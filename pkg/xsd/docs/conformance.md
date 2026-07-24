# Support and conformance

The stable target is XML Schema 1.0 Second Edition plus its published errata.
XML Schema 1.1 is outside the stable scope. Exact source versions and digests
are recorded in `specification/manifest.tsv`.

`specification/requirements/xsd-1.0.tsv` is the support contract. `implemented`
means the row has executable evidence for its stated scope. `partial` means a
useful subset exists but the broad feature is not complete. `missing` means no
support is claimed.

The official XSTS 2007-06-20 suite is pinned by digest. `make xsts` downloads
that exact archive, confines resource access to the extracted suite root, and
runs every accepted valid or invalid expectation. All 24,696 accepted
expectations pass with no failures or skips; 90 upstream `queried`
expectations are reported separately. Every current XML Schema 1.0 support and
quality row is implemented. A regression or newly identified semantic gap
must reopen the affected row instead of weakening its executable evidence.
New support claims require a normative matrix row, focused tests, and
applicable XSTS evidence. The measured result is recorded in the
[XSTS baseline](xsts-baseline.md).

`make differential` runs a shared positive and negative corpus through byte,
incremental-reader, and caller-owned tree validation, then runs the same
corpus through the JDK JAXP XML Schema reference implementation. The Java
reference runs without network access in the digest-pinned Eclipse Temurin 25
container declared by `scripts/run-java-reference.sh`.
