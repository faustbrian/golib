# Operations, compatibility, performance, troubleshooting, and FAQ

## Startup and lifecycle

Construct sources and the plan once at the composition root, then load with a
startup deadline. Treat any constructor, source, merge, decode, or validation
error as a failed candidate. Required sources fail closed. Keep the immutable
snapshot or its typed value; `Value` returns a defensive copy.

V1 does not watch or hot-reload. An application implementing reconciliation
must reopen all sources, rebuild the complete plan, validate, and atomically
publish a new snapshot. Failed refreshes must leave the prior snapshot clearly
identified as stale; the library does not terminate processes or manage
readiness.

## Compatibility

The minimum toolchain is Go 1.25. CI tests Go 1.25 and the current stable Go on
Linux, macOS, and Windows. `CaseNative` intentionally differs on Windows;
choose an explicit case mode for portable environment contracts. Symlink and
POSIX permission behavior is tested where the operating system supports it.
Windows discovery rejects reparse points by default, including junctions and
mount points. Exact root containment deliberately fails closed for
case-distinct paths; preserve operating-system path casing for compatibility.

JSON follows the standard library. YAML uses `go.yaml.in/yaml/v4`; TOML uses
BurntSushi TOML. Dependency updates pass cross-format fixtures, exact coverage,
race tests, fuzz smoke, and API comparison.

## Performance budgets

The committed benchmark classes cover plan load, decode, merge, validation, and
a 10,000-key JSON document. They are smoke-gated on pull requests and archived
weekly. On an Apple M4 Max reference run, the 10,000-key parser processed about
36 MB/s; machine-specific values are evidence, not a guarantee.

Operational budgets should be set per service. Keep startup configuration small
(normally below 1 MiB), set explicit tighter limits, and investigate material
regressions in allocations or latency before release. This package optimizes
determinism and safety ahead of accepting unbounded inputs.

## Troubleshooting

- `configuration source not found`: only suppress it by marking that exact
  source optional; check the explicit path/root.
- `configuration source changed during read`: retry the complete load from a
  stable generation; never publish the failed candidate.
- `unsupported extension`: set a supported filename extension or an explicit
  `filesystem.Format` for readers/data files.
- merge type conflict: make layers use the same structural kind or delete/null
  explicitly; implicit conversion does not happen during merge.
- unknown or required field error: align `config` tags and document keys before
  changing decoder strictness.
- environment collision: choose unique explicit `env` tags after prefix,
  separator, and case normalization.
- interpolation absent/cycle/limit: supply the explicit variable view, break the
  cycle, or adjust a justified bound.
- symlink/outside-root/permission rejection: fix deployment layout or select a
  deliberate documented policy; do not broadly disable checks.

## FAQ

**Does it read `.env` automatically?** No. Construct a dotenv source explicitly.

**Does it search parents or home automatically?** No. Enable each search root
and upward/user-config behavior explicitly.

**Can slices append across layers?** No. The upper slice replaces the lower.

**Can I inspect secrets through provenance?** No. Provenance contains metadata,
not values. `Secret.Reveal` is the explicit plaintext boundary.

**Is Infisical supported?** Platform-delivered environment/files are supported
normally. A native SDK adapter is intentionally absent from core.

**Can I mutate a snapshot?** Returned values are copies; mutation does not alter
the stored snapshot. Build a new plan/load for a new generation.

**Can cancellation preempt arbitrary extension code?** Only cooperative code
can be canceled safely in Go. Use context-aware decode and filesystem
interfaces for untrusted or remote extensions. Legacy hooks and ordinary
`fs.FS` implementations are trusted to return; the library does not spawn
abandonable goroutines that could leak.
