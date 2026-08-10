# Source provenance

The [specification decision register](specification-decisions.md) maps these
normative and interoperability inputs to selected behavior and executable
evidence.

The following upstream revisions were resolved on 2026-07-29. They are
compatibility inputs, not dependencies. Imported fixtures will retain their
upstream bytes and carry a checksum, license, update procedure, applicability,
and local-coverage record beside the corpus.

| Source | Pinned revision | Purpose |
| --- | --- | --- |
| Ethereum Yellow Paper | `efc5f9a1f356cba376c978eedb63cb0363c2aa85` | Trie, RLP, and execution-layer definitions |
| execution-specs | `44d2b9cbd028b48f13e6ebf2635f977141cc397b` | Fork-aware executable specification |
| execution-spec-tests repository | `10eaa63d5da2f50b63d4359968f36542212f9f50` | Archived fixture framework and format documentation |
| execution-spec-tests stable fixtures | `v5.4.0` (`88e9fb8f10ed89805aa3110d0a2cd5dcadc19689`) | Official stable blockchain fixture release |
| ethereum/tests | `c67e485ff8b5be9abc8ad15345ec21aa22e290d9` | Legacy trie fixtures not yet superseded |
| go-ethereum v1.17.3 | `117e067f0f0bae1a17082321f224dedb6765b10f` | Pinned Go differential oracle |
| Erigon | `aa82d55f3917439cd33cb1cbdca52f582d9bad11` | Pinned comparison source; no executable gate |
| Hyperledger Besu | `bf2a94134cd3a05fa5b1458e2dc199ae76bf23b2` | Pinned comparison source; no executable gate |
| Nethermind | `3261082a9e3b8cd833cdafe2b042bb29f8286044` | Pinned comparison source; no executable gate |
| @ethereumjs/mpt v10.1.2 | `3adf102baf8991f82feda860e0d3a3ec644d0802` | Independent interoperability oracle |
| @ethereumjs/rlp v10.1.2 | `3adf102baf8991f82feda860e0d3a3ec644d0802` | Independent canonical RLP oracle |

Normative web sources are the current Ethereum Yellow Paper, execution
specifications, execution specification tests, legacy Ethereum tests,
ethereum.org MPT and RLP documentation, and applicable EIPs. Specifications
outrank client behavior.

Geth is isolated in the alternate `go.interop.mod` test graph and is absent
from the production module graph. EthereumJS is installed only for the
interoperability gate from the exact package lock, whose archive integrity is
`sha512-dBlXpkP1ssp+AcUxsJUrY72LZuE1JQEp1AZn5mgmbBcd2Gwkpyi57q4zATlZbxrZUxp/K9UIidgqxQWcOCbo5g==`.
The independently pinned RLP archive integrity is
`sha512-T5Zt6C2pd02Wd88Q9A5/UX+He1Q2Y1LntHxz/038tfbUMiqby4fYSSTLEDx+TEfJqw1BsJSBY/TSu6goUzlk+w==`.
Neither client is used by the production package.

State-account fields and storage-zero deletion are derived from the pinned
execution-specs `state.py`, `state_mpt.py`, and fork `fork_types.py` account
encoder. Account RLP, minimally represented storage values, secure address and
slot paths, and roots are compared with Geth v1.17.3. Secure paths and roots
are also compared with the independently implemented EthereumJS MPT v10.1.2.
Canonical strings and lists at the short/long and length-of-length boundaries,
nested lists, and malformed or non-minimal encodings are compared directly
with Geth v1.17.3 and EthereumJS RLP v10.1.2.

The execution-spec-tests v5.4.0 `fixtures_stable.tar.gz` release asset is pinned
by its published SHA-256 digest and imported through a checksum-verifying
update script. Selected byte-identical Frontier, Berlin, London, Cancun, and
Prague blockchain fixtures prove official pre/post allocation state roots and
raw transaction roots for legacy and EIP-2718 types 1 through 4. The fixtures
contain receipt-root commitments but not receipt values, so they are not
presented as receipt-root construction evidence.

The fork-profile envelope table is derived from the pinned execution-specs
`berlin`, `london`, `paris`, `shanghai`, `cancun`, `prague`, and `osaka`
transaction and receipt encoders. The type framing and receipt-type binding are
derived from EIP-2718. Interoperability tests use Geth's concrete type-1 through
type-4 transaction and receipt encoders and `DeriveSha`, then compare the same
indexed roots with EthereumJS MPT. Ordered account and storage proofs generated
directly by Geth and EthereumJS are verified through the EIP-1186 helpers for
membership and absence. EthereumJS independently verifies the package's
generated account and storage proofs over the same secure paths.

Selected Geth v1.17.3 transition-tool outputs are imported byte-for-byte with
their LGPL-3.0 license and per-file checksums. Their exact receipt values
reconstruct legacy, type-2, type-3, and type-4 receipt roots. Type-1 receipt
roots remain covered dynamically by both pinned client oracles.

The separately labeled local EIP-1186 regression fixture records decoded proof
bytes for deterministic account membership, account absence, storage
membership, and storage absence claims. Its manifest records the exact
generator corpus, checksum, CC0 dedication, update procedure, and pinned Geth
and EthereumJS oracle revisions. It is not represented as an official
Ethereum fixture.

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

The selected Geth transition-tool corpus is imported byte-for-byte under
`testdata/go-ethereum`. Its manifest records the pinned revision, LGPL-3.0
license, per-file SHA-256 values, update procedure, fork applicability, exact
receipt-root coverage, and the explicit official-fixture limitation.
