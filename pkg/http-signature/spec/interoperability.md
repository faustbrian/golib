# Interoperability inventory

This module verifies the RFC 9421 Appendix B fixtures locally, runs the pinned
official-vector suites of two independently maintained Go implementations, and
executes a checked-in differential corpus across the local implementation and
the peers. Peer source and dependencies are fetched only by the explicit
development gate; the library performs no network access.

| Peer | Pinned revision | Shared evidence | Decision |
|---|---|---|---|
| [`yaronf/httpsign`](https://github.com/yaronf/httpsign) | `de382d35c1add89cc09b9355161d61471fb7f632` | Its request and response tests consume RFC 9421 Appendix B signatures. The shared corpus compares three request component bases and authenticates each reconstructed peer base with the peer-emitted HMAC. | Compatible fixture and shared-corpus results are required. Its optional forwarded-header scheme source is not used here; deployments of this module must supply trusted `ExternalRequestContext`. |
| [`dadrus/httpsig`](https://github.com/dadrus/httpsig) | `0f24bf7dd9b76727af985d9a6f7ce87207a18387` | Its verifier suite consumes RFC 9421 Appendix B signatures for RSA-PSS, ECDSA P-256, HMAC, and Ed25519. The shared corpus compares the same three request component bases and authenticates each reconstructed peer base with the peer-emitted HMAC. | Compatible fixture and shared-corpus results are required, subject only to the negotiation-label exception below. |
| [`shogo82148/go-sfv`](https://github.com/shogo82148/go-sfv) | `v0.3.3` | The shared corpus parses and serializes valid and malformed Structured Fields through both implementations. | Common valid values must have identical canonical forms, common malformed values must be rejected, and every intentional semantic divergence is recorded in the corpus and its README. |

The peers do not export their generated signature bases. The differential
harness therefore parses each peer's emitted `Signature-Input`, reconstructs
the complete base locally, compares all covered-component lines with the fixed
corpus, and proves that the peer's HMAC authenticates those exact bytes. This
is stronger than running peer self-tests or accepting peer output with the
same peer, but it does not claim raw internal-base access. The complete corpus,
Structured Fields decisions, pinned module versions, and execution boundary
are documented in
[`differential/shared-corpus/README.md`](../differential/shared-corpus/README.md).

## Interoperability exception

| Peer | Exact divergence | Specification analysis | Security impact | Chosen behavior |
|---|---|---|---|---|
| `dadrus/httpsig` at `0f24bf7dd9b76727af985d9a6f7ce87207a18387` | The peer emits a configured dictionary label in `Accept-Signature`, but its verifier does not require a returned signature to retain that label. It selects applicable returned signatures by the signed `tag` and configured expectations, so an otherwise conforming `Signature-Input` and `Signature` pair can verify under a different label. The peer documents this as deliberate. | RFC 9421 Section 5.2 step 2.1 makes the request member key the output label, and the section's result requires a fulfilled signature to have that same label. Section 7.2.5 separately allows an intermediary to relabel an attached signature and warns applications not to assign security semantics to unsigned labels; it recommends a signed `tag` for application meaning. That processing rule does not remove the Section 5.2 fulfillment requirement. | Label equality is not cryptographic authentication because the label is outside the signature base. Ignoring it can nevertheless miscorrelate multiple requested members or silently treat an additional or relabeled signature as fulfillment of a specific request. The remaining cryptographic risk is bounded only when the verifier still enforces the requested covered components, parameters, key policy, nonce, and signed `tag`; label matching alone must never replace those checks. | `ParseAcceptSignatures` preserves the requested dictionary key, and an application fulfilling that request passes the same label to `Signer.Sign` under an exact signing profile. General verification may separately accept an intermediary-relabeled signature by selecting its actual label, but the application must not report that pair as fulfillment of the original request without trusted correlation. Signed `tag` values carry application semantics. |

Run `make interoperability` to fetch the exact commits into disposable
directories, run the selected peer suites and shared corpus with disposable Go
build and module caches, and remove all fetched data. A changed peer revision
requires a reviewed fixture and divergence audit before updating this file and
the gate.

The NIST P-384/SHA-384 verification vector is independently pinned in
[`sources.lock.json`](sources.lock.json), because RFC 9421 Appendix B does not
provide a P-384 example.
