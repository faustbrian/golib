# Usage Guide

## Stability first

The only constructible profile is
`BandersnatchIPA256V0`. It is pre-v1, may change incompatibly, and
is not an Ethereum compatibility claim. New deployments must persist the exact
profile identity with every root, proof, witness, and node namespace and must
reject a different profile before cryptographic work.

The package has no unbounded defaults. Every expensive operation requires a
`context.Context` and explicit limits. Start with workload-specific bounds,
observe `ResourceError` results, and raise one measured resource at a time.
Do not copy test limits into a service without measuring its state and proof
cardinality.

## Initialize or reconstruct a snapshot

Use `NewSnapshot` for an ordered or unordered in-memory entry corpus. Keys and
values are fixed 32-byte arrays. Entries are defensively copied and sorted;
duplicate keys fail the complete construction. A present all-zero `Value` is
not absence.

Use `DecodeSnapshot` only for the package-owned canonical whole-snapshot
format. Decoding validates the container, rebuilds the complete authenticated
tree, and compares the derived root. It is not a shortcut around proof or
storage verification.

Use `LoadSnapshot` for caller-owned node storage. The `NodeReader` must provide
one isolated `NodeReadSnapshot`; the package verifies every reachable content
address, canonical node, topology edge, mathematical root, and root-node
address before returning a snapshot.

## Read and update immutable state

`Snapshot.Get` returns `(value, present, error)`. Interpret the result in this
order:

1. handle a non-nil error;
2. when `present` is false, treat the key as absent and ignore `value`; and
3. when `present` is true, retain the value even when every byte is zero.

Build updates with `Set` and `Delete`, then call `Snapshot.Apply`. Duplicate
keys reject the whole batch. Accepted updates are canonicalized by key, create
a new immutable snapshot, and leave the receiver unchanged. An authenticated
delete of an absent key is a deterministic no-op; setting an all-zero value is
not deletion. Check `Transition.PreRoot()` and `Transition.PostRoot()` when a
separate component must bind the exact state change.

Snapshots, roots, proofs, witnesses, proof engines, stateless engines, and
successful result values are immutable and safe for concurrent reads. Store
adapters own mutable writer coordination. Do not mutate an adapter namespace
concurrently unless the adapter implements the required compare-and-swap and
snapshot-isolation contracts.

## Generate and verify proofs

Initialize one `ProofEngine` for the fixed profile and reuse it concurrently.
`ProofEngine.Prove` accepts an unordered, duplicate-free key set and derives
membership or non-membership claims from one immutable snapshot. Multiple keys
produce one aggregate opening; a single-key call uses the same canonical proof
system.

The pinned dependency internally chooses `runtime.NumCPU()` workers and
cannot be cancelled after proof arithmetic begins. Set `OpeningLimits.MaxWorkers`
to at least that fixed demand. Each engine admits one dependency proof call at
a time; `MaxQueuedOperations` bounds concurrent calls waiting for that slot,
and zero makes excess calls fail with `ErrResourceExhausted` instead of waiting.
A queued call respects its context. Use separate engines only after accounting
for the multiplied worker and setup-memory budget; cancellation still cannot
stop an already admitted dependency call.

When the same process also verifies stateless witnesses, pass that initialized
proof engine to `NewStatelessEngineFromProofEngine`. This avoids a second
aggregate-opening setup. Proof generation and stateless verification then
share the proof engine's bounded dependency-operation gate; use a separate
stateless engine only when the additional setup memory and workers are
intentional.

Proof bytes are untrusted input. `DecodeProof` establishes canonical syntax and
ownership only. Obtain `Proof.Root()` and `Proof.Claims(ctx)`, compare the root
with the exact trusted root and the claim keys with the exact requested key
set, and call `ProofEngine.Verify` before accepting any claim. When the caller
requires a particular claim kind or value, compare that expectation too;
otherwise the verified claim kind and value are authenticated outputs.
Accepting any root or key set carried by an otherwise valid proof permits
application-level replay. A verification error is not an absence result.

`Proof.Claims` returns an owned canonical copy. `Claim.Value` distinguishes a
membership value from an absence claim, including present all-zero values.

## Produce and apply stateless witnesses

For an update batch, use `ProofEngine.ProveUpdates`. It adds the exact bounded
auxiliary claims needed for topology-changing deletion. Apply the same updates
to the trusted stateful snapshot to obtain the claimed post-state root, then
construct a `Witness` with `NewWitness`.

`DecodeWitness` validates canonical syntax only. A consumer must initialize a
`StatelessEngine` and call `StatelessEngine.Apply`. Compare the successful
result's `PreRoot()` with the exact trusted pre-state root before authorizing the
transition. Success means the complete carried pre-state proof verified, the
canonical updates were applied, and the independently derived post-state root
matched the witness. It does not establish application authorization, execution
validity, or storage durability by itself.

## Canonical bytes and ownership

`Root.Bytes`, `Snapshot.Bytes`, `Proof.Bytes`, and `Witness.Bytes` produce the
package-owned pre-v1 formats documented in the profile specification.
Returned byte arrays and slices are caller-owned. Decoders defensively own
accepted input. These encodings are not `go-verkle`, Rust Verkle, Ethereum
execution-witness, or network wire formats.

## Error handling

Use `errors.Is` for sentinel categories and `errors.As` for `ResourceError` and
`StoreCapabilityError`. Preserve wrapped causes in operational error handling,
but do not log keys, values, proof bytes, witness bytes, persisted node bytes,
or cryptographic intermediates.

Important distinctions include:

- absence versus present zero;
- `ErrStorageNodeMissing` versus `ErrStorageNodeCorrupt`;
- cancellation versus resource exhaustion;
- malformed encoding versus cryptographic verification failure;
- unsupported profile versus invalid zero-value use; and
- stale publication state versus general adapter failure.

See [API boundaries](api-boundaries.md), the
[threat model](threat-model.md), and the
[pre-v1 profile specification](../specification/bandersnatch-ipa-256-v0.md)
for the exact contracts.
