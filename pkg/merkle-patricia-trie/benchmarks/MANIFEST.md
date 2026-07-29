# Benchmark evidence manifest

The `raw` directory contains unedited command output and derived `benchstat`
summaries for the 2026-07-29 baseline documented in
[`docs/benchmarks.md`](../docs/benchmarks.md).

## Files

| File | Contents |
| --- | --- |
| `raw/2026-07-29-local.txt` | Ten complete local samples collected in one invocation |
| `raw/2026-07-29-local-benchstat.txt` | Pinned benchstat summary of the local samples |
| `raw/2026-07-29-geth-comparison.txt` | Ten local and ten pinned-Geth owned-lookup samples |
| `raw/2026-07-29-geth-comparison-benchstat.txt` | Normalized-package comparison summary |
| `raw/2026-07-29-parallel.txt` | Ten 16-worker populated-read samples |
| `raw/2026-07-29-parallel-benchstat.txt` | Pinned benchstat parallel summary |
| `raw/2026-07-29-memory.txt` | One ordinary-construction run under macOS `/usr/bin/time -l` |

The summaries are derived; each raw file records the exact command followed by
its unedited output. No outlier was removed.

`inputs.sha256` covers every Go source/test file and module/benchmark control
file that can affect these commands. Recompute it after any such change and
rerun every affected result. Raw-result and documentation files are excluded
to avoid circular evidence identity.

## Update procedure

1. Confirm the source pins and execution-time client inventory.
2. Run functional and interoperability checks before timing.
3. Capture environment and tool versions.
4. Run the exact distribution, comparison, parallel, and memory commands.
5. Preserve complete raw output without filtering or outlier removal.
6. Derive summaries with the pinned benchstat revision.
7. Recompute `inputs.sha256`.
8. Update the methodology, results, changelog, and this manifest together.
