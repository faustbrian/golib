# Performance, FAQ, and troubleshooting

Lookups use immutable generated maps and require no network. Benchmark with
`make benchmark`; protect regressions by comparing `ns/op`, allocations, and
bulk conversion throughput on the same Go version and hardware. Phone parsing
is intentionally heavier than fixed-code lookup.

## Local verification

`make release-check` reproduces every blocking release gate: formatting, vet,
Staticcheck, strict golangci-lint, tests, exact coverage, race detection,
generated-data drift, provenance and license checks, reviewed mutation checks,
documentation, API compatibility, vulnerability scanning, workflow linting,
fuzz smoke, and benchmarks. `make nilaway` runs separately because NilAway is
an explicitly advisory signal. Every component is also available as its own
documented Make target; run `make format` to apply formatting changes.
All Make targets set `GOWORK=off`, so ignored developer workspaces cannot
replace the dependency versions pinned by `go.mod` or make local evidence
disagree with CI.

The generator target acquires checksum-pinned authoritative inputs. Core
identifier parsing, validation, lookup, and formatting remain offline and do
not invoke the generator or perform network requests.

**Why was a lowercase code rejected?** Strict `Parse` preserves the boundary.
Use an explicit canonicalization API only when your contract permits it.

**Why is a historic code rejected?** Enable the corresponding parse option and
retain its status. Do not silently alias it.

**Why does national phone parsing fail?** Supply `RegionHint`; the package does
not infer locale or country.

**Why is a possible number invalid?** Its shape is plausible but current
metadata does not recognize it as valid. Neither result proves ownership.

**Why can’t postal parsing confirm a city?** Postal values are bounded opaque
values. Address validation, search, and provider rules belong in Postal.

For generated drift, verify network access, upstream checksum changes, and
license terms before updating a pin. For config or SQL failures, inspect the
typed safe error and confirm the input is a string/byte value, not a number.
