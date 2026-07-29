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
| go-ethereum v1.17.3 | `117e067f0f0bae1a17082321f224dedb6765b10f` | Pinned Go differential oracle |
| Erigon | `aa82d55f3917439cd33cb1cbdca52f582d9bad11` | Independent Go differential oracle |
| Hyperledger Besu | `bf2a94134cd3a05fa5b1458e2dc199ae76bf23b2` | Independent interoperability oracle |
| Nethermind | `3261082a9e3b8cd833cdafe2b042bb29f8286044` | Independent interoperability oracle |
| @ethereumjs/mpt v10.1.2 | `28d6a165971964f239b77004196c6c0e87697835` | Independent interoperability oracle |

Normative web sources are the current Ethereum Yellow Paper, execution
specifications, execution specification tests, legacy Ethereum tests,
ethereum.org MPT and RLP documentation, and applicable EIPs. Specifications
outrank client behavior.

Geth is isolated in the alternate `go.interop.mod` test graph and is absent
from the production module graph. EthereumJS is installed only for the
interoperability gate from the exact package lock, whose archive integrity is
`sha512-dBlXpkP1ssp+AcUxsJUrY72LZuE1JQEp1AZn5mgmbBcd2Gwkpyi57q4zATlZbxrZUxp/K9UIidgqxQWcOCbo5g==`.
Neither client is used by the production package.

State-account fields and storage-zero deletion are derived from the pinned
execution-specs `state.py`, `state_mpt.py`, and fork `fork_types.py` account
encoder. Account RLP, minimally represented storage values, secure address and
slot paths, and roots are compared with Geth v1.17.3. Secure paths and roots
are also compared with the independently implemented EthereumJS MPT v10.1.2.

The fork-profile envelope table is derived from the pinned execution-specs
`berlin`, `london`, `paris`, `shanghai`, `cancun`, `prague`, and `osaka`
transaction and receipt encoders. The type framing and receipt-type binding are
derived from EIP-2718. Interoperability tests use Geth's concrete type-1 through
type-4 transaction and receipt encoders and `DeriveSha`, then compare the same
indexed roots with EthereumJS MPT.

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

## Imported corpus

The legacy `ethereum/tests` `TrieTests` corpus at the pinned revision is
imported byte-for-byte under `testdata/ethereum-tests`. Its manifest records
per-file SHA-256 values, license, update procedure, applicability, and the
tests covering raw roots, secure roots, ordered mutations, hex byte inputs,
and neighbor iteration behavior.
