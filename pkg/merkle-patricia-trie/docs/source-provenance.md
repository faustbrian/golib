# Source provenance

The following upstream revisions were resolved on 2026-07-29. They are
compatibility inputs, not dependencies. Imported fixtures will retain their
upstream bytes and carry a checksum, license, update procedure, applicability,
and local-coverage record beside the corpus.

| Source | Pinned revision | Purpose |
| --- | --- | --- |
| Ethereum Yellow Paper | `efc5f9a1f356cba376c978eedb63cb0363c2aa85` | Trie, RLP, and execution-layer definitions |
| execution-specs | `44d2b9cbd028b48f13e6ebf2635f977141cc397b` | Fork-aware executable specification |
| execution-spec-tests | `10eaa63d5da2f50b63d4359968f36542212f9f50` | Current official fixtures |
| ethereum/tests | `c67e485ff8b5be9abc8ad15345ec21aa22e290d9` | Legacy trie fixtures not yet superseded |
| go-ethereum | `b988c00bf4cba86ef5c43691ce84f4f2aea2821f` | Pinned Go differential oracle |
| Erigon | `aa82d55f3917439cd33cb1cbdca52f582d9bad11` | Independent Go differential oracle |
| Hyperledger Besu | `bf2a94134cd3a05fa5b1458e2dc199ae76bf23b2` | Independent interoperability oracle |
| Nethermind | `3261082a9e3b8cd833cdafe2b042bb29f8286044` | Independent interoperability oracle |
| ethereumjs monorepo | `3fa006c51b21877d160960e2d87dc3da6c58a71c` | Independent interoperability oracle |

Normative web sources are the current Ethereum Yellow Paper, execution
specifications, execution specification tests, legacy Ethereum tests,
ethereum.org MPT and RLP documentation, and applicable EIPs. Specifications
outrank client behavior.

## Updating

1. Resolve and record the exact upstream commit.
2. Review specification and client changes affecting claimed profiles.
3. Import fixtures byte-for-byte into a source-specific corpus.
4. Record upstream path, license, revision, SHA-256, fork/profile, and covered
   local tests in the corpus manifest.
5. Run checksum validation, conformance, both differential oracles, and all
   affected hostile-input tests.
6. Update compatibility decisions and the changelog when observable behavior
   or claimed scope changes.
