# Experimental Public API Audit

## Scope and status

This audit covers the complete exported surface of package
`github.com/faustbrian/golib/pkg/verkle-tree` as represented by
`api/baseline.txt` with SHA-256
`67541479daa50169f3e49a6a973304abf3e9ea9c687e0c073c7ae5d3f6ce952f`.
It reviews semantics, ownership, error classification, concurrency, resource
cost, and caveats. Every exported field has an inline Go documentation comment;
the tables below cover every exported type, constant, variable, function, and
method, including the methods re-exported by the immutable `Profile` alias.

This is an audit of the current experimental API, not a stable-release or
cryptographic-security claim. The production backend, maintained independent
implementation, stable profile, final mutation campaign, and release evidence
remain open. Any exported API change invalidates this audit until the API
baseline checksum and this report are refreshed together.

## Cross-cutting contract

- `Key`, `Value`, `NodeID`, and canonical root bytes are fixed-size values and
  cannot alias caller memory.
- Opaque values defensively own retained data. Slice-returning accessors return
  copies; no public operation transfers ownership to the package implicitly.
- `Snapshot`, `ProofEngine`, `Proof`, `Witness`, `StatelessEngine`, roots,
  transitions, publications, audits, and successful maintenance results are
  immutable and safe for concurrent reads. Store view interfaces require only
  sequential use by one operation; adapters own synchronization across calls.
- Every I/O or attacker-amplified public operation accepts `context.Context`.
  Nil contexts return `ErrInvalidContext`; cancellation and deadlines match
  both `ErrCancelled` and the original context error.
- Zero opaque values either expose only safe zero metadata or reject any
  operation that could return usable state with their documented typed error.
  Fixed-size byte values and enum zero values remain ordinary data only where
  explicitly documented.
- Limit structures have no unbounded zero default. Each exported field names
  one checked resource and is documented at its declaration.
- Errors preserve `errors.Is`; `ResourceError` and `StoreCapabilityError`
  additionally preserve `errors.As` with exact sanitized metadata.
- No public identifier exposes a curve point, scalar, generator, transcript,
  backend proof type, mutable node, scratch buffer, or runtime composition
  callback.

## Profile, roots, and fixed data

| Exported identifiers | Semantics and ownership | Errors, concurrency, cost, and caveats |
| --- | --- | --- |
| `ProfileID`, `ProfileBandersnatchIPA256V0`, `Profile`, `ExperimentalBandersnatchIPA256V0`; `Profile.ID`, `Name`, `Version`, `Experimental`, `BranchingWidth`, `KeySize`, `StemSize`, `ValueSize`, `EncodingVersion`, `Validate` | One immutable package-owned convention; callers cannot compose algorithms. | Constant time and concurrency safe. Zero or altered profiles match `ErrUnsupportedProfile`. The only profile is experimental. |
| `RootSize`, `RootDecodingLimits`, `Root`, `DecodeRoot`, `Root.Bytes`, `Profile`, `IsEmpty` | One exact profile-bound canonical root; returned bytes are by value. | Constant-size validation and at most one point decode. Zero roots match `ErrInvalidRoot`; malformed and unsupported-profile inputs remain distinct. |
| `Key`, `Value`, `Entry`, `Entry.Key`, `Entry.Value` | Exact 32-byte keys and values. A present all-zero `Value` differs from absence. | Value copies are constant time. `Entry` is caller-owned input and is copied before retention. |

## Immutable state and transitions

| Exported identifiers | Semantics and ownership | Errors, concurrency, cost, and caveats |
| --- | --- | --- |
| `UpdateKind`, `UpdateSet`, `UpdateDelete`, `Update`, `Set`, `Delete`, `Update.Kind`, `Key`, `Value` | Immutable Set or Delete; Delete has no value and absent deletion is a deterministic no-op. | Accessors are constant time. Zero updates match `ErrInvalidUpdate`; duplicate batch keys match `ErrDuplicateKey`. |
| `StateLimits`, `TreeLimits`, `CommitmentLimits`, `SnapshotLimits` and every exported field | Explicit state, topology, commitment, and temporary-memory budgets. | Validation is constant time and rejects zero, overflow-prone, or package-maximum violations with `ErrInvalidLimits`. |
| `Snapshot`, `NewSnapshot`, `Snapshot.Get`, `Root`, `Apply`; `Transition`, `Transition.PreRoot`, `PostRoot` | Construction owns, validates, and orders entries. Apply publishes a new snapshot atomically and leaves the receiver unchanged. | Snapshots and transitions are concurrency safe. Get is logarithmic in retained entries. Construction and Apply rebuild the complete committed tree and are bounded by entries, nodes, commitments, and commitment terms; they are not incremental-update APIs. |
| `SnapshotEncodingLimits`, `SnapshotDecodingLimits`, `Snapshot.Bytes`, `DecodeSnapshot` | One canonical whole-state encoding; decode rebuilds and compares the authenticated root before returning state. | Linear in encoded entries plus full committed-tree reconstruction. Alternate order, lengths, roots, profiles, and trailing bytes fail closed. |

## Proofs and stateless witnesses

| Exported identifiers | Semantics and ownership | Errors, concurrency, cost, and caveats |
| --- | --- | --- |
| `OpeningLimits`, `ProofMaterialLimits`, `ProverQueryLimits`, `VerifierQueryLimits`, `ProofContainerLimits`, `ProofGenerationLimits`, `ProofVerificationLimits`, `ProofEncodingLimits`, `ProofDecodingLimits` and every exported field | Stage-specific bounds for paths, queries, decodes, MSM work, workers, bytes, and scratch memory. | Validated before attacker-amplified work. The backend cannot be interrupted inside its current fixed proof call despite pre/post cancellation checks. |
| `ClaimKind`, `ClaimMembership`, `ClaimAbsence`, `Claim`, `Claim.Kind`, `Key`, `Value` | Immutable membership or exact-key absence assertion; all-zero membership is unambiguous. | Constant-time accessors. A zero or inconsistent claim matches `ErrInvalidProof`. |
| `ProofEngine`, `NewProofEngine`, `ProofEngine.Prove`, `ProveUpdates`, `Verify`; `Proof`, `DecodeProof`, `Proof.Bytes`, `Claims`, `Root` | Fixed-profile aggregate proof generation and independent verification over exact canonical claims and paths. Returned proof data is owned. | Engines and proofs are concurrency safe. Work scales with keys, distinct stems, paths, complete vector openings, and backend MSM terms. Decode alone never verifies. Verification fails closed with `ErrVerification`; malformed containers match `ErrInvalidProof`. |
| `WitnessLimits`, `WitnessEncodingLimits`, `WitnessDecodingLimits`, `StatelessUpdateLimits` and every exported field | Explicit construction, codec, embedded-proof, update, path, commitment, field, point, and scratch budgets. | No unbounded defaults; nested proof limits are independently enforced. |
| `Witness`, `NewWitness`, `DecodeWitness`, `Witness.Bytes`, `Proof`, `Updates`, `PostRoot` | Immutable canonical proof, ordered update batch, and claimed post-root container. Accessors return owned values. | Concurrency safe. Construction and decoding do not verify the proof or post-root. Missing or surplus authenticated material fails closed. |
| `StatelessEngine`, `NewStatelessEngine`, `StatelessEngine.Apply`; `StatelessResult`, `PreRoot`, `PostRoot` | Verifies the complete witness, applies authenticated updates, and compares the independently derived post-root. | Concurrency safe. Cost scales with proof openings and authenticated topology changes. A zero engine/result rejects use; mismatch matches `ErrPostStateMismatch`. |

## Caller-owned storage

| Exported identifiers | Semantics and ownership | Errors, concurrency, cost, and caveats |
| --- | --- | --- |
| `NodeIDSize`, `NodeID`, `NodeID.Bytes`, `StoredNode`, `StoredNode.ID`, `Encoded` | SHA-256 content addresses and immutable canonical node bytes. Encoded returns an owned copy. | Fixed-size or linear in one node encoding. Content addressing is integrity evidence, not adapter durability evidence. |
| `StoreCapabilities`, all seven `StoreCapability*` constants, all four `Required*StoreCapabilities` constants, `StoreCapabilities.Supports`, `StoreCapabilityError` and its fields/methods | Explicit adapter guarantee negotiation and exact missing-capability reporting. Unknown capability bits are ignored. | Constant time. Missing guarantees fail before encoding or I/O and match `ErrStoreCapability`. |
| `NodeStore`, `StoreCommit`, all `StoreCommit` accessors, `Snapshot.Commit` | One atomic durable node-set and compare-and-swap root publication request. Adapter inputs are owned copies. | Core work is linear in committed nodes and bytes. The adapter must provide atomicity and durability; a successful return is its assertion, not independent proof. |
| `StorePublication`, `NewStorePublication`, `StorePublication.Root`, `RootNode`; `NodeReader`, `NodeReadSnapshot`; `StorageReadLimits`, `LoadSnapshot` | One fixed publication/root-node pair and one isolated read view. Load validates every address and node, rebuilds state, and closes exactly once. | Linear in reachable nodes, edges, bytes, and full reconstruction. Missing and corrupt nodes are distinct. The adapter owns snapshot isolation and cleanup behavior. |
| `NodeAuditStore`, `NodeAuditSnapshot`, `StorageAuditLimits`, `StorageAudit`, `AuditStorage`, all `StorageAudit` count and node accessors | Read-only verification of current and retained publications plus complete canonical inventory classification. Returned unreachable IDs are owned and ordered. | Linear in all verified reachable nodes plus inventory. The result is not deletion authority. View methods are sequential; the adapter owns isolation. |
| `NodeMaintenanceStore`, `StoreMaintenance` and all accessors, `MaintainStorage`, `RecoverStorage`, `StorageMaintenanceResult` and all accessors | One profile-bound atomic compare/retain/delete request, or preservation of every publication while deleting unpublished debris. | Linear in all publications, reachable nodes, and inventory. The adapter owns the atomic handoff, durable deletion, deferred reclamation, and process-restart guarantees. The core cannot restore missing or corrupt published nodes. |
| `StorageLimits`, `StorageReadLimits`, `StorageAuditLimits` and every exported field | Explicit node, edge, entry, call, byte, hash, point, page, publication, result, and scratch budgets. | Checked before amplified allocation or adapter access. Nested snapshot reconstruction has independent limits. |

The storage interface methods `Capabilities`, `CommitSnapshot`, `OpenSnapshot`,
`Publication`, `ReadNode`, `Close`, `OpenAudit`, `CurrentPublication`,
`RetainedPublications`, `NodeIDs`, `MaintenanceProfile`, and
`ApplyMaintenance` are all part of the audited exported surface. Their Go
documentation defines ownership, ordering, maximum-length enforcement,
sequential-use, cleanup, compare-and-swap, and atomicity obligations.

## Errors and resource identifiers

The exported sentinel set is:

`ErrUnsupportedProfile`, `ErrInvalidContext`, `ErrCancelled`,
`ErrInvalidLimits`, `ErrInvalidSnapshot`, `ErrInvalidUpdate`,
`ErrDuplicateKey`, `ErrInvalidTransition`, `ErrInvalidRoot`,
`ErrInvalidProofEngine`, `ErrInvalidProof`, `ErrInvalidWitness`,
`ErrInvalidStatelessEngine`, `ErrInvalidStatelessResult`,
`ErrIncompleteWitness`, `ErrUnsupportedUpdate`, `ErrPostStateMismatch`,
`ErrInvalidStore`, `ErrStoreCapability`, `ErrStorageCommit`,
`ErrStorageRead`, `ErrStorageAudit`, `ErrStorageMaintenance`,
`ErrInvalidRetention`, `ErrStorageInventory`, `ErrStorageSnapshotMissing`,
`ErrStorageNodeMissing`, `ErrStorageNodeCorrupt`, `ErrStaleRoot`,
`ErrVerification`, `ErrResourceExhausted`, and `ErrCryptographic`.

Every sentinel has one non-overlapping caller-visible meaning documented at its
declaration. Wrapped adapter and context errors remain discoverable without
including keys, values, roots, proofs, witnesses, or node contents in package
diagnostics.

`Resource`, `ResourceError`, `ResourceError.Resource`, `Limit`, `Actual`,
`ResourceError.Error`, and `Unwrap` form the typed budget-error surface. The
resource constants cover entries, batch updates, keys, stems, stem paths,
nodes, edges, commitments, path commitments and derivations, path bytes,
queries, node reads and bytes, encoded node bytes, hashes, claims, field
mappings, commitment terms and updates, generator derivations, precomputed
points, scalar decodes, MSM terms, temporary bytes, root bytes, point decodes,
proof bytes, workers, publications, inventory pages and nodes, unreachable
nodes, witness bytes, path lookups, and snapshot bytes. Each exact constant is
documented at its declaration and identifies the preflight dimension reported
through `errors.As`.

## Audit result and remaining boundary

No exported identifier bypasses the fixed profile, exposes unchecked
cryptographic values, retains caller slices without copying, or silently treats
zero limits as unbounded. No public-contract defect was identified in this
audit.

The surface remains experimental because its cryptographic backend is not
approved for production and the stable profile cannot yet be frozen. This
report therefore closes the current API inventory and documentation review,
but it does not constitute the final hardening-completion report or earn a
stable-release claim.
