# Verification evidence

`specification/conformance/normative.tsv` inventories all 49 normative
statements from the pinned prose and schema descriptions. The reviewed
`evidence.tsv` maps each statement to implementation and executable tests.

`object-fields.tsv` inventories all 75 declared object fields, including shape,
requiredness, nullability, defaults, extension behavior, and unknown-field
policy. Its reviewed overlay maps each object to model, meta-schema, complete
round-trip, removal, and explicit-null tests.

The [resolver threat model](resolver-threat-model.md) maps adversarial cases to
controls and executable evidence. The [resource budget table](resource-budgets.md)
records every default limit and the operation that enforces it.
The [specification report](specification-report.md) records the supported
version boundary, pinned meta-schema alignment, corpus verdicts, and
independent validation layers.

Run:

```sh
make conformance
make coverage-report
make race
make leak
make fuzz FUZZ_TIME=2s
make benchmark BENCH_TIME=100ms
```

`ROADMAP.md` lists optional post-foundation expansion, not unresolved release
blockers. Passing line coverage alone is not conformance evidence; assertions
must detect the normative defect they claim to cover.

The fuzz gate exercises strict JSON, complete OpenRPC parsing, preserving and
canonical round trips, meta-schema and semantic validation, Draft 7
compilation, expressions, references and pointers, composition, semantic diff,
and discovery snapshots. Every target uses finite input policies and asserts
panic freedom or deterministic output.
