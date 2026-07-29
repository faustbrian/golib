# Architecture

## Boundary

The root `mpt` package owns canonical paths, nodes, roots, immutable snapshots,
proofs, limits, errors, and the caller-facing node-store contract. It has no
service, queue, database, telemetry, EVM, network, JSON-RPC, or complete-client
dependency. Optional persistent adapters belong in additive nested modules.

Raw, secure, state, storage, transaction, and receipt profiles use distinct
constructors or types. No boolean selects key hashing or value encoding.

## Ownership

Caller input and returned bytes are copied. Immutable snapshots may be used
concurrently. A mutable writer has one caller-owned synchronization owner and
never launches hidden goroutines. Context cancellation and limits bound every
I/O or potentially expensive public operation.

## Canonical representation

In-memory node forms are implementation details. Persistence and proofs use
canonical RLP. Child nodes whose canonical encoding is shorter than 32 bytes
are embedded; encodings of 32 bytes or more are referenced by the legacy
Keccak-256 digest of the complete encoding. Public roots are 32-byte
commitments; encoded root nodes use an explicitly different type.

## Delivery sequence

1. Source pins, decisions, threat model, and public boundaries.
2. Nibbles, compact paths, RLP, Keccak references, nodes, and empty root.
3. Immutable lookup, update, deletion, and small-state model comparison.
4. Stores, atomic commit/publication, snapshots, iteration, builders, rebuild,
   missing-node recovery, and pruning.
5. Membership, absence, multi-key, range, and EIP-1186 proofs.
6. Ethereum profile helpers, official fixtures, independent differentials,
   hardening, documentation, benchmarks, and release evidence.
