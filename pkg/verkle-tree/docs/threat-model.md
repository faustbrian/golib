# Threat Model

## Security objective

A verifier must accept only a canonical proof or witness that establishes the
claimed values or absences for the exact key set, profile, and root. Tree
updates must produce deterministic immutable snapshots without publishing
partially durable state.

This document defines the complete audit scope. Current internal controls cover
canonical point and scalar decoding, strict profile-bound root decoding,
strict bounded decoding of the fixed raw aggregate-opening payload, fixed
generator-set validation, bounded serial vector commitment, immutable state
transitions, canonical stem topology, bounded deterministic full-root
construction, atomic root-bound snapshot transitions, and canonical
profile-bound tree claims plus an immutable canonical root-bound unverified
tree-proof container with an exact package-owned encoding and strict aggregate
decoder, and public fixed-profile aggregate tree-proof generation and
verification, plus canonical content-addressed storage writes,
capability-checked atomic root publication, and bounded isolated persisted
snapshot reconstruction, plus bounded read-only auditing of current and
retained roots against a canonical complete node inventory and bounded atomic
retention/pruning requests, plus canonical bounded stateless witnesses that
verify a complete pre-state proof and independently match the claimed
post-state root, including creation below authenticated missing or different
stem paths and deletion that is absent, topology-preserving, or backed by
complete authenticated collapse disclosure. Crash-repair application,
dependency-level cancellation, concrete storage
adapters, and complete side-channel controls remain unimplemented.

## Trust boundaries

The following inputs are hostile:

- keys, values, update batches, duplicate operations, and declared sizes;
- persisted nodes, roots, snapshot identifiers, and storage responses;
- points, scalars, commitments, openings, proofs, and witnesses;
- profile identifiers, serialization versions, transcripts, and generator
  identities;
- fixtures, generated constants, setup material, and dependencies;
- contexts, callbacks, stores, workers, scratch buffers, and concurrent callers;
  and
- benchmark corpora and diagnostic output.

The caller owns storage durability and mutable writer coordination. The package
must validate cryptographic and serialization invariants and must not claim
stronger atomicity, isolation, or durability than the selected store provides.

## Primary attacks

### Proof soundness and malleability

- cross-root, cross-key-set, cross-profile, cross-version, and cross-proof-kind
  replay;
- omitted, duplicated, reordered, surplus, or conflicting openings;
- batch verification that accepts when only a subset is valid;
- missing transcript fields, ambiguous encodings, absent domain separation, or
  attacker-controlled batch coefficients; and
- alternate encodings that decode to the same mathematical object.

### Group and field validation

- malformed, identity, infinity, off-curve, wrong-subgroup, and quotient-group
  edge-case points;
- non-canonical, negative-equivalent, or out-of-field scalars;
- generator or setup substitution; and
- exceptional formulas, architecture-specific behavior, or mutable precomputed
  state.

The raw aggregate-opening decoder currently mitigates alternate payload length,
trailing-byte, malformed-point, non-canonical-point, wrong-subgroup-point,
non-canonical-scalar, caller-aliasing, and declared decode budget attacks. It
accepts the exact all-zero identity representation only in proof-point
positions because valid IPA proofs can require it; identity remains forbidden
for roots, nodes, paths, and standalone commitments. Arbitrary identity
substitution remains subject to the complete cryptographic equation.
Acceptance proves only canonical syntax for one opaque payload; it does not
prove the opening or authenticate any tree claim.

The root decoder rejects wrong profile and encoding headers before point work,
rejects alternate lengths and non-canonical commitment payloads, and uses a
distinct empty kind so an identity point cannot be smuggled through root bytes.
It does not bind a snapshot or prove that the committed state is available.

The stateless witness decoder rejects wrong profile/version headers, alternate
lengths, trailing bytes, empty or reordered update sets, duplicate keys,
unsupported update kinds, malformed post-state roots, and malformed embedded
proofs under separate witness, proof, update, and scratch limits, with exactly
one permitted post-root point decode.
It also requires the embedded proof claim keys to equal the canonical update
keys exactly, rejecting both omission and surplus disclosure.
Decoding establishes canonical structure only. `StatelessEngine.Apply`
cryptographically verifies the complete embedded proof before using old values
or terminal topology, requires an exact authenticated claim and stem path for
every update, constructs canonical missing/different-stem subtrees when
required, derives all changed commitments bottom-up, and rejects a different
claimed post-state root. A successful result does not authorize the update, prove
storage availability, or establish application-level execution validity.

### Tree and state transitions

- absent, zero, empty, and deleted values becoming indistinguishable;
- path, suffix, width, or node-kind confusion;
- nondeterministic map iteration or update ordering;
- duplicate or conflicting batch operations;
- mixed-snapshot proof reads; and
- partial publication, stale-root publication, corrupt-node use, or unsafe
  pruning.

The authenticated-state layer currently mitigates absent/zero/delete
ambiguity, nondeterministic batch order, duplicate operations, partial
in-memory publication, caller mutation of fixed arrays, cross-snapshot root
confusion, omitted or changed old values in supported stateless witnesses, and
omitted or conflicting terminal topology for new-stem insertion. A present
delete must authenticate its old value and either an exact retained member or a
same-stem Set; otherwise it fails closed before topology could change. The
package does not yet prove deletion-time collapse completeness or protect a
future mutable writer from concurrent ownership violations.

The storage write boundary encodes the complete immutable arena into canonical
profile-bound nodes, hashes the complete bytes for content addressing, orders
the batch deterministically, copies adapter-visible encodings, and rejects
missing atomicity, durability, immutable-node, or compare-and-swap capability
claims before I/O. Publication failure leaves the immutable snapshot unchanged.
The persisted loader requires immutable-node and isolated-snapshot-read
capabilities, then verifies each reachable content address before strict point
decoding. It rejects malformed envelopes, alternate encodings, invalid points,
wrong depth or path, duplicate references, missing nodes, resource overflow,
and any mismatch with the independently rebuilt mathematical root or canonical
root-node address. It closes the read view exactly once and returns no snapshot
when read, reconstruction, cancellation, or close fails. The adapter remains
trusted to honor its asserted capabilities; only adapter crash and isolation
tests can prove those guarantees. The audit boundary requires an isolated
complete inventory, verifies every current and retained root before classifying
nodes, requires strictly ascending bounded inventory pages, and never decodes
unreachable bytes. Returned publication capacity and its normalized copy are
charged together. Each adapter page bound is reduced to remaining temporary
memory before I/O, including result-buffer growth and defensive copying, and
hidden returned capacity is rejected. This limits
attacker-controlled amplification while
identifying debris outside all valid snapshots. A dishonest adapter can still
omit inventory entries unless its asserted complete-inventory capability is
independently tested. The audit alone never authorizes deletion.

The maintenance boundary mitigates stale-audit deletion and retained-root loss
by rejecting a missing or mismatched store-namespace profile before audit I/O,
independently reopening and verifying the complete isolated view, accepting
only a canonical subset of observed retained publications, retaining the
current publication unconditionally, and deriving deletion from the complete
inventory against the current plus desired roots. The store contract requires
that the entire inventory namespace belong exclusively to that profile, so
another profile's nodes cannot legitimately enter the deletion calculation. It
verifies dropped roots before planning, closes the view before mutation, and
sends the exact profile, current publication, previous retained set, desired
retained set, and deletion IDs through one opaque atomic request. The adapter
must compare the complete publication set and either apply the entire request
or leave storage unchanged, including for a no-op plan. It must preserve nodes
needed by pre-existing read snapshots, potentially through deferred
reclamation. The package cannot detect an adapter that lies about namespace
scope, inventory completeness, atomic compare/delete behavior, or snapshot
lifetime; adapter crash-point, concurrency, and recovery tests remain required
before its guarantees can be trusted.

The canonical claim-set boundary additionally rejects duplicate and conflicting
claimed keys, preserves present-zero and claimed-absence distinctions, and
removes caller-order and aliasing ambiguity before proof construction. Because
it carries no root, path, transcript, or opening, accepting a claim set provides
no authentication and does not mitigate proof replay or omitted-path attacks.

Snapshot proof-material assembly mitigates mixed-snapshot reads by deriving the
root, actual membership or absence claims, terminal stem topology, and required
commitments from one immutable committed arena. It rejects duplicate keys,
normalizes caller order, deduplicates shared commitment paths, and accounts
aggregate node reads and path work before extraction. The result remains
structural material only: without a transcript and valid aggregate opening it
does not authenticate any claim or commitment.

The unverified tree-proof container binds a claim set to an exact root, one
terminal topology result per queried stem, every required non-root commitment
path, and one strict raw opening payload. It rejects omitted, duplicate, surplus,
or conflicting stem and path metadata, including missing-child membership and
inconsistent shared paths. Construction is deterministic, immutable,
cancellation-aware, and resource-bounded. It does not construct or verify the
transcript or opening equations, so acceptance still provides no
cryptographic authentication and does not prevent a producer from supplying
mathematically false but structurally valid commitments.
Its strict decoder rejects the wrong profile before point decoding, alternate
or inconsistent lengths, trailing bytes, nonzero fixed-width path padding,
invalid claim and topology tags, misplaced empty-vector markers, malformed or
identity commitment encodings, malformed opening points or scalars, and
non-canonical reconstructed ordering. Canonical identity opening points remain
permitted only inside the aggregate payload and still require cryptographic
verification. It
preflights aggregate proof bytes, record counts, derived paths, retained path
bytes, point and scalar decodes, and conservative temporary memory before
cryptographic decoding or attacker-amplified allocation. Cancellation from
nested cryptographic decoders remains distinguishable from malformed syntax,
and accepted state does not alias the hostile input.
For an explicit empty root, it accepts only absence claims with one depth-one
missing path per distinct stem and no non-root commitments. The proof engine
still requires an aggregate opening of the selected all-zero root-vector
positions plus a fixed nonzero anchor. A SHA-256 digest of the canonical root,
claims, and reconstructed openings is injected before the first transcript
challenge, so the otherwise trivial zero-vector proof cannot be replayed for a
different key set.

The internal proof engine mitigates false structural claims by deriving full
prover vectors from the immutable tree, independently reconstructing verifier
evaluations from the proof, requiring exact canonical agreement, and verifying
the aggregate opening under the fixed generator set and `verkle` transcript.
It binds the complete canonical statement digest and one fixed nonzero anchor,
consolidates identical commitment/index openings, and rejects conflicting
vectors or evaluations, changed roots or claimed values, malformed proofs,
partial opening sets, resource exhaustion, and cancellation observed by owned
work. Verification is independent from mutable tree state. The pinned backend
does not accept a context once aggregate proof arithmetic begins and chooses
`runtime.NumCPU()` workers internally; preflight and post-call cancellation
checks do not stop that in-flight work. This residual denial-of-service and
worker-control risk blocks production-backend approval.

### Resource exhaustion

- attacker-selected allocation, recursion, storage fan-out, point decoding,
  multi-scalar multiplication, worker count, queued work, or retained state;
- integer conversion, size, offset, and accounting overflow;
- cancellation that does not stop backend or worker activity; and
- small inputs causing disproportionate CPU, memory, or I/O.

### Ownership and concurrency

- caller slices or returned values aliasing mutable state;
- scratch buffers shared between concurrent operations;
- locks held across storage I/O, callbacks, channels, or cryptographic work;
- goroutine, timer, file, response, transaction, or iterator leaks; and
- writer races or store close racing with active operations.

### Supply chain and disclosure

- compromised dependencies, fixtures, generators, generated constants, or
  release artifacts;
- unverifiable source revisions or license ambiguity;
- secrets, complete values, keys, proofs, or witnesses in errors, logs, fuzz
  artifacts, benchmarks, or mutation reports; and
- performance claims that omit validation, encoding, ownership copying, storage
  work, or incompatible semantics.

## Required control evidence

Before release, each attack class needs positive, negative, boundary, fuzz,
mutation, and independent differential evidence where mathematically
applicable. Concurrent and lifecycle controls also need race, stress, leak, and
failure-injection evidence. Storage guarantees need crash-point evidence
against each adapter that claims durability.

Cryptographic audit status, assumptions, generator provenance, supported
platforms, side-channel limits, and every residual risk must be public.
