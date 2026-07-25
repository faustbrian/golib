# Benchmarks

Core benchmarks use fixed semantic frames, event streams, terminal dimensions,
capabilities, validation boundaries, and option counts. They report allocations
and include setup consistently within or outside the timed loop as documented
by each benchmark. Run them independently of the sibling workspace:

```sh
GOWORK=off go test ./... -run '^$' -bench Benchmark -benchmem -benchtime=100ms
```

The 2026-07-22 Apple M4 Max development snapshot at Go 1.26.5 and a 100 ms
sample window was:

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| FirstSemanticRender | 6,632 | 8,246 | 45 |
| InteractiveTextEditing | 52,329 | 347,669 | 138 |
| InteractiveTextEditingMaximumBound | 15,178,547 | 20,629,166 | 125 |
| SearchLargeOptionSet | 7,140,494 | 9,099,128 | 49,944 |
| InteractiveSearchNavigationPagination | 1,274,977 | 1,714,097 | 9,466 |
| FormValidationAndTransitions | 33,748 | 338,827 | 127 |
| SemanticRenderProfiles/plain | 7,231 | 8,064 | 39 |
| SemanticRenderProfiles/no-color | 6,483 | 8,064 | 39 |
| SemanticRenderProfiles/ansi-256 | 7,329 | 8,246 | 45 |
| SemanticRenderProfiles/true-color | 8,068 | 8,246 | 45 |
| SemanticRenderProfiles/redirected-ascii | 8,584 | 10,312 | 57 |
| InteractiveCancellationCleanup | 26,542 | 330,172 | 32 |
| ProgressUpdateAndRender | 4,996 | 5,089 | 37 |
| ProgressUpdateCoalescing | 8,329 | 0 | 0 |

These are local observations, not cross-machine release budgets or comparative
speed claims. Search includes normalization and ranking of 10,000 options.
Interactive editing includes terminal construction, acquisition, event
processing, semantic rendering after each event, submission, and release.
Progress includes an update and explicit linear render. First render measures
only semantic ANSI rendering after frame construction.

The core suite also measures a 64 KiB maximum-bound text submission,
interactive search filtering plus page navigation and resize, form validation
and field transitions, plain/no-color/ANSI-256/true-color/redirected-ASCII
rendering, cancellation with terminal cleanup, and 1,000 coalesced progress
updates. Portable allocation gates cover representative text editing,
10,000-option static search, interactive 1,000-option search pagination, form
transitions, progress rendering, cancellation cleanup, and first render. Timing
results remain observational; allocation ceilings fail `make check`.

## Comparative pseudo-terminal benchmark

The dependency-isolated module in `benchmarks/comparison` drives a single-line
text prompt through an 80 by 24 pseudo-terminal. Every engine receives the
same `Ada` plus Enter byte stream, renders to the same terminal, returns the
same answer, and includes prompt construction, terminal setup, rendering,
editing, submission, cleanup, and harness setup in the timed region. Survey's
cursor-position requests are answered by a VT10x terminal model. The direct
Bubble Tea/Bubbles case includes a complete program and renderer lifecycle.

Run it on Unix with:

```sh
cd benchmarks/comparison
BENCHTIME=1s COUNT=3 ./run-benchmarks.sh
```

Each engine runs in a fresh process. This prevents package-global terminal or
renderer state in one candidate from contaminating another candidate's run.

The median 2026-07-22 Apple M4 Max observation with Go 1.26.5 was:

| Engine | Version | median ns/op (range) | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| prompts | local | 1,172,152 (822,200-1,349,903) | 14,264 | 134 |
| Huh | 2.0.3 | 1,313,779 (800,721-1,393,199) | 468,850 | 565 |
| Survey | 2.3.7 | 1,537,728 (1,443,420-1,648,731) | 154,128 | 891 |
| PromptUI | 0.9.0 | 721,049 (700,994-873,682) | 56,770 | 458 |
| Bubble Tea/Bubbles | 2.0.8 / 2.1.1 | 19,374,509 (18,866,274-19,506,337) | 727,740 | 1,966 |

The raw three-sample results are retained in
`specification/benchmark-comparison-2026-07-22.tsv`. These are observations,
not portable speedup claims. The engines render
different visual detail and the Bubble Tea case pays for full-screen renderer
startup and shutdown. The benchmark exists to expose those boundaries rather
than erase them.

## Binary-size observation

`benchmarks/comparison/measure-binaries.sh` builds stripped, trim-path,
CGO-disabled binaries that construct one text-input definition. On the same
machine the sizes were 2,132,674 bytes for prompts, 3,664,818 for Huh,
2,384,946 for Survey, 1,949,282 for PromptUI, and 2,527,154 for Bubbles. This is
a minimum-import measurement, not the size of a complete application. Raw
values are retained in
`specification/binary-size-comparison-2026-07-22.tsv`.

Latency remains observational because shared CI runners are not stable timing
references. Allocation budgets and a 2.5 MB minimum-import binary ceiling are
the portable regression gates; release snapshots record timing distributions
without treating cross-machine variation as a package regression.
