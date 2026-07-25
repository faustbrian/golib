# Versioning and release guide

Releases are tag-driven and reproducible from a clean `main` commit.

## Prepare a release

1. choose the SemVer version and confirm compatibility impact;
2. move relevant `CHANGELOG.md` entries from `Unreleased` to a dated version;
3. update migration notes for every breaking change;
4. confirm documentation and executable examples reflect the release;
5. run the complete local verification suite;
6. merge the release preparation through normal review;
7. create an annotated `vMAJOR.MINOR.PATCH` tag on the verified commit;
8. push the tag; the release workflow verifies and publishes artifacts.

## Required verification

```sh
test -z "$(gofmt -l .)"
go mod tidy -diff
go vet ./...
go test ./...
go test ./... -race
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go test ./... -run '^Example'
go test ./... -run '^$' -bench . -benchtime=100ms
govulncheck ./...
```

Run all fuzz targets for at least one minute each before a stable release.

## Tag rules

- tags must use canonical `vMAJOR.MINOR.PATCH` numbers, without leading zeros,
  and may include a SemVer prerelease;
- build metadata is not used in release tags;
- the module version and exactly one valid, dated changelog heading must agree;
- the changelog date records release preparation and need not equal the later
  tag-push date;
- tags are never force-updated or reused;
- the workflow builds from the tag commit, not a mutable branch;
- release notes are generated from the matching changelog section and commit
  history, then reviewed before publication when practical.

## Release artifacts

Go modules are consumed from the Git tag and source archive. The GitHub release
workflow creates checksummed source archives and a release entry; it does not
publish binaries because this repository is a library.

## Rollback

Published module versions are immutable. Fix a bad release with a new patch
version. Retracting a module version requires a documented reason in `go.mod`
and release notes; do not delete or rewrite the original tag.
