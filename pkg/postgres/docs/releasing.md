# Release process

1. Confirm the intended SemVer and update `CHANGELOG.md` with a dated version.
2. Review public API, pgx release notes, supported Go/PostgreSQL matrix,
   security findings, and migration guidance.
3. Run `make check`, `go mod tidy -diff`, `git diff --check`, and actionlint.
4. Push the release commit and verify every CI, integration, Security, fuzz,
   benchmark, and compatibility job on that exact SHA with no required skip.
5. Create a signed or protected `vMAJOR.MINOR.PATCH` tag on a commit reachable
   from `main`.
6. Let the release workflow re-run gates, build a deterministic source archive,
   publish checksums, and create release notes from the changelog.

Never move or force-update a published tag. A pgx or PostgreSQL support change
requires explicit compatibility evidence before release.
