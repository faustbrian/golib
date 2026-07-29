# Ethereum tests fixture manifest

- Source: `https://github.com/ethereum/tests`
- Revision: `c67e485ff8b5be9abc8ad15345ec21aa22e290d9`
- Upstream paths: `TrieTests/*.json`, `LICENSE`
- License: MIT; the byte-identical upstream text is in `LICENSE`
- Applicability: legacy execution-layer raw and secure modified Merkle
  Patricia trie roots, ordered mutations, hex byte inputs, and neighbor
  iteration semantics; no fork-dependent execution behavior
- Local coverage: `legacy_fixture_test.go`

| File | SHA-256 |
| --- | --- |
| `TrieTests/hex_encoded_securetrie_test.json` | `487f9e1e404e46dc0a54d526b14927d3a5ba90f7f52625e7d49cd170974ce9ff` |
| `TrieTests/trieanyorder_secureTrie.json` | `acba24dcef034b8ddd78d4ba8e716468854bac1a5ac886cc806c93f1c93f1ed4` |
| `TrieTests/trieanyorder.json` | `92404d5c2076524e62f02e9657a684aa0561067d49f3b489b78b5033c6fc3e2d` |
| `TrieTests/trietest_secureTrie.json` | `98b76fd92fed69cb449d7a555cdb3eb397e7179614857de1289de2f63ac8e77a` |
| `TrieTests/trietest.json` | `0ce5e1151210958edf47911b332fe188696d741f9f44b1a471ee62bc666c1f0f` |
| `TrieTests/trietestnextprev.json` | `ac9d8f62664d6b47ab25050e16617d617ef3363b4905590829267f9a9d33c6f0` |

## Update procedure

1. Fetch the source repository and check out the exact reviewed revision.
2. Copy the listed files without transformation.
3. Recompute every SHA-256 value and review upstream license or fixture-format
   changes.
4. Run the checksum, root, mutation, secure-profile, and neighbor fixture
   tests.
5. Update the pinned revision, compatibility decisions, and changelog only
   after reviewing any changed expected behavior.
