# Ethereum profiles and proofs

## Profile selection

Choose the public type from the key and value contract, not from a convenience
preference:

| Use case | Public boundary | Key rule | Value rule |
| --- | --- | --- | --- |
| Low-level or indexed trie | `RawTrie` | Caller bytes directly | Non-empty opaque bytes |
| Secure application trie | `SecureTrie` | Keccak-256 exactly once | Non-empty opaque bytes |
| Ethereum state | `StateTrie` | Exact 20-byte address, hashed once | Canonical account RLP from `NewAccountValue` |
| Ethereum account storage | `StorageTrie` | Exact 32-byte slot, hashed once | Minimal non-zero integer RLP; zero deletes |
| Transaction root | `TransactionRoot` | `RLPIndexKey(index)` | Legacy RLP or fork-bound typed envelope |
| Receipt root | `ReceiptRoot` | `RLPIndexKey(index)` | Receipt envelope matching transaction kind/type/profile |

Do not prehash keys passed to a secure, state, or storage API. Do not use a
secure trie for transaction or receipt indexes; those indexes are already
canonical RLP byte keys in a raw trie.

## State accounts

`NewAccountValue` encodes:

```text
RLP([nonce, balance, storage_root, code_hash])
```

Nonce is an unsigned 64-bit integer. Balance is an unsigned 256-bit word.
Storage root and code hash are exact 32-byte values. Integer fields use minimal
RLP integer bytes. The helper does not apply empty-account clearing, code
lifecycle, or any fork's state-transition rules.

An account with a storage trie is constructed from the storage root:

```go
storage, err := mpt.NewStorageTrie(limits)
storage, err = storage.UpdateSlot(ctx, slot, word)
storageRoot, err := storage.Root()

accountValue, err := mpt.NewAccountValue(
    nonce,
    balance,
    storageRoot,
    codeHash,
    limits,
)
state, err := mpt.NewStateTrie(limits)
state, err = state.UpdateAccount(ctx, address, accountValue)
```

## Storage values

A storage slot key is always an exact 32-byte canonical slot. A non-zero
32-byte word is trimmed to its minimal unsigned bytes and stored as an RLP byte
string. An all-zero word deletes the slot. Consequently, missing and zero have
the same Ethereum state commitment but remain explicit at the API:
`GetSlot` returns `ErrAbsentKey` rather than a fabricated present zero.

## Transaction and receipt profiles

Legacy values are complete canonical RLP lists. Typed values are the exact
`type || payload` envelope from EIP-2718, where the supplied payload is a
canonical RLP list for the supported types.

| Fork profile | Accepted typed values |
| --- | --- |
| Berlin | Type 1 |
| London | Types 1-2 |
| Paris | Types 1-2 |
| Shanghai | Types 1-2 |
| Cancun | Types 1-3 |
| Prague | Types 1-4 |
| Osaka | Types 1-4 |

The helper validates envelope framing and activation only. It does not validate
transaction fields, signatures, receipt fields, logs, bloom filters, execution
status, or fork state transitions. Higher-level code must provide exact
already-validated bytes.

`ReceiptRoot` requires the matching transaction sequence and rejects a receipt
whose legacy/typed kind, type, or profile differs at the same index.

## Membership and absence proofs

`Proof` is an ordered root-to-terminal sequence of canonical RLP nodes. Embedded
children remain inside their parent and are not repeated. Verification binds
the expected root, selected raw or secure profile, exact key, value or absence,
node ordering, reference hashes, compact paths, terminal result, and resource
limits.

Use `ProofFromNodes` at a transport boundary after decoding hex or another
container format. It copies node bytes and applies aggregate bounds.
Verification then rejects missing, duplicate, reordered, unrelated, surplus,
non-canonical, or wrong-root nodes.

`MultiProof` deduplicates shared hashed nodes across sorted keys.
`MembershipClaim` and `AbsenceClaim` make each expected result explicit.

## Range proofs

Raw range proofs use an inclusive start and exclusive end in byte-key order.
An empty end is unbounded. The verifier requires the exact consecutive leaf
sequence and rejects omitted leaves or unused witness nodes.

Secure range APIs use already-transformed 32-byte paths:
`ProveHashedRange` and `VerifySecureHashedRange` do not hash endpoints or item
keys. This is a narrow proof/iteration boundary and is not a general prehashed
mutation API.

## EIP-1186 verification

The core accepts decoded proof-node bytes and deliberately does not depend on
JSON-RPC types. An adapter must:

1. obtain a trusted state root independently;
2. decode the 20-byte address and each 32-byte storage slot;
3. decode proof node hex into raw RLP bytes;
4. canonicalize the returned account fields with `NewAccountValue`;
5. verify the account proof;
6. use the storage root from the verified account; and
7. verify every storage membership or absence claim.

```go
accountValue, err := mpt.NewAccountValue(
    responseNonce,
    responseBalanceWord,
    responseStorageRoot,
    responseCodeHash,
    limits,
)
accountProof, err := mpt.ProofFromNodes(accountProofNodes, limits)
account, err := mpt.VerifyAccountProof(
    ctx,
    trustedStateRoot,
    address,
    accountValue.Bytes(),
    accountProof,
    limits,
)

slotProof, err := mpt.ProofFromNodes(storageProofNodes, limits)
err = mpt.VerifyStorageProof(
    ctx,
    account,
    slot,
    minimalNonZeroStorageValue, // nil verifies absence/zero
    slotProof,
    limits,
)
```

For an absent account, call `VerifyAccountAbsence`; do not construct a synthetic
empty account. A verified absent account, an absent slot under a verified
account, and a malformed proof are distinct outcomes.

JSON quantity parsing, hex decoding, RPC request/response structures, block-tag
resolution, and root authorization belong in the adapter. Proof verification
establishes only the claim under the supplied root. It does not establish that
the root is canonical-chain, finalized, recent, or authorized.

The package-level `ExampleVerifyAccountProof` is an executable version of the
account-membership flow and is compiled and run by the documentation gate.

## Compatibility evidence

Official execution-spec-tests allocations bind state and storage roots and
their block RLP binds legacy and type-1 through type-4 transaction roots.
Pinned Geth transition fixtures bind exact legacy and typed receipt values to
receipt roots. Geth and EthereumJS independently agree on state, storage,
transaction, receipt, membership, absence, and range-proof surfaces documented
in [source provenance](source-provenance.md).
