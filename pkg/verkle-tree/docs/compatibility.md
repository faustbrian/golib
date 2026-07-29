# Compatibility Status

No compatibility claim is currently implemented.

| Target | Pinned revision or status | Intended use | Claim |
| --- | --- | --- | --- |
| Generic `verkle-tree` v1 | Not frozen | Future package profile | None |
| `ethereum/go-verkle` | `aa0a270c0ed03faa6c502e0d96bf26189d1d6542` | Go differential research | No API, wire, or production compatibility |
| `crate-crypto/rust-verkle` | `e27b8b4edf1992b4afa636c2fc7983bcc27ddb88` | Independent differential research | Canonical scalar and Banderwagon commitment encoding only; no tree, proof, API, wire, or production compatibility |
| `crate-crypto/verkle-trie-ref` | `483f40c737f27bc8f059870f862cf6c244159cd4` | Algorithm and transcript research | Work-in-progress reference only |
| EIP-6800 | Stagnant at EIPs commit `c55786f4242e5324afd14c6bca890a369a771d7f` | Historical Ethereum Verkle layout | Not implemented |
| EIP-7612 | Stagnant at the same EIPs commit | Historical overlay transition | Out of generic package scope |
| EIP-4762 | Draft at the same EIPs commit | Witness-related gas changes | Out of generic package scope |
| EIP-7748 | Draft at the same EIPs commit | Historical state conversion | Out of generic package scope |
| EIP-7864 | Draft at the same EIPs commit | Current binary-tree alternative | Not a Verkle profile |
| Ethereum mainnet | No activated Verkle profile selected | Protocol integration | No readiness claim |

Agreement with one implementation or one fixture set will not establish broader
compatibility. Any future row marked compatible must identify the exact profile,
root semantics, transcript, generators, proof form, canonical encoding, and
positive and negative differential corpus.

The Rust encoding claim is reproduced by the pinned Cargo harness in
`interoperability/rust-verkle`. It generates five deterministic scalar and
generator-multiple pairs and compares them byte-for-byte with the fixture
consumed by the Go decoder tests. No setup, commitment-vector, opening,
transcript, tree, proof, or witness operation is exercised.

Protocol activation, client database migration, gas accounting, block
execution, and network witness distribution remain outside the generic package.
