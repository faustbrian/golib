# Hardening evidence

The release-equivalent local stack is `make ci`. Its individual gates are
reproducible and pinned through `go.mod`, exact GitHub Action commits, and a
digest-pinned PostgreSQL image.

## Semantic and security evidence

- Schema/request/plan tables exhaust unknown, duplicate, conflicting,
  deprecated, unauthorized, revision, type, operator, null, empty, bound, cost,
  and pagination behavior.
- Cursor tests cover encryption, tampering, wrong keys/versions/schema/sorts,
  expiry, replay, rotation, nullable positions, direction, first/last pages, and
  raw-token removal during compilation.
- A deterministic 100-seed pagination corpus covers forward/backward traversal,
  ties, nulls, inserts, deletes, and empty boundaries under the documented seek
  consistency model.
- HTTP, JSON-RPC, filter-expression, Unicode/duplicate/depth, and cursor fuzz
  targets run as smoke gates and support longer native Go fuzz sessions.
- Shared schemas, plans, codecs, clocks, and key rotation run under `-race`.
- Canonical conformance compares HTTP, JSON-RPC, and authoritative JSON:API
  bridge plans, verifies the closed OpenRPC query descriptor, and compares
  authenticated cursor state independently of randomized token bytes.
- PostgreSQL 18 integration executes allowlisted fragments and proves injected
  filter text remains data, mandatory tenants cannot be escaped, and seek pages
  remain stable across an intervening insert.

## Resource and quality evidence

Every runtime production package is held at 100.0% statement coverage.
`apiquerytest` is intentionally measured as test-support code rather than a
runtime package. Allocation tests enforce compile, canonical, large-schema, and
cursor budgets in normal and race builds. Schema costs are bounded,
overflow-safe API weights.

The final local mutation run killed 434 of 434 mutants with 0 lived, uncovered,
timed out, nonviable, or skipped: 100.00% efficacy and mutant coverage. It used
two workers and a 10x timeout coefficient and targeted all module production
packages, including allowlists, mandatory constraints, authorization, limits,
ordering, cursor verification, and page boundaries.

Static gates are gofmt, vet, Staticcheck, strict correctness/security-focused
golangci-lint, visible advisory NilAway, govulncheck, actionlint, documentation
presence, and exported API compatibility. No suppression weakens a runtime
security invariant; the deterministic property test's non-cryptographic random
generator is the sole documented gosec exception.

## Publication order

Local verification uses the sibling `validation` implementation at commit
`238f8ef9a96a498b45cb34b7bee5edbdf55f9104`. Publish that commit first, replace
the local `replace` directive with its public version, run `make ci` again, and
only then create the v1 tag. GitHub workflows check out that exact dependency
commit while the pre-v1 repositories are released in sequence.
