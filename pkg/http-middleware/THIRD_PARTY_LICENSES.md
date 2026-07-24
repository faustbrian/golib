# Third-party attribution

| Module | Build scope | Use | License |
|---|---|---|---|
| `github.com/felixge/httpsnoop` | runtime | exact writer interface wrapping | MIT |
| `golang.org/x/net` | runtime | IDNA lookup profile | BSD-3-Clause |
| `golang.org/x/text` | runtime transitive | Unicode tables | BSD-3-Clause |
| `go.uber.org/goleak` | tests | goroutine leak verification | Apache-2.0 |

Test and graph-only transitive modules are inventoried in
[`docs/dependencies.md`](docs/dependencies.md). They are not linked into the
production package.

Release preparation runs `go list -m all` and vulnerability scanning. A release
maintainer must update this table when the module graph changes.
