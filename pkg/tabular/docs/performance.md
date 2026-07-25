# Performance and memory verification

Benchmarks generate representative inputs before timing so the repository does
not carry large derived fixtures. `go test ./... -run '^$' -bench . -benchmem`
exercises:

- 20,000 CSV rows with three fields;
- 20,000 fixed-width rows with three byte-positioned fields;
- ZIP extraction of a 20,000-row CSV member;
- an 8 MiB bounded XLS source; and
- a 10,000-row XLSX workbook written with a streaming OOXML writer.

Every benchmark reports allocations and bytes processed. Results are runtime,
architecture, Go-version, and dependency-version specific; the project does
not publish universal throughput or heap guarantees from a single machine.

Large-file verification is kept behind the `largebench` build tag because the
fixtures are generated on disk and are intentionally expensive. Run each case
once with process-level peak-memory reporting on macOS:

```console
/usr/bin/time -l go test -tags=largebench -run '^$' \
  -bench '^BenchmarkDelimitedReader50MiB$' -benchmem -benchtime=1x
/usr/bin/time -l go test -tags=largebench -run '^$' \
  -bench '^BenchmarkXLSXReader50MiB$' -benchmem -benchtime=1x
```

Both cases require at least 50 MiB of compressed/on-disk input and exactly
100,000 data rows. They fail during setup if either constraint is missed. The
CSV fixture is approximately 62 MiB; the XLSX fixture is approximately 84 MiB
after ZIP compression because its cell data is intentionally difficult to
compress. CI runs both cases weekly and on manual dispatch, recording
`/usr/bin/time` output in the workflow summary.

On an Apple M4 Max with Go 1.26.5, the CSV reader initially measured 34.9 ms,
89.6 MB allocated, and 162 MB maximum RSS. Avoiding redundant normalization
copies and buffering source reads at 64 KiB reduced a one-iteration run of the
identical case to 24.6 ms, 80.1 MB allocated, and 162 MB maximum RSS. The XLSX
reader initially measured 14.59 s, 6.07 GB allocated, and 2.10 GB maximum RSS.
Skipping cell-type metadata lookups for ordinary values reduced the identical
XLSX case to 5.99 s, 3.46 GB allocated, and 630 MB maximum RSS. These figures
are a regression reference, not a cross-platform performance guarantee.

Delimited and fixed-width constructors do not read input, and regression tests
feed one-byte chunks to prove the first row can be returned without consuming
the complete source. ZIP entry extraction is streamed through `io.Copy`.

XLS necessarily materializes its source and rejects files above
`MaxWorkbookBytes`. XLSX returns rows incrementally, but validation and
Excelize may allocate substantially more heap than the compressed workbook
size. Profiling the optimized large case attributes most remaining allocation
to worksheet XML validation and Excelize row decoding. ZIP limits bound
declared expanded payload sizes; callers must also set process/job memory
limits appropriate to their environment.
