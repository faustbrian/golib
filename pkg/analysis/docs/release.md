# Release process

## Local candidate verification

Install the exact stable patch release recorded in `.go-version`. `make check`
fails before analysis when the active `go env GOVERSION` differs, and workflow
policy requires every CI and release setup step to consume the same file.

Choose a semantic version without a leading `v`, update the changelog and any
rule `introduced_version` metadata, then run:

```sh
make check
make race
make benchmark
make nilaway
make release-verify VERSION=0.1.0
```

`release-verify` packages the candidate twice and compares every byte. It
checks six CGO-disabled targets: Linux, macOS, and Windows on amd64 and arm64.
Every ZIP contains only the versioned directory, executable, README, changelog,
and security policy. It validates sorted SHA-256 checksums, exact archive
contents, and the host executable's reported version.

To inspect a candidate without publishing it, use an empty output directory:

```sh
make release VERSION=0.1.0 DIST_DIR=dist
shasum -a 256 -c dist/checksums.txt
```

The release command refuses invalid semantic versions and non-empty output
directories. Builds use `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and a
linker-injected version. Source file timestamps and ZIP metadata are normalized
before checksums are created.

## Tag publication

After local verification and review, a maintainer may create a signed
`vX.Y.Z` tag pointing at the exact candidate commit. The tag-triggered release
workflow repeats `make check`, race tests, and release verification before its
publish job receives `contents: write`. All earlier steps retain read-only
contents permission. The publish job builds the same archives and creates the
GitHub release with `checksums.txt`.

The workflow never force-updates tags and does not publish from branches or
arbitrary workflow input. Signing is owned by the maintainer and publishing
environment; the repository does not manufacture signing identity. Consumers
verify both the trusted tag or signature and the archive checksum.

Branch CI runs the complete candidate gate on Linux and macOS. A blocking
Windows leg independently runs vet, all analyzer tests, the race detector, and
a trimpath command build so path and platform behavior is exercised before a
candidate can be tagged.

## Rollback and replacement

Published artifacts are immutable. If a candidate is wrong, publish a new
patch version with a changelog entry. Do not replace archives or checksums under
an existing version. A withdrawn release may be marked clearly, but its tag and
artifacts remain available for audit unless security response requires a
documented exception.
