# Performance

Validation is synchronous and allocation-conscious. Reports are immutable,
which intentionally allocates defensive copies when findings are added. Pass
paths are cheaper than failure aggregation.

Apple M4 Max, Go 1.26.5 baseline. Times are medians from five 500 ms
samples per workload:

| Workload | Time | Bytes | Allocations |
| --- | ---: | ---: | ---: |
| Scalar range pass | 73.5 ns | 48 B | 1 |
| Five-item two-rule collection | 1.55 us | 888 B | 21 |
| Two-rule collect-all failure | 742 ns | 928 B | 14 |
| Oversized hostile collection rejection | 338 ns | 464 B | 7 |
| Oversized hostile string rejection | 350 ns | 464 B | 7 |
| One MiB hostile path rejection | 275 ns | 440 B | 7 |
| Reflection-free typed plan | 223 ns | 120 B | 3 |
| Startup-compiled reflective plan | 130 ns | 80 B | 3 |

Numbers are evidence, not cross-machine pass thresholds. Reproduce the table
with:

```sh
go test . -run '^$' -bench BenchmarkValidation -benchmem \
  -benchtime=500ms -count=5
```

`make benchmark` provides the shorter release smoke. The scalar allocation
test enforces at most two allocations. Collection and string limits are
checked before child validation or parsing. Map keys are bounded before they
are copied and sorted for deterministic reports.

Before bounded path-length scanning, the one MiB hostile path case allocated
1,049,028 B per operation and had a 156 us three-sample median. The current
five-sample median allocates 440 B and takes 275 ns because rejection no longer
renders attacker-controlled path text.

Use typed plans on hot paths. Tag plans compile reflection once and are safe for
concurrent validation. Use a bounded `structplan.Cache` only when many types are
compiled dynamically. Code generation is not shipped because current evidence
does not justify a second conformance path.
