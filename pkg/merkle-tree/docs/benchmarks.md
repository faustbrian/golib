# Benchmark methodology

## Owned benchmarks

`benchmark_test.go` measures tiny, medium, and large root construction;
streaming and proof-retaining append and batch append; streaming root reads;
inclusion, multi-inclusion, and consistency generation and verification;
proof encoding and malformed rejection; and snapshot marshal, validation, and
builder resumption. The package has no storage adapter or parallel constructor,
so no storage-I/O or owned parallel benchmark is claimed. Snapshot parse and
resume measure the storage-independent reconstruction boundary.

Run:

```sh
GOMAXPROCS=2 GOWORK=off go test -run '^$' -bench '^Benchmark' \
  -benchmem -benchtime=100ms -count=5
```

The Go benchmark harness reports latency, throughput where bytes are defined,
allocation bytes, and allocations. For stable capacity or regression
decisions, run on an otherwise idle pinned machine, retain every raw sample,
and compare with `benchstat` at its default 95% confidence level. Record
process peak RSS separately with the platform time tool.

## Required ecosystem audit

The following latest tagged modules were resolved on 2026-07-29:

| Module | Pinned version | Tagged |
|---|---:|---:|
| `github.com/transparency-dev/merkle` | v0.0.2 | 2023-05-05 |
| `github.com/cbergoon/merkletree` | v0.2.0 | 2019-08-21 |
| `github.com/txaty/go-merkletree` | v0.2.2 | 2023-11-24 |
| `github.com/wealdtech/go-merkletree/v2` | v2.6.1 | 2025-01-08 |

Pinned versions, revisions, and checksums are retained as test-only module
requirements in `go.mod`, `go.sum`, and the reference-fixture provenance. They
are not imported by production package files. Tag recency is reported rather
than asserting an unverified maintenance status.

## Semantic tracks

No end-to-end latency ranking is valid across these APIs:

| Track | Leaf and branch encoding | Non-power-of-two rule | Retained state and ownership |
|---|---|---|---|
| `merkle-tree-rfc9162` | SHA-256 with `0x00`/`0x01` domains | RFC recursive split | validates limits; accepts pre-owned copied `RawLeaf`; logarithmic root state |
| `transparency-dev-rfc6962` | same SHA-256 digest semantics | same split | borrows raw slices; benchmark test tree retains all nodes; no context or limits |
| `cbergoon-native` | caller SHA-256 leaf, unprefixed SHA-256 branches | duplicates odd nodes | retains content and pointer tree; no empty tree or resource policy |
| `txaty-native` | unprefixed SHA-256 leaves and branches | duplicates odd nodes per level | retains leaves, nodes, and lookup map; separate sequential and two-worker tracks |
| `wealdtech-native` | unprefixed SHA-256 leaves and branches | pads to a power of two with zero digests | retains data and complete padded node array |

Only `transparency-dev/merkle` produces the same RFC root and proof bytes, and
the conformance suite proves that interoperability independently. Its
construction benchmark is still a separate native track because input
ownership, retained nodes, validation, and proof capability cannot be
configured identically. The other libraries produce different roots for
nontrivial inputs. Publishing a speed ratio between these tracks would compare
unlike work.

The comparison corpus is deterministically ordered 32-byte leaves and uses
standard SHA-256, unsorted siblings, and no salt. Construction is single
threaded except the explicitly named txaty two-worker track. Fixture setup is
outside the timed loop; each timed operation builds a fresh native tree.

## Current evidence

The 2026-07-29 audit used Go 1.26.5 on Darwin arm64, Apple M4 Max, with
`GOMAXPROCS=2`, five independently calibrated 50 ms samples, and one
single-iteration process-RSS sample. The machine was shared and the latency
samples showed severe contention; they are retained for reproducibility and
API smoke evidence, not a performance claim or release threshold. The whole
benchmark process peaked at 120,733,696 resident bytes; this is not attributable
to an individual implementation.

See [raw benchmark evidence](benchmarks/raw/2026-07-29-darwin-arm64.tsv).
