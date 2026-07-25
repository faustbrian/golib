# Dependency audit

Audited with `go list -m all`, `go mod why -m`, `go list -m -u all`, and
`govulncheck` on 2026-07-18. Direct modules had no available update. Older
versions shown for graph-only modules are not compiled into this module; their
selection belongs to an upstream module graph.

| Module group | Scope and necessity | License | Cost and maintenance decision |
|---|---|---|---|
| `github.com/felixge/httpsnoop` v1.1.0 | runtime; exact optional writer interfaces | MIT | retained; replacing it risks interface lies |
| `golang.org/x/net` v0.57.0 | runtime; IDNA Lookup profile | BSD-3-Clause | retained for Fetch origin serialization |
| `golang.org/x/text` v0.40.0 | runtime transitive; IDNA Unicode tables | BSD-3-Clause | unavoidable through `x/net/idna` |
| `go.uber.org/goleak` v1.3.0 | test; goroutine leak gate | Apache-2.0 | retained outside production builds |
| `testify` v1.8.0, `go-spew` v1.1.1, `go-difflib` v1.0.0, `yaml.v3` v3.0.1 | transitive tests of `goleak` | MIT, ISC, BSD-3-Clause, MIT | no production packages or API surface |
| `x/crypto`, `x/mod`, `x/sync`, `x/sys`, `x/term`, `x/tools` | graph-only `x/net` modules | BSD-3-Clause | `go mod why` reports no needed package |
| `kr/pretty`, `check.v1` | graph-only test modules | MIT, BSD-2-Clause | `go mod why` reports no needed package |

The production build has three external module dependencies and uses no cgo,
unsafe package, linkname, runtime patch, plugin loader, or hidden exporter.
Command-line quality tools are version-pinned in the Makefile and are invoked
with `go run`; they are not library dependencies.

`integration/siblings` is a separate test-only module. It pins `router` and
`service` revisions for real compatibility checks while keeping both out of
the production module graph.
