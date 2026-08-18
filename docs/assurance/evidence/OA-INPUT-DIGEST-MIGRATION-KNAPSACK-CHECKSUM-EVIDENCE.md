# Knapsack Checksum And Evidence Migration

Observed at `2026-08-18T17:05:00Z` on `darwin/arm64` with Go 1.26.6.

## Change Boundary

The Knapsack module requires the owned Math and Measurement modules at the
local source-proxy version `v0.0.0`. Their release archive contents changed
when repository normalization updated release notes and module metadata, but
the parent Knapsack `go.sum` still contained the preceding archive checksums.

The parent checksums were refreshed from the current task-owned local proxy,
and `pkg/knapsack/specification/evidence.json` was regenerated after recording
the change in the Knapsack changelog. No production source, tests, public API,
runtime configuration, service image, external dependency, or reference
scenario behavior changed.

Knapsack benchmark fingerprints intentionally include `go.sum`, so the same
archive-checksum refresh changed the identity of the native, peak-RSS, and
BoxPacker comparison inputs without changing executable code, benchmark
fixtures, generators, thresholds, runtime versions, or recorded samples. The
stored input identities were migrated exactly as follows; the measured data
was not regenerated or altered:

| Profile | From SHA-256 | To SHA-256 |
|---|---|---|
| Native | `bae55cf36f3847ab0ba0f699c382650cf5ae8d012ba140bfc78aed2f3db0a865` | `2d058a7ee82221f789a4a28d82b1e7e4d516125c97e76a01de0d55837a8bc5f1` |
| Peak RSS | `2746ec4423ed606fd186777a31479685c3eb5e69efa888783a2d5082367a66ae` | `bcdb87fe8575bf1cc0db6b7d85c0f166f86f6cef2f30e7823f4f7951a2de8ae9` |
| BoxPacker | `11b61e2ccc2ac7413642796f095f606f39368ad25bc057f6fc8d7478a9c71d0f` | `9c12570fee9b9bb79ee5adaef532530b2e86559628a66fcd817ee123119f3da9` |

The nested Go Money objective includes parent Knapsack evidence and checksum
metadata in its operational-assurance input closure. Its exact one-way input
identity migration is:

| Module | From SHA-256 | To SHA-256 |
|---|---|---|
| `pkg/knapsack/objective/gomoney` | `59991910a75373bd251394ccbcf355ef51d1ee6f85f583a373d38b928849435a` | `821617ca0f01f200c49fa9f0d7589e310f0a506fbadc54265b5679f37193823a` |

## Verification

The Knapsack evidence generator completed against the current task-owned
local module proxy, and the focused evidence-currentness test passed against
the regenerated manifest. The strict package check then identified only the
three benchmark fingerprint changes caused by the checksum refresh; after the
exact identity migration above, the unchanged benchmark evidence must pass
its currentness checks. Repository operational-assurance validation identified
only the exact Go Money objective transition recorded above.

## Claim Boundary

This evidence authorizes only the exact one-way operational-assurance input
digest migration caused by refreshing the parent Knapsack dependency
checksums, changelog, and aggregate evidence. It does not replace package,
release, mutation, or operational-scenario gates and does not authorize any
future migration across source, tests, runtime configuration, dependencies,
tools, services, or orchestration changes.
