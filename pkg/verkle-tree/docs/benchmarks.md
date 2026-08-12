# Benchmarks

## Status and scope

The public pre-v1 benchmark matrix exercises complete package entry points for
ordered 32-entry root construction, immutable lookup, insert, update, delete,
mixed batch application, single membership and non-membership proof generation
and verification, eight-key aggregate proof generation and verification,
trusted-root and exact-key-set aggregate verification,
canonical proof encoding and decoding, truncated-proof rejection, and two-key
stateless-witness container construction, encoding, decoding, trusted-pre-root
verification, and application. Shared immutable snapshot reads and aggregate-proof
verification also have parallel measurements. The proof and witness rows
report their exact canonical encoded sizes.

Topology-changing stateless samples cover deleting one member while retaining
its stem, replacing an emptied stem, and collapsing a unary parent. Each case
measures both update-proof generation and complete witness verification with
post-state application.

A separate public initialization benchmark compares constructing a standalone
stateless engine with constructing one from an already initialized proof
engine. It isolates the explicit backend-ownership choice: the reuse path
initializes commitment arithmetic but does not repeat aggregate-opening setup.

The matrix uses the package-owned pre-v1 profile, an in-memory snapshot, and
caller-owned in-memory storage. It includes separate warm-store reconstruction
and cold-store materialization-plus-reconstruction tracks. It excludes durable
database or filesystem I/O, an audited backend, cross-implementation equivalent
workloads, latency distributions under stable load, and deployment-specific CPU
feature controls. The matrix satisfies the pre-v1 descriptive benchmark
boundary but does not support a production or comparative ranking claim.
Parallel rows demonstrate a bounded harness workload on one machine; they are
not scalability claims.

The remaining pre-v1 component microbenchmarks cover the implemented
cryptographic boundary: canonical Banderwagon commitment and scalar encoding,
strict raw aggregate-opening-proof decoding, strict profile-bound root
decoding, the commitment-to-field map, serial fixed-width vector commitment,
and internal aggregate tree-proof generation and verification. Two public
loader tracks use an in-memory test reader as described below. The suite
does not measure a production storage adapter or an equivalent cross-
implementation end-to-end workload, and it supports no comparative performance
claim.

One additional component benchmark measures rebuilding the implemented
immutable committed-node arena and mathematical root for the pinned four-entry,
two-stem corpus. It excludes builder and generator initialization, proof work,
serialization, persistence, and incremental updates. It is not an end-to-end
tree benchmark or a comparison with either reference implementation.

Two public whole-snapshot benchmarks measure canonical encoding of a retained
two-entry immutable snapshot and strict decoding followed by complete tree
reconstruction and root comparison. They exclude transport and persistence;
decode includes commitment construction because accepted bytes are not trusted
until the independently derived root matches.

A storage-image component benchmark measures canonical encoding, content
hashing, ownership copying, and content-address sorting for the same four-entry
corpus. It excludes adapter calls, durable writes, compare-and-swap
publication, persisted reads, recovery, and pruning. It therefore measures the
package-owned storage-write preparation boundary, not storage performance.

Two public persisted-load tracks open an in-memory caller-owned read view,
verify and decode every canonical node, reconstruct the complete immutable
four-entry tree, recompute the mathematical root and canonical root-node
address, and close the view. The warm track reuses the caller-owned publication
and node index. The cold track first materializes a fresh owned publication and
node index, including copies of all encoded nodes. Both tracks copy every node
returned across the package boundary. They have no database or filesystem I/O,
locking, durability, transaction, or recovery cost, so they measure the
package-owned cold/warm storage boundary rather than a particular adapter's
cache or durable-media behavior.

One public storage-audit benchmark verifies a current and one retained
in-memory snapshot, unions their reachable nodes, pages a complete mock node-ID
inventory, and identifies one unpublished node. The mock has no I/O, locking,
transaction, crash-recovery, retention-mutation, or deletion cost. The result
measures only package-owned validation, reconstruction, and inventory
classification.

One public storage-maintenance benchmark verifies the same current and retained
snapshots, drops the retained publication, classifies its exclusive nodes plus
one unpublished node for deletion, closes the isolated view, and hands the
opaque request to an in-memory atomic-maintenance mock. The mock records but
does not apply the request. The result includes package-owned validation,
inventory, canonical retention, deletion planning, ownership, and call-handoff
cost; it excludes transaction, compare-and-swap, locking, physical deletion,
deferred reclamation, durability, and crash-recovery cost.

One public storage-recovery benchmark verifies a current and retained in-memory
snapshot, preserves their exact publication set, identifies one unreachable
unpublished node, closes the isolated view, and hands the opaque request to an
in-memory atomic-maintenance mock. The mock records but does not apply the
request. The result includes package-owned validation, inventory, deletion
planning, ownership, and call-handoff cost; it excludes transaction, compare-
and-swap, locking, physical deletion, deferred reclamation, restoration,
durability, and adapter crash behavior.

Two authenticated-state component benchmarks measure an immutable lookup and a
single-value replacement that rebuilds a one-entry committed tree through an
already initialized snapshot builder. They exclude snapshot construction,
generator initialization, proofs, witnesses, serialization, and persistence.
Two further component benchmarks measure canonicalizing sixteen fixed-size tree
claims and returning an owned copy of the resulting claim set. They exclude
path construction, opening generation, proof encoding, and verification.
One snapshot proof-material benchmark derives eight membership claims, eight
missing-stem absence claims, their canonical topology, and their deduplicated
non-root commitments from one already constructed immutable snapshot. It
excludes snapshot construction, aggregate-opening generation, proof encoding,
verification, and persistence.
Two proof-container benchmarks measure canonical validation and ordering of
sixteen claims, sixteen stem paths, and sixteen non-root commitment paths, plus
owned copies of the retained stem and commitment metadata. They reuse an
already decoded raw opening payload and exclude opening generation,
cryptographic verification, and public interoperability. Three serialization
benchmarks measure canonical encoding, strict decoding, and wrong-length
rejection for the same internal unverified proof boundary.
Two proof-engine benchmarks measure generation and independent verification of
one sixteen-key membership proof through the complete internal snapshot,
query-reconstruction, aggregate-opening, and tree-proof container path. They
reuse initialized snapshot and proof engines and exclude setup, persistence,
witness processing, and public API ownership costs.
Three stateless-witness component benchmarks measure canonical encoding, strict
decoding, and verified post-state application for one present-key Set witness.
They reuse initialized proof and stateless engines and exclude witness
generation from a snapshot, storage, network transport, topology-changing
updates, and external interoperability. The apply benchmark includes complete
aggregate-proof verification and bottom-up commitment propagation.

The accepted-input benchmarks exclude fixture construction from the measured
loop. Rejection benchmarks measure the complete fail-closed decoder path,
including typed error construction. Every benchmark reports processed bytes,
allocations, and allocation bytes.

## Method

Command:

```console
GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublicSnapshotOperations$' \
  -benchmem -benchtime=20x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublicSnapshotOperations$/^get-present(-parallel)?$' \
  -benchmem -benchtime=100000x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublic(ProofOperations|StatelessWitnessOperations)$' \
  -benchmem -benchtime=1x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublicStatelessEngineInitialization$' \
  -benchmem -benchtime=1x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublicStatelessTopologyTransitions$' \
  -benchmem -benchtime=1x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^BenchmarkPublicProofOperations$/^verify-aggregate-8-parallel$' \
  -benchmem -benchtime=32x -count=5

GOWORK=off go test ./internal/backend -run '^$' \
  -bench '^(BenchmarkDecode|BenchmarkEncode|BenchmarkCommitmentToScalar|BenchmarkCommitVector)' \
  -benchmem -count=5

GOWORK=off go test ./internal/committedtree -run '^$' \
  -bench '^(BenchmarkBuildFourEntries|BenchmarkProofPath|BenchmarkStorageImage)$' \
  -benchmem -count=5

GOWORK=off go test ./internal/authstate -run '^$' \
  -bench '^(BenchmarkSnapshotGet|BenchmarkSnapshotApply|BenchmarkNewClaimSet|BenchmarkClaimSetCopy|BenchmarkSnapshotProofMaterial|BenchmarkNewTreeProof|BenchmarkTreeProofCopy|BenchmarkEncodeTreeProof|BenchmarkDecodeTreeProof)' \
  -benchmem -count=5

GOWORK=off go test ./internal/authstate -run '^$' \
  -bench '^BenchmarkProofEngine$' -benchmem -benchtime=1x -count=5

GOWORK=off go test ./internal/authstate -run '^$' \
  -bench '^(BenchmarkEncodeStatelessWitness|BenchmarkDecodeStatelessWitness|BenchmarkApplyStatelessWitness)$' \
  -benchmem -benchtime=1x -count=5

GOWORK=off go test . -run '^$' \
  -bench '^(BenchmarkEncodeSnapshotTwoEntries|BenchmarkDecodeSnapshotTwoEntries|BenchmarkLoadSnapshotFourEntries|BenchmarkAuditStorageCurrentAndRetainedSnapshots|BenchmarkMaintainStorageDropRetainedAndPrune|BenchmarkRecoverStoragePreserveRetainedAndDeleteOrphan)$' \
  -benchmem -count=5
```

Repository-local runs MUST give every independent command a fresh build cache.
The commands above were each executed inside this wrapper:

```console
(
  agent_gocache=$(mktemp -d "${TMPDIR:-/tmp}/golib-gocache.XXXXXX")
  trap 'find "$agent_gocache" -depth -delete' EXIT HUP INT TERM
  export GOCACHE="$agent_gocache"

  # One command from above.
)
```

Process peak memory was sampled separately so compiler and linker memory did
not contaminate the result:

```console
(
  agent_gocache=$(mktemp -d "${TMPDIR:-/tmp}/golib-gocache.XXXXXX")
  agent_benchdir=$(mktemp -d "${TMPDIR:-/tmp}/golib-verkle-peaks.XXXXXX")
  trap 'find "$agent_gocache" "$agent_benchdir" -depth -delete' EXIT HUP INT TERM
  export GOCACHE="$agent_gocache"

  GOWORK=off go test -c -o "$agent_benchdir/verkle-peaks.test" .
  /usr/bin/time -l "$agent_benchdir/verkle-peaks.test" \
    -test.run '^$' \
    -test.bench '^BenchmarkPublicProofOperations$/^generate-aggregate-8$' \
    -test.benchmem -test.benchtime=1x -test.count=1
)
```

Repeat the timed binary invocation with
`^BenchmarkPublicSnapshotOperations$/^construct-ordered-32$` and
`^BenchmarkPublicStatelessWitnessOperations$/^verify-and-apply-2$` for the
other named workloads. Run it with only `-test.run '^$'` for the empty-binary
baseline.

Environment:

- Date: 2026-08-01; public API, bound proof-engine, stateless-witness, and
  canonical whole-snapshot rows refreshed 2026-08-03; stateless-engine
  initialization, trusted-expectation verification, and topology-transition
  rows added or refreshed 2026-08-08; cold/warm caller-store, storage-audit,
  storage-maintenance, and storage-recovery rows refreshed 2026-08-12
- Go: `go1.26.5`
- OS: macOS 27.0 (`26A5388g`)
- Architecture: `darwin/arm64`
- CPU: Apple M4 Max
- `GOMAXPROCS`: Go default, reported by the benchmark suffix as `16`
- Backend: `github.com/crate-crypto/go-ipa`
  `v0.0.0-20240223125850-b1e8a79f509c`

The Go benchmark harness calibrates each sample independently. No latency
threshold is enforced because no stable cross-runner baseline exists yet.
Reproduce measurements on the target deployment hardware before using them for
capacity planning.

The 2026-08-03 public API, proof, and witness refresh ran while an unrelated
repository mutation job shared the host, as required by the non-blocking
verification workflow. The raw samples retain the resulting scheduling spikes
and MUST NOT be used as a regression baseline or comparison result.
The parallel aggregate-verification row was rerun after engine-local dependency
admission and queue bounds were added; race, coverage, and mutation work also
shared the host during that sample.

## Raw samples

The values below are the five samples emitted by the command above. Times are
nanoseconds per operation.

### Public API samples

| Benchmark | ns/op samples | ops/s samples | Canonical bytes | B/op | allocs/op |
| --- | --- | --- | ---: | ---: | ---: |
| Construct ordered 32-entry root | 9548017, 9420452, 9332735, 9534233, 10015558 | 104.7, 106.2, 107.1, 104.9, 99.84 | - | 201966-202040 | 3845-3846 |
| Get one present value | 55.04, 55.26, 54.76, 55.02, 54.45 | 18167090, 18097091, 18262338, 18174383, 18363787 | - | 0 | 0 |
| Get one present value in parallel | 24.31, 12.28, 8.300, 10.56, 12.60 | 41143104, 81455380, 120609076, 94719394, 79367725 | - | 0 | 0 |
| Insert a suffix into an existing stem | 1597567, 1618040, 1625673, 1522285, 1787048 | 626.0, 618.0, 615.1, 656.9, 559.6 | - | 37436-37458 | 346 |
| Insert a new stem | 2660154, 2127231, 1617529, 1645096, 1633638 | 375.9, 470.1, 618.2, 607.9, 612.1 | - | 39152-39164 | 358 |
| Update one present value | 1614081, 1521215, 1543675, 1472765, 1491229 | 619.5, 657.4, 647.8, 679.0, 670.6 | - | 35728-35740 | 333 |
| Delete one present value | 1467727, 1520560, 1405171, 1439740, 1473419 | 681.3, 657.7, 711.7, 694.6, 678.7 | - | 35320-35354 | 329 |
| Delete one absent value | 1408467, 1588312, 1461646, 1924758, 1470738 | 710.0, 629.6, 684.2, 519.5, 679.9 | - | 35728-35740 | 333 |
| Apply mixed 16-update batch | 1953133, 1974062, 2031648, 1965502, 2001744 | 512.0, 506.6, 492.2, 508.8, 499.6 | - | 51240-51252 | 469 |
| Generate one-key membership proof | 31226458, 25259333, 41684375, 20144792, 18077833 | 32.02, 39.59, 23.99, 49.64, 55.32 | - | 1532104-1534064 | 4854-4886 |
| Verify one-key membership proof | 2236125, 2060250, 2253208, 2158625, 2173125 | 447.2, 485.4, 443.8, 463.3, 460.2 | 898 | 287256-287944 | 1042-1055 |
| Generate one-key non-membership proof | 16698000, 16422708, 16451709, 16217083, 16167375 | 59.89, 60.89, 60.78, 61.66, 61.85 | - | 1550512-1551064 | 4887-4895 |
| Verify one-key non-membership proof | 2108917, 2269334, 2120041, 2032500, 2098542 | 474.2, 440.7, 471.7, 492.0, 476.5 | 898 | 287680-287928 | 1048-1053 |
| Generate eight-key aggregate proof | 17427833, 16802209, 16720167, 16717833, 16652791 | 57.38, 59.52, 59.81, 59.82, 60.05 | 2193 | 5011040-5014784 | 4981-5010 |
| Verify eight-key aggregate proof | 2289875, 2249208, 2236084, 2176417, 2199334 | 436.7, 444.6, 447.2, 459.5, 454.7 | 2193 | 421040-421536 | 1078-1088 |
| Verify eight-key aggregate proof for trusted root and keys | 8976958, 5308917, 5922000, 4335333, 6029292 | 111.4, 188.4, 168.9, 230.7, 165.9 | 2193 | 324848-325360 | 1081-1092 |
| Verify eight-key aggregate proof in parallel | 5385238, 5362990, 8107587, 6090237, 7293846 | 185.7, 186.5, 123.3, 164.2, 137.1 | 2193 | 416229-416265 | 1072-1073 |
| Encode eight-key aggregate proof | 18666, 15791, 15000, 12959, 16459 | 53573, 63327, 66667, 77166, 60757 | 2193 | 2304 | 1 |
| Decode eight-key aggregate proof | 248541, 256959, 242875, 243292, 239709 | 4023, 3892, 4117, 4110, 4172 | 2193 | 25768-25864 | 40-42 |
| Reject truncated aggregate proof | 14375, 6500, 6375, 5458, 10417 | 69565, 153846, 156863, 183217, 95997 | 2192 input | 2976-3040 | 24-25 |
| Construct two-update witness container | 27792, 6833, 17083, 6041, 5083 | 35982, 146349, 58538, 165536, 196734 | 1217 | 432 | 3 |
| Encode two-update witness | 17833, 10834, 15250, 14667, 18417 | 56076, 92302, 65574, 68180, 54298 | 1217 | 2432 | 2 |
| Decode two-update witness | 211708, 201750, 201875, 203041, 202542 | 4723, 4957, 4954, 4925, 4937 | 1217 | 7816 | 37 |
| Verify and apply two-update witness | 2239084, 2198208, 2141375, 2148125, 2148250 | 446.6, 454.9, 467.0, 465.5, 465.5 | 1217 | 312512-312528 | 1119 |
| Initialize standalone stateless engine | 284504167, 334086750, 322192166, 671019375, 239801375 | 3.515, 2.993, 3.104, 1.490, 4.170 | - | 833184080-833217712 | 235761-235890 |
| Initialize stateless engine from proof engine | 7763375, 7833708, 8752375, 7720792, 7823042 | 128.81, 127.65, 114.25, 129.52, 127.83 | - | 166544-166824 | 3517-3523 |
| Generate retained-member delete proof | 25674000, 25313041, 28543416, 49260500, 34791375 | 38.95, 39.51, 35.03, 20.30, 28.74 | 1087 witness | 1079696-1081744 | 4875-4912 |
| Verify and apply retained-member delete | 7107042, 8009542, 5044000, 6255000, 6966125 | 140.7, 124.9, 198.3, 159.9, 143.6 | 1087 witness | 282912-283520 | 1087-1098 |
| Generate emptied-stem replacement proof | 42480250, 61070000, 38492583, 33765750, 43077166 | 23.54, 16.37, 25.98, 29.62, 23.21 | 17856 witness | 8235200-8237064 | 5790-5823 |
| Verify and apply emptied-stem replacement | 10411041, 19402917, 15148375, 19550125, 19554583 | 96.05, 51.54, 66.01, 51.15, 51.14 | 17856 witness | 1160848-1161784 | 1268-1285 |
| Generate unary-parent collapse proof | 47687542, 55102667, 45004958, 54773125, 96688000 | 20.97, 18.15, 22.22, 18.26, 10.34 | 50752 witness | 11471576-11473112 | 7120-7146 |
| Verify and apply unary-parent collapse | 11051958, 10129750, 12737042, 13999542, 16536083 | 90.48, 98.72, 78.51, 71.43, 60.47 | 50752 witness | 1881160-1882016 | 1521-1537 |

The parallel rows use the Go benchmark harness at `GOMAXPROCS=16`; ns/op is
aggregate wall time divided by completed operations, not per-goroutine latency.
The non-parallel cryptographic samples use one measured iteration and
deliberately expose host scheduling variance. The parallel proof row uses 32
operations so the harness can schedule concurrent calls; the engine serializes
their entry into the dependency proof boundary and bounds the waiting queue.
The row therefore measures admission contention as well as verification and is
not backend parallel-scaling evidence. Neither set is suitable for percentile
or regression claims.

The initialization rows exclude construction of the proof engine supplied to
the reuse case. They measure the marginal cost faced by a caller that already
owns that immutable engine. The standalone row includes a complete additional
aggregate-opening setup; both rows include commitment-backend initialization.

### Process peak-memory samples

The benchmark test binary was built once and each selected benchmark was run in
a fresh process under `/usr/bin/time -l` with `-benchtime=1x`. The result is
whole-process maximum resident set size, including benchmark fixture and
backend initialization performed outside the timed loop. It is not
per-operation scratch memory and cannot be subtracted safely from `B/op`.

| Process workload | Maximum resident set bytes |
| --- | ---: |
| Empty benchmark binary baseline | 6619136 |
| Ordered 32-entry construction | 9584640 |
| Eight-key aggregate proof generation | 514834432 |
| Two-update witness verification and application | 582811648 |

The proof process constructs its snapshot and proof engine before the measured
loop. The witness process additionally constructs its update proof, post-state,
witness, and stateless engine. These high-water marks expose the current
pinned backend's initialization and fixture footprint; they MUST NOT be
presented as the incremental memory cost of the named operation.

### Component samples

| Benchmark | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| Decode canonical commitment | 26570, 22148, 25850, 28067, 11645 | 32 | 1 |
| Reject identity commitment | 286.4, 304.8, 214.2, 168.1, 241.0 | 64 | 2 |
| Encode commitment | 79.31, 57.34, 31.62, 50.70, 33.85 | 0 | 0 |
| Map commitment to scalar | 1474, 1392, 2558, 1942, 3718 | 8 | 1 |
| Decode canonical scalar | 77.17, 73.11, 70.89, 74.57, 74.84 | 0 | 0 |
| Reject non-canonical scalar | 309.7, 375.0, 215.9, 195.7, 176.3 | 176 | 5 |
| Encode scalar | 13.64, 23.18, 10.80, 11.81, 19.64 | 0 | 0 |
| Decode committed 42-byte root | 9574, 7986, 8663, 8545, 7773 | 32 | 1 |
| Decode explicit empty 42-byte root | 20.94, 26.36, 25.16, 21.12, 25.21 | 0 | 0 |
| Reject 41-byte root | 159.1, 139.5, 165.8, 131.1, 128.1 | 80 | 2 |
| Decode canonical 576-byte raw opening proof | 429197, 110929, 115482, 111094, 107296 | 544 | 17 |
| Reject 575-byte raw opening proof | 77.65, 78.30, 81.36, 79.81, 79.23 | 80 | 2 |
| Commit sparse five-term vector | 108785, 69867, 64012, 231937, 70566 | 1321 | 20 |
| Commit dense 256-term vector | 4835446, 16280795, 15844953, 2671274, 2909500 | 67632-67655 | 1024 |
| Build four-entry, two-stem committed root | 504199, 500008, 447677, 1315124, 1282870 | 7450-7452 | 89 |
| Extract one immutable committed-tree proof path | 3765, 3824, 4398, 4883, 6526 | 4864 | 1 |
| Encode and content-address four-entry storage image | 9049, 9001, 10835, 13112, 13058 | 1440 | 8 |
| Encode canonical two-entry whole snapshot | 1429, 1358, 1543, 1520, 1478 | 320 | 2 |
| Decode and independently rebuild two-entry whole snapshot | 9041426, 10959098, 10006862, 8807812, 8367261 | 168125-168148 | 3574-3575 |
| Warm caller store: load and independently reconstruct four-entry persisted snapshot | 8443332, 8408506, 10614097, 8393489, 8371986 | 174873-174935 | 3630-3631 |
| Cold caller store: materialize, load, and independently reconstruct four-entry persisted snapshot | 8694073, 9045159, 8255835, 8693603, 9582935 | 175967-176041 | 3635-3636 |
| Audit current and retained snapshots plus one unreachable node | 16053079, 16160084, 16058963, 16020784, 16218618 | 338952-339101 | 7178-7179 |
| Drop one retained snapshot and plan pruning plus atomic handoff | 17443835, 17336738, 16890330, 19827156, 16562481 | 339268-339331 | 7180-7181 |
| Preserve all publications and plan interrupted-write cleanup | 17170816, 20442187, 16197336, 17251313, 19039382 | 335119-335154 | 7141-7142 |
| Get one present snapshot value | 22.06, 23.42, 22.77, 21.85, 20.84 | 0 | 0 |
| Replace one value and rebuild its committed root | 355831, 219311, 199943, 165296, 152352 | 2860 | 37 |
| Canonicalize sixteen tree claims | 2886, 1112, 1013, 1374, 1169 | 2304 | 2 |
| Copy sixteen canonical tree claims | 211.9, 241.0, 356.6, 728.3, 300.0 | 1152 | 1 |
| Assemble sixteen-key snapshot proof material | 186928, 54189, 59131, 61148, 50293 | 161793-161800 | 32 |
| Canonicalize sixteen-claim unverified tree proof | 33948, 46259, 33676, 31726, 31940 | 50816-50818 | 7 |
| Copy sixteen-claim tree-proof metadata | 1306, 1680, 3757, 2622, 2787 | 3456 | 2 |
| Encode canonical unverified tree proof | 5136, 16481, 16351, 13961, 6149 | 1024 | 1 |
| Decode canonical unverified tree proof | 253440, 627923, 368237, 277831, 307604 | 4354-4355 | 30 |
| Reject wrong-length encoded tree proof | 474.3, 374.8, 377.9, 267.9, 338.3 | 96 | 2 |
| Generate sixteen-key aggregate tree proof | 43424750, 30970417, 28593083, 32713125, 51248250 | 10574424-10576752 | 5191-5229 |
| Verify sixteen-key aggregate tree proof | 4989750, 15851417, 11878209, 8317041, 10396709 | 635072-635744 | 1146-1158 |
| Encode one-update canonical stateless witness | 15125, 13625, 14875, 22375, 7625 | 2048 | 2 |
| Decode one-update canonical stateless witness | 223916, 249625, 354125, 227458, 226417 | 4464 | 32 |
| Verify and apply one-update stateless witness | 3356666, 104793459, 202132792, 8603458, 23423500 | 286304-286728 | 1073-1080 |

These results are descriptive evidence for this source and environment, not a
portable performance guarantee. The vector samples show substantial local
variance and therefore are not suitable as a release threshold or comparative
ranking.
