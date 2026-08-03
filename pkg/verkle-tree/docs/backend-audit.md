# Commitment Backend Audit

## Decision

`github.com/crate-crypto/go-ipa` is approved only for the current internal,
pre-v1 canonical-encoding and bounded serial vector-commitment research
boundary. It is rejected as the production commitment backend until the
blockers below are resolved and re-audited.

The imported module version is
`v0.0.0-20240223125850-b1e8a79f509c`, corresponding to commit
`b1e8a79f509c5dd26b44d64c5f4aff67d7e69ed0`.

The upstream branch was refreshed again on 2026-08-03 and remains at
`53bbb0ceb27adb011950fd0fce885ad6d4516f84`. Its only later commit updates
transitive module versions; it does not resolve the production blockers below
or provide a tagged release.

The same refresh found no maintained drop-in Go replacement. The current
`sila-chain/go-verkle` repository adds one branding commit on top of the pinned
`ethereum/go-verkle` history and still requires the same `crate-crypto/go-ipa`
pseudo-version. The current `stark-verkle/verkle` repository implements a
different BabyBear/Poseidon/FRI construction and writes its cryptographic
primitives in the tree repository, so it cannot back this profile or satisfy
the rule against implementing the commitment arithmetic in the tree package.
Both rejected candidates are revision- and checksum-pinned in the source
manifest. Neither changes the production-backend decision.

The maintained `mratsim/constantine` repository was also reviewed at commit
`ca0006c7fb02ef034f8fce2257e0b8dcb23b5afb`. It contains Banderwagon,
Ethereum-Verkle transcript, IPA, and multiproof primitives and is dual licensed
MIT or Apache-2.0. It is not presently an adoptable backend: its public support
matrix marks Ethereum Verkle IPA as under construction in Nim and unavailable
through C, Rust, and Go; its planning document still requires finishing the IPA
test suite; and the repository's security statement says it remains unaudited
and best effort. No Verkle or IPA operation is exported from its C headers,
Rust bindings, or Go package at the reviewed revision. Adopting it would
therefore require this package to design and maintain a new foreign-function
interface around unfinished cryptographic internals, which does not satisfy the
maintained, independently reviewable, replaceable-backend requirement. Exact
source, tree, license, API, implementation, transcript, README, and planning
checksums are recorded in the source manifest so this decision can be
revisited without relying on a moving branch.

Two newer maintained repositories also fail the selection boundary for
different, independently verified reasons:

- MegaETH's `salt` is an active, released Rust authenticated store with
  Banderwagon and IPA multipoint code descended from the migrated
  `rust-verkle` history. Its tree is SALT's fixed four-level trie over dynamic
  hash-table buckets, not this package's profile, and the reviewed tree exports
  no Go package, C header, or FFI binding for its commitment backend. Adopting
  it would require designing and maintaining a new foreign-function boundary,
  while counting it as independent interoperability would duplicate the Rust
  cryptographic lineage.
- `luxfi/crypto` actively develops a Go `verkle` package and Banderwagon code,
  but that package identifies its tree logic as vendored from `go-verkle` and
  the repository's Lux Ecosystem License 1.2 forbids general commercial use
  and unauthorized forks. Its separately tagged `github.com/luxfi/crypto/ipa`
  v1.2.4 module retains MIT/Apache licensing, but its parallel executor is
  byte-for-byte identical to the reviewed `crate-crypto/go-ipa` executor and
  does not remove the CPU-derived worker, cancellation, initialization, unsafe
  surface, or independent-lineage blockers.

Both scans are pinned with exact repository and module revisions, trees,
licenses, source hashes, and review procedures in the source manifest. They
remain useful research inputs, but neither is an adoptable production backend
or the maintained independent implementation required to freeze v1.

Three maintained BLS12-381 KZG implementations were reviewed as possible ways
to leave the unmaintained Banderwagon IPA line. They improve maintenance and
audit evidence, but none exposes the complete Verkle backend required here:

- `gnark-crypto` v0.20.1 provides released Apache-2.0 KZG primitives and strict
  checked decoding paths. Its public batch opener aggregates polynomials only
  at one shared evaluation point, while tree queries require openings at many
  positions. Producing one compact proof for those queries would require this
  package to implement an additional multipoint polynomial-commitment protocol.
  Its KZG paths also accept no context, start goroutines internally, derive
  parallelism from `runtime.NumCPU`, and expose mutable SRS structures plus
  explicitly unsafe decoders.
- `crate-crypto/go-eth-kzg` is active, tagged, Apache-2.0, and carries a 2025
  PeerDAS audit. Its API is fixed to 4096-evaluation EIP-4844 blobs and
  EIP-7594 cells. Arbitrary-point proofs remain one proof per opening, and the
  cell batch verifier consumes one proof per cell rather than one compact tree
  multiproof. Public work accepts worker counts but no context; context setup
  performs seconds of preprocessing, starts one goroutine per setup point, and
  contains panic paths and mutable exported variables. The reviewed head is
  newer than the latest v1.5.0 release, so its pooling changes also lack a
  tagged release boundary.
- `ethereum/c-kzg-4844` is active, tagged, Apache-2.0, single-threaded, and has
  independent 2023 and 2025 audits. Its supported proof API is deliberately
  fixed to EIP-4844 and EIP-7594 and likewise exposes no compact arbitrary
  multi-position tree opening. The official Go binding uses process-global
  mutable setup state, panics on setup lifecycle errors, crosses `unsafe` and
  cgo, and accepts no context or per-operation resource budget.

KZG therefore remains a viable construction family, not a selected backend.
Adopting any reviewed implementation would require either changing to a
different fully specified profile with independent tree interoperability or
authoring the missing compact multipoint protocol, setup boundary, and
cancellation semantics. The latter would violate the rule against implementing
polynomial-commitment arithmetic in this tree package. Exact revisions,
release deltas, source hashes, licenses, audits, and review procedures are
pinned in the source manifest.

The resolved graph deliberately overrides that module's stale requirements
with `gnark-crypto` `v0.20.1`, `x/sync` `v0.22.0`, and `x/sys` `v0.47.0`.
This composition is accepted for the canonical encoding seam, strict
profile-bound root decoding, the internal experimental commitment engine, the
strict standalone commitment decoder, strict raw aggregate-opening-proof
decoder, strict internal tree-proof decoder, fixed-profile aggregate opening
and verification, and the pinned proof corpus exercised here. It is not
evidence that untested hostile-input behavior, dependency-level cancellation,
side-channel behavior, or production suitability remain compatible.

## Evidence

At the pinned revision:

- the upstream unit suite and race suite pass;
- `go vet ./...` passes;
- the complete upstream unit, race, and vet suites also pass when selected
  through this module's dependency overrides;
- five scalar and generator-multiple encodings generated by the independently
  pinned Rust implementation agree byte-for-byte with the accepted Go scalar
  and commitment encoding seam;
- five non-identity generator-multiple commitments map to the same canonical
  scalar bytes in the pinned Go and Rust implementations, and both pinned
  sources define the internal identity image as zero;
- the ordered 256-point generator sets independently derived by the pinned Go
  and Rust implementations from `eth_verkle_oct_2021` have the same
  canonical-encoding digest;
- zero, first and last one-hot, sparse boundary, and dense width-256 vectors
  produce the same commitments and commitment-to-field images in the bounded
  Go engine and independently pinned Rust implementation;
- the Go engine accepts only fixed-size vectors of canonical scalars, checks
  declared scalar, group-operation, generator, and scratch budgets before
  amplified work, and performs commitment terms serially with deterministic
  cancellation checkpoints;
- one deterministic three-opening corpus and one single zero-evaluation corpus
  produce the same canonical 576-byte aggregate proofs through both
  implementations, and the Go verifier accepts both Rust proofs;
- the internal raw-proof decoder requires exactly 576 bytes, validates all 17
  points and the final little-endian scalar canonically, accepts the all-zero
  identity representation only in proof-point positions where the IPA equation
  permits it, rejects malformed and wrong-subgroup points, owns accepted bytes,
  and enforces byte, point-decode, scalar-decode, and cancellation bounds
  before amplified work;
- the Go verifier rejects one-bit mutations in every serialized proof element,
  a wrong transcript label, and a wrong opened value for that corpus;
- point decoding checks canonical base-field encoding, curve membership, and
  the Banderwagon subgroup condition;
- scalar decoding provides a canonical little-endian field decoder;
- the fixed root container rejects profile and encoding mismatches before point
  decoding, represents an empty root without an identity point, and bounds
  bytes and point decodes;
- the standalone commitment decoder enforces caller-declared byte and point
  budgets around strict canonical non-identity point decoding; and
- the internal tree-proof decoder rejects profile mismatches, alternate
  lengths, trailing bytes, nonzero path padding, invalid record semantics, and
  malformed root, path-commitment, or aggregate-opening encodings under
  aggregate byte, count, path, decode, scratch, and cancellation limits;
- the 256-point generator set is deterministically derived from
  `eth_verkle_oct_2021`; and
- upstream fixes at the pinned commit distinguish trusted uncompressed
  serialization from the checked compressed path.

The module-local dependency review also found:

- all resolved dependency licenses are Apache-2.0, BSD-3-Clause, MIT, or the
  package's recorded dual-license choice;
- the secret scan reports no findings; and
- `govulncheck` reports no vulnerabilities in the resolved module graph.

These results establish useful research behavior for one positive proof
corpus. They do not establish production suitability, constant-time behavior,
transcript soundness, comprehensive negative-proof behavior, or the
package-level hostile-input contract.

The pinned Rust generator graph has no known vulnerability reported by
`cargo-audit` 0.22.2, but it retains the unmaintained `derivative` and `paste`
dependencies identified by RUSTSEC-2024-0388 and RUSTSEC-2024-0436. The
`banderwagon` and `ipa-multipoint` crate manifests also omit license
expressions even though the repository root carries pinned MIT and Apache-2.0
license files. This graph is accepted only as a reproducible research
generator and is rejected as a production dependency.

## Production Blockers

### Mutable cryptographic globals

The backend exports mutable `Generator`, `Identity`, curve parameters, and
related group values. Application code importing the same module can mutate
them before setup construction. The package cannot prove generator identity or
configuration immutability while those globals remain authoritative.

### Uncancellable and CPU-derived work

Generator derivation, setup precomputation, multi-scalar multiplication, and
multiproof aggregation do not accept `context.Context`. Several paths derive
goroutine counts from `runtime.NumCPU`, and setup precomputation uses
`context.Background`. The internal engine avoids the backend precomputation
and parallel multi-scalar paths: it derives the fixed set explicitly and
commits serially. It still cannot interrupt the one fixed-width generator
derivation call after it starts, so constructor cancellation is limited to
preflight and post-derivation checks.

The wrapper-control audit traced the complete proof call graph at the pinned
revision. `multiproof.CreateMultiProof` calls an unexported aggregation helper
that always starts `runtime.NumCPU()` goroutines. Prover folding and both
verification MSMs reach `ipa.MultiScalar`, which supplies
`banderwagon.MultiExpConfig{NbTasks: runtime.NumCPU()}` itself. Setup reaches
`banderwagon.NewPrecompPoint`, whose errgroup uses `context.Background()` and a
`runtime.NumCPU()` limit; its normalization helpers also use the package's
parallel executor, whose default is `runtime.NumCPU()`. None of the exported
multiproof or IPA proof entry points accepts a context or worker configuration.
`IPAConfig` embeds the concrete precomputation type rather than an injectable
bounded operation. A wrapper goroutine that returned on cancellation would
therefore abandon live dependency CPU and workers, not cancel them. Replacing
these calls would require maintaining a fork or reimplementing the IPA/MSM
path, neither of which satisfies the selected-backend requirements.

The package now serializes entry into that uncancellable dependency boundary
per proof-engine instance. `MaxWorkers` must cover the dependency's fixed
`runtime.NumCPU()` demand, and `MaxQueuedOperations` bounds callers waiting for
the one active slot; zero rejects concurrent work rather than queueing it.
Waiting observes cancellation without starting dependency work, and queue
overflow is a typed resource failure. This prevents one engine from multiplying
dependency workers through concurrent calls. It does not stop a call already
inside the dependency, bound independently constructed engines, or turn the
backend's CPU-derived worker choice into caller-controlled scheduling.

### Unsafe public surface

The dependency publicly exposes unchecked or trusted decoding operations and
raw group, field, transcript, setup, and proof types. The public `verkletree`
API does not re-export any of them.

### Initialization and mutable precomputation

Field and square-root packages initialize mutable lookup tables and parameters
through package initialization. The tree goal forbids hidden setup generation
and mutable global registries at package initialization.

### Verification robustness

Proof APIs accept pointer-rich inputs without package-level nil, size, work,
or cancellation budgets. The tree boundary must validate every count and
encoding before invoking proof verification and must convert malformed input
into typed errors without panic.

### Side-channel and maintenance evidence

The reviewed source does not state a complete constant-time contract. The
selected pseudo-version is untagged, and the upstream repository has not
published the maintenance, audit, vulnerability, or release evidence required
for a production cryptographic dependency.

The reviewed arithmetic also contains observable data-dependent control flow:
fixed-base MSM skips zero scalars, precomputed scalar multiplication branches
on scalar windows and indexes lookup tables by those windows, and generic MSM
partitions and schedules work using scalar values. The package's serial
research commitment path likewise skips zero vector entries. Consequently the
experimental implementation does not claim to hide key, value, vector-sparsity,
or witness information from a same-host timing or cache observer. Production
selection must define the confidentiality scope and either supply reviewed
constant-time operations for secrets in that scope or explicitly constrain the
profile to public inputs with an independently reviewed rationale.

### Override compatibility scope

Upstream `go-ipa` has not tested or released the dependency combination used by
this module. The module's unit, race, fuzz, and mutation evidence covers the
canonical point and scalar encoding seam only. Production reconsideration must
revalidate all setup, arithmetic, commitment, opening, and verification
operations against the final dependency graph.

## Accepted Internal Boundary

The current internal boundary may:

- decode exactly 32-byte compressed Banderwagon commitments;
- reject non-canonical, off-curve, wrong-subgroup, and identity commitment
  encodings while accepting the canonical all-zero identity only inside opaque
  aggregate-proof point positions;
- decode exactly 32-byte canonical little-endian scalars;
- map an already validated non-identity commitment to its canonical scalar
  field image;
- explicitly derive the fixed 256-point `eth_verkle_oct_2021` generator set
  and reject a set whose ordered canonical digest differs from the pinned
  independent fixture;
- commit a fixed-width vector of canonical scalar encodings through bounded,
  deterministic serial group operations;
- apply a bounded, defensively owned, canonically ordered sparse scalar delta to
  an opaque commitment after the caller has authenticated every old value;
- retain the resulting identity only as an opaque in-memory commitment and
  map it to scalar zero;
- return one canonical encoding for accepted commitments and scalars; and
- defensively copy caller bytes before dependency decoding; and
- strictly decode canonical commitments where aggregate resource preflight has
  already authorized one point operation;
- initialize the exact pinned IPA settings after generator, precomputation,
  scratch-memory, and dependency-worker preflight;
- create aggregate openings only for complete canonical width-256 vectors
  whose recomputed commitments match; and
- verify complete canonical opening sets under the fixed `verkle` transcript;
  package-owned bound operations additionally inject a canonical SHA-256
  statement digest and one fixed nonzero anchor opening before proof work.

It may decode only the fixed raw aggregate-proof payload described by the
experimental profile and the package-owned internal unverified tree-proof
container that embeds it. It may construct and verify openings only through the
fixed internal profile boundary. It must not accept a serialized identity as a
root, node, path, or standalone commitment, expose dependency values outside
`internal/`, accept caller-selected
transcripts or generators, or claim cancellation once a dependency proof call
has begun. The unbound raw opening methods exist only to reproduce the pinned
interoperability corpora; the package-owned proof engine uses only the bound
statement-and-anchor methods.

The engine's generator, query, scalar, multi-scalar-multiplication, worker, and
scratch-byte accounting is a deterministic conservative package budget. The
pinned independent sparse-boundary commitment is reproduced both through full
commitment and through the sparse-update path. That agreement proves only the
group-arithmetic update primitive: it does not authenticate supplied old
scalars, establish tree-path completeness, or constitute a stateless witness.
The package-owned internal stateless updater separately authenticates old
scalars for present paths and terminal missing/different paths for new stems
through its verified tree proof before composing commitment operations. The
public canonical witness format composes that authenticated operation without
expanding the backend interoperability claim beyond the pinned corpora.
The pinned proof implementation uses `runtime.NumCPU()` internally and accepts
no context, so the wrapper rejects insufficient worker budgets beforehand,
admits only one dependency proof call per engine, bounds waiting calls, and can
check cancellation only before and after the in-flight call. This does not
prove the dependency's complete heap allocation profile or constant-time
behavior. The engine therefore remains an experimental internal component
rather than an approved production backend.

The separate `internal/leafvector` boundary performs dependency-free,
fixed-size byte decomposition only. It produces canonical scalar bytes that are
mathematically below the scalar modulus; it does not decode dependency values
or broaden this backend approval.

## Reconsideration Gate

Production selection requires either an upstream revision or a separately
reviewable maintained backend that removes mutable cryptographic globals,
provides bounded context-aware work, narrows unsafe APIs, documents
side-channel behavior, and passes the complete differential, fuzz, mutation,
license, vulnerability, and provenance gates.
