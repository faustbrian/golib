# Versioning and release guide

Releases are immutable semantic-version tags created from a clean, reviewed
`main` commit. The GitHub tag workflow reruns tests, race detection, coverage,
and vet before creating release notes.

## Choose a semantic version

- Patch: backward-compatible fixes and documentation corrections.
- Minor: backward-compatible features and new optional APIs.
- Major: post-v1 breaking exported API or documented behavior changes.

Use a prerelease such as `v1.0.0-rc.1` when external adopters need to validate a
candidate. Never move or replace a published tag.

## Release commands

After adding a dated changelog section and release link, run the target for the
intended compatibility level:

```sh
make release-patch
make release-minor
make release-major
```

Each target calculates the next stable version from the latest stable tag,
requires a clean `main` branch synchronized with `origin/main`, runs the
complete release checks, and creates a local annotated tag. It never pushes the
tag. Review the tag before running the printed `git push origin vX.Y.Z`
command.

## Release checklist

1. Confirm `CHANGELOG.md` moves relevant Unreleased entries under the version
   and date.
2. Confirm `go.mod`, examples, and documentation use the canonical public
   module path.
3. Confirm the compatibility impact and migration notes for every observable
   protocol or API change.
4. Run:

   ```sh
   test -z "$(gofmt -l .)"
   go vet ./...
   staticcheck ./...
   go test -race ./...
   scripts/check-coverage.sh
   go test -run='^$' -bench=. -benchmem ./...
   go test -fuzz=FuzzDispatcher -fuzztime=30s .
   go test -fuzz=FuzzRequestUnmarshal -fuzztime=30s .
   govulncheck ./...
   ```

5. Verify all required GitHub Actions checks are green on the release commit.
6. Create an annotated tag: `git tag -a vX.Y.Z -m "vX.Y.Z"`.
7. Push only that tag: `git push origin vX.Y.Z`.
8. Confirm the release workflow created the GitHub release and generated notes.
9. In a clean temporary module, run the following command and compile a minimal
   client:

   ```sh
   go get github.com/faustbrian/golib/pkg/jsonrpc@vX.Y.Z
   ```

## Failure handling

If verification fails, fix forward with a normal commit and restart the
checklist. Do not bypass hooks, force-update the tag, or edit a published tag.
If a broken version was published, document it and release the next patch.

## Reproducibility

This repository publishes a Go library, so no binary artifact is required. The
tag identifies the source consumed by the Go module proxy. Workflows pin tool
versions where they install tools and use the module's declared Go version.
