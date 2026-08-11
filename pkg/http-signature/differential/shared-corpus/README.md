# Shared-corpus differential interoperability

This standalone module executes one checked-in corpus through this module and
maintained independent Go implementations. It does not run peer self-tests.

## Comparisons

- Structured Field dictionary inputs are parsed and canonically serialized by
  the public `http-signature` parser for their protocol field and independently
  by `shogo82148/go-sfv`. Valid outputs must equal the corpus and each other;
  shared malformed inputs must be rejected by both.
- Every signature-base request is constructed from the same method, target,
  body, header instances, and component list. The local base must equal the
  complete expected base in the corpus.
- `yaronf/httpsign` and `dadrus/httpsig` do not export their generated signature
  bases. The strongest direct equivalent is used: each peer signs the shared
  message with the pinned HMAC-SHA-256 key, its emitted `Signature-Input` is
  independently parsed, the full base is reconstructed locally, and the peer's
  signature must equal HMAC-SHA-256 over those exact reconstructed bytes. The
  covered-component lines must also equal the fixed corpus base. A peer cannot
  pass by merely accepting its own output.

The content-digest case gives both peers the RFC example body and configures
`dadrus/httpsig` to derive SHA-512 explicitly, because that peer deliberately
updates `Content-Digest` while signing.

## Intentional Structured Field divergences

| Peer | Exact divergence | Specification analysis | Security impact and chosen behavior |
|---|---|---|---|
| `shogo82148/go-sfv` `v0.3.3` | A duplicate dictionary label is accepted and the later member replaces the earlier member. | The generic Structured Fields dictionary parsing algorithm has replacement semantics; RFC 9421 labels still identify security inputs consumed by an application. | Replacement can hide label shadowing from a verifier. The RFC 9421 path rejects duplicate labels before generic parsing. |
| `shogo82148/go-sfv` `v0.3.3` | Repeated component identifiers remain valid inner-list items. | Generic Structured Fields does not impose RFC 9421 component-coverage semantics. | Repetition can create canonicalization differentials. The RFC 9421 path rejects identical component identifiers. |
| `shogo82148/go-sfv` `v0.3.3` | A decimal dictionary value is valid generic SFV but invalid as an integrity preference. | RFC 9530 integrity preferences use integer weights from 0 through 10, a semantic constraint outside generic SFV. | A decimal could produce peer-dependent negotiation. The RFC 9530 parser rejects it. |

## Pinned peers

| Boundary | Version | Source revision |
|---|---|---|
| `yaronf/httpsign` | `v0.5.3-0.20260728182352-de382d35c1ad` | `de382d35c1add89cc09b9355161d61471fb7f632` |
| `dadrus/httpsig` | `v0.9.1-0.20260717221208-0f24bf7dd9b7` | `0f24bf7dd9b76727af985d9a6f7ce87207a18387` |
| `shogo82148/go-sfv` | `v0.3.3` | module tag and `go.sum` checksum |

The checked-in module has no `replace` directives. The disposable gate creates
a temporary workspace containing only the harness and implementation under
test; all peer dependencies remain immutable versions checked by `go.sum`.
Repository aggregate execution must register this module as a
non-releasable interoperability harness and resolve the owned dependency
through the repository local-proxy flow.

## Run

```sh
./check.sh
```

The gate uses a disposable workspace plus disposable build and module caches,
runs with `-mod=readonly`, and removes them afterward.
