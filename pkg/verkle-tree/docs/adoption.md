# Adoption, Migration, and FAQ

## Adoption decision

Adopt this module when a pre-v1 API and the documented backend qualifications
are acceptable. The current package implements its named profile and is useful
for authenticated application state, pinned interoperability work, and
caller-owned storage integration. It does not claim a stable public wire
protocol, external cryptographic audit, production suitability, or Ethereum
mainnet compatibility.

Before a deployment, record:

- the exact module revision and Go toolchain;
- profile name, numeric ID, and version;
- backend and transitive dependency revisions;
- generator-set identity and transcript;
- canonical root, proof, witness, snapshot, and node encoding versions;
- application key derivation and value semantics;
- resource limits and maximum accepted state/proof sizes;
- adapter capability, crash, recovery, and retention evidence; and
- a rollback path that preserves the prior reader and data format.

Do not persist only an unversioned root byte string. Persist the profile and
container identity alongside every application-level snapshot reference.

## Migration into the package

1. Freeze the source key/value corpus and define whether every all-zero value is
   present or absent. The package always treats an inserted all-zero value as
   present.
2. Convert keys and values to exact 32-byte application encodings. Hashing or
   truncation is an application protocol choice and must be versioned outside
   the package.
3. Construct a bounded `Snapshot`, encode it canonically, and record its
   profile-bound root.
4. Reconstruct the snapshot from those bytes and compare every ordered entry
   and root.
5. If using node storage, commit into a new profile-exclusive namespace, load
   through an isolated read view, and compare the reconstructed root and state.
6. Generate membership, absent-suffix, absent-stem, aggregate, and stateless
   update fixtures for the application corpus. Verify them independently from
   mutable state.
7. Run both old and new readers in observation mode. Do not publish a new root
   as authoritative until application invariants and adapter crash tests pass.

There is no supported in-place conversion from a Merkle tree, `go-verkle`
database, Rust Verkle database, or Ethereum state database. Rebuild from the
authoritative ordered key/value state into a separate namespace.

## Pre-v1 upgrade policy

Assume every pre-v1 release can change profile semantics or canonical bytes.
Before upgrading:

1. compare the profile specification and compatibility matrix;
2. regenerate exact roots, proofs, witnesses, and snapshot bytes for the pinned
   application corpus;
3. decode old artifacts only with the old binary;
4. rebuild new artifacts from authoritative state;
5. verify both stores before changing publication ownership; and
6. retain the old reader and namespace until rollback is no longer required.

Never reinterpret old bytes under a new profile version.

## Ethereum boundary

The package-owned profile borrows researched Bandersnatch/IPA conventions but
does not implement an Ethereum execution profile. It does not define account,
code, storage, or protocol key derivation; execution witnesses; gas rules;
migration overlays; network serialization; or activation rules.

Passing a `go-verkle`, Rust Verkle, or WASM cryptography fixture proves only the
exact shared construction exercised by that fixture. It does not establish
Ethereum client, network, or mainnet readiness. See
[compatibility status](compatibility.md) for the pinned claims.

There is also no current Ethereum migration target for this package to adopt.
Geth v1.17.0 describes binary-tree migration work as replacing its Verkle tree
implementation, while EIP-7864 remains Draft and does not yet select its hash.
The ethereum.org Verkle page is retained as moving background rather than an
activation specification. Applications MUST NOT encode an assumed Ethereum
transition into the package-owned pre-v1 profile.

## FAQ

### Is this a Merkle tree with branching factor 256?

No. Internal and stem nodes commit to wide scalar vectors with a vector
commitment. SHA-256 content addresses protect persisted node identity only;
they are not the Verkle root construction.

### Is a zero value absent?

No. `Set(key, Value{})` creates or preserves a present all-zero value. Use
`Delete(key)` for deletion. Reads and proof claims preserve that distinction.

### Does decoding a proof or witness verify it?

No. Decoding establishes canonical syntax, bounds, and ownership. Use
`ProofEngine.Verify` for proofs and `StatelessEngine.Apply` for witnesses. The
application must also compare the verified proof root and exact requested key
set, or the stateless result's pre-state root, with its trusted expectation.
When the application requires a particular claim kind or value, it must compare
that expectation too.

### Can verification consult the live tree?

It does not need to. Verification reconstructs the expected openings from the
profile-bound root, claims, topology, and proof container. Proof generation,
however, binds one immutable snapshot.

### Can I customize the curve, width, transcript, or generators?

No. Runtime cryptographic composition would create unnamed interoperability
variants. A different complete convention requires a different reviewed
profile identity.

### Does `RecoverStorage` repair a corrupt published snapshot?

No. It preserves every verified publication and removes only unreachable
unpublished nodes. Missing or corrupt reachable state fails and needs an
adapter-specific restoration source.

### Does a successful storage call prove durability?

No. It is the adapter's assertion. Prove it with real transactions, process
termination, restart, concurrent-view, and recovery tests for that adapter.

### Are snapshots and engines concurrency safe?

Immutable snapshots, proof engines, stateless engines, and verified values are
safe for concurrent reads and operations. Mutable store writers require the
adapter's explicit coordination and compare-and-swap semantics.

### Why are there so many limits?

Keys, persisted nodes, proofs, witnesses, inventories, and store responses are
hostile inputs. Limits prevent attacker-controlled allocation, recursion,
point decoding, multi-scalar multiplication, storage fan-out, and worker use.

### When will v1 be stable?

Only after an exact profile and production-suitable backend satisfy the freeze,
audit, provenance, canonical encoding, verification, interoperability, and
release gates. Until then the correct version is pre-v1.
