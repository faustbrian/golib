# Local EIP-1186 regression fixture

- Origin: deterministic local regression corpus; not an official Ethereum
  fixture.
- Format: transport-decoded EIP-1186 account and storage proof material.
- Generator inputs: the exact state, account, storage, and decoy values in
  `TestEthereumJSEIP1186AccountAndStorageProofs`.
- Independent oracle: `@ethereumjs/trie` 10.1.2 at
  `3adf102baf8991f82feda860e0d3a3ec644d0802`.
- Additional semantic oracle: go-ethereum 1.17.3 at
  `117e067f0f0bae1a17082321f224dedb6765b10f`.
- License: CC0-1.0.
- Applicability: account membership, account absence, storage membership, and
  storage absence under explicit supplied roots. JSON-RPC encoding is outside
  scope.
- Local coverage: `eip1186_fixture_test.go`,
  `ethereumjs_interop_test.go`, and `_interop/geth_interop_test.go`.

| File | SHA-256 |
| --- | --- |
| `eip1186.json` | `9ef6b4b3a4b740172a6372c717277de725ace3d8ac0af958d4087bcc13f6d000` |

## Update procedure

1. Review EIP-1186, the pinned client revisions, and the deterministic corpus.
2. Generate proofs from the local state and storage tries.
3. Verify the exact roots, values, and proof nodes with EthereumJS and Geth.
4. Serialize the decoded proof bytes without JSON-RPC quantity conversion.
5. Recompute the SHA-256 value and run conformance and interoperability gates.
