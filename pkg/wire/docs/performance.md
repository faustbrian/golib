# Performance and allocation policy

Benchmarks are smoke evidence and comparison tools, not pass/fail latency
contracts. Run them with:

```sh
go test -run '^$' -bench . -benchmem ./...
```

Each format benchmarks representative decode and encode paths. Shared
adversarial benchmarks track rejection and allocations for deep JSON/XML/SOAP,
YAML aliases, byte limits, forged binary lengths, and cyclic encoder values.
Correctness tests separately require malformed BSON lengths, impossible and
oversized MessagePack collections, CBOR collection/depth limits, and YAML
alias/depth limits to be rejected before application-sized allocation is
trusted. Forged MessagePack, CBOR, and BSON length regressions also enforce a
broad cross-toolchain ceiling of 200 allocations per rejection.

All decoder readers first apply a 1 MiB default byte limit. This bounds bytes
retained from an untrusted stream, but decoded Go values can be larger than
their wire representation. CBOR and MessagePack therefore also cap nesting and
collection counts; YAML applies parser alias/depth protection. Applications
should choose tighter limits from measured protocol envelopes.

The shared `FuzzRoundTrip` target continuously compares writer and reader
semantics for all eight formats. It is a correctness oracle, not a throughput
benchmark, and discovered the YAML implicit block-indentation differential.

Encode benchmarks include allocation counts because every public `Encode`
returns a complete byte slice and every `EncodeWriter` serializes before
destination I/O. Every encoder retains at most `MaxBytes` of output; zero uses
the 1 MiB default. Limit failures return `wire.ErrSizeLimit` and writer APIs do
not publish the partial in-memory result. Decoded or source Go values may still
occupy more memory than their wire representation, so applications should also
bound attacker-influenced source collections.

Before recursive encoding, a path-local reflection walk rejects cycles and
more than 1,000 traversed levels. Shared acyclic subvalues are accepted because
only the current traversal path is tracked. This adds linear preflight work in
the size of an ordinary value and prevents stack exhaustion in codecs that do
not implement cycle detection.

Record Go version, OS/architecture, CPU, command, fixtures, and option profile
when comparing samples. Do not turn single-run nanosecond or allocation samples
into correctness gates; investigate statistically meaningful regressions with
multiple runs and `benchstat` outside the release gate.
