# Platform And CPU Audit

## Evidence Scope

This audit was refreshed on 2026-08-12 with Go 1.26.5 on an Apple M4 Max
running darwin/arm64. Every Go command used `GOWORK=off`, `CGO_ENABLED=0`, and
a disposable task-owned `GOCACHE`. Cross-compilation proves only that every
package and test binary builds for the named target. It does not prove runtime,
kernel, scheduler, signal, race-detector, or durability behavior there.

The dependency revisions and source-file hashes are pinned in
[`../specification/sources.json`](../specification/sources.json).

## Runtime Evidence

| Runtime | Build path | Evidence | Result |
| --- | --- | --- | --- |
| darwin/arm64 | default | Complete internal conformance suite | Pass |
| darwin/arm64 | `purego,noadx` | Complete `internal/backend` suite | Pass |
| darwin/arm64 | default | Pinned Go IPA scalar-field multiplication, squaring, and inversion properties | Pass |
| darwin/arm64 | default and `purego` | Pinned gnark base-field multiplication, squaring, inversion, zero inversion, and batch inversion properties | Pass |
| darwin/amd64 under Rosetta 2 | default | Complete `internal/backend` suite | Pass |
| darwin/amd64 under Rosetta 2 | `purego,noadx` | Complete `internal/backend` suite | Pass |
| darwin/amd64 under Rosetta 2 | default | Pinned Go IPA and gnark field properties, including assembly-to-generic multiplication comparison | Pass |

Rosetta did not expose the ADX path during the verbose Go IPA arithmetic run;
the upstream test emitted no ADX-disable rerun. The amd64 results therefore
exercise the ordinary amd64 assembly path, not real-hardware ADX/BMI2 or
AVX-512 dispatch.

## Compile Matrix

Every package and test binary compiled with `go test -c -o <owned-directory>
./...` for:

| Target | Build tags | Result | Claim boundary |
| --- | --- | --- | --- |
| linux/amd64 | default | Pass | Compile only |
| linux/amd64 | `purego,noadx` | Pass | Compile only |
| linux/arm64 | default | Pass | Compile only |
| linux/386 | default | Pass | Compile only; no 32-bit runtime evidence |
| windows/amd64 | default | Pass | Compile only |
| darwin/amd64 | default | Pass | Also exercised under Rosetta as listed above |

The compile matrix uses the default Go architecture levels: `GOAMD64=v1` and
`GOARM64=v8.0`. No cgo path is part of the module or this evidence.

## Arithmetic Dispatch

The pinned `go-ipa` scalar field has amd64 assembly for addition,
subtraction, multiplication, reduction, and related operations. It selects the
ADX multiplication path only when both ADX and BMI2 are reported; the `noadx`
tag disables that selection. Non-amd64 targets use its Go arithmetic.

The resolved gnark base field uses assembly on amd64 and arm64 unless the
`purego` tag is set. Its amd64 scalar operations dispatch on ADX, and vector
operations contain separate AVX-512 and AVX-512 IFMA paths selected by CPU
capabilities and input size.

The `purego` tag does **not** make this complete dependency graph pure Go:
`go-ipa`'s scalar-field amd64 assembly does not honor that tag. Combining
`purego,noadx` selects gnark's Go base-field path and disables Go IPA's ADX
branch, but retains Go IPA's ordinary amd64 assembly.

## Unverified Platforms And Features

The following remain release evidence gaps:

- native Linux, Windows, and 32-bit execution;
- race, stress, leak, crash, and process-termination behavior outside native
  darwin/arm64;
- real amd64 ADX/BMI2 and AVX-512 or AVX-512 IFMA execution;
- big-endian architectures and architectures outside amd64, arm64, and 386;
- platform-specific performance, peak memory, and scheduler scaling; and
- side-channel behavior for every assembly, generic, and CPU-dispatched path.

Compile success and arithmetic property tests MUST NOT be presented as
production support, constant-time evidence, or cross-platform durability.
