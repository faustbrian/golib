# Releasing

1. Finish implementation and documentation locally with a clean worktree.
2. Run `go mod tidy -diff`, `make check`, `make mutation`, and
   `make postgres-matrix`.
3. Review mutation survivors, fuzz crashes, vulnerability findings, benchmark
   changes, dependency licenses, and locale-data changes.
4. Update `CHANGELOG.md`, compatibility provenance, API baseline, and evidence.
5. Create a signed semantic-version tag only after the user verifies final
   hosted CI. Git state is never an implementation blocker.
6. The release workflow verifies the tag and builds a deterministic source
   archive with checksums before publishing notes from the changelog.

No release may claim successful hosted checks from local workflow syntax alone.
