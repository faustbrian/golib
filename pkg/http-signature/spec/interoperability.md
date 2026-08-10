# Interoperability inventory

This module verifies the RFC 9421 Appendix B fixtures locally and runs the
same official fixtures through two independently maintained Go
implementations. Peer source and dependencies are fetched only by the explicit
development gate; the library performs no network access.

| Peer | Pinned revision | Shared evidence | Decision |
|---|---|---|---|
| [`yaronf/httpsign`](https://github.com/yaronf/httpsign) | `de382d35c1add89cc09b9355161d61471fb7f632` | Its request and response verification tests consume RFC 9421 Appendix B signatures. | Compatible fixture results are required. Its optional forwarded-header scheme source is not used here; deployments of this module must supply trusted `ExternalRequestContext`. |
| [`dadrus/httpsig`](https://github.com/dadrus/httpsig) | `0f24bf7dd9b76727af985d9a6f7ce87207a18387` | Its verifier suite consumes RFC 9421 Appendix B signatures for RSA-PSS, ECDSA P-256, HMAC, and Ed25519. | Compatible fixture results are required. The peer documents a deliberate `Accept-Signature` label divergence; this module follows the normative RFC 9421 Section 5.2 requirement and preserves the requested label. |

Run `make interoperability` to fetch the exact commits into disposable
directories, run the selected peer suites with disposable Go build and module
caches, and remove all fetched data. A changed peer revision requires a
reviewed fixture and divergence audit before updating this file and the gate.

The NIST P-384/SHA-384 verification vector is independently pinned in
[`sources.lock.json`](sources.lock.json), because RFC 9421 Appendix B does not
provide a P-384 example.
