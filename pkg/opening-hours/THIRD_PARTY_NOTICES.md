# Third-party notices

Runtime and test integration uses `github.com/jackc/pgx/v5` under the MIT
license. Its transitive modules and exact checksums are pinned by `go.mod` and
`go.sum`.

The package integrates the MIT-licensed owned modules `calendar`,
`temporal`, `clock`, `wire`, `validation`, and `config` at pinned
versions. These modules retain their own copyright notices.

Quality tools are invoked at pinned versions from the Makefile and scripts but
are not linked into the library. Their respective upstream licenses apply:
golangci-lint, Staticcheck, NilAway, govulncheck, actionlint, and go-mutesting.

Run `go list -m all` for the complete resolved module list before redistribution.
