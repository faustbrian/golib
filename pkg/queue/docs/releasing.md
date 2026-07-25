# Versioning and release guide

Releases follow semantic versioning and are created from annotated `vX.Y.Z`
tags on a clean `main` commit synchronized with `origin/main`.

## Release commands

After moving the relevant changelog entries to a dated release section and
adding its release link, choose the intended compatibility level:

```sh
make release-patch
make release-minor
make release-major
```

Each target calculates the next stable version, runs the complete local
release checks, and creates a local annotated tag. It never pushes the tag.
Review the result before running the printed `git push origin vX.Y.Z` command.

## Release checklist

1. Ensure CI, independent Redis Streams and Valkey 9 integration, coverage,
   mutation, lint, security, docs, and example gates pass.
2. Add user-visible changes and semantic notes to `CHANGELOG.md`.
3. Verify `go mod tidy` produces no diff.
4. Run the appropriate `make release-*` target.
5. The release workflow verifies the tag and publishes release notes/checksums.
6. Verify the public module through the Go proxy before announcing adoption.

Any ack, retry, redelivery, ordering, or shutdown behavior change must be called
out explicitly and treated as breaking when existing correctness can change.
The release workflow blocks publication if either native Streams integration
job fails.

If verification fails, fix forward with a normal commit. Never bypass a check,
move a published tag, or force-push release history.
