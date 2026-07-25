# knapsack

[![CI](https://github.com/faustbrian/golib/actions/workflows/knapsack-ci.yml/badge.svg)](https://github.com/faustbrian/golib/actions/workflows/knapsack-ci.yml)
[![Release](https://github.com/faustbrian/golib/actions/workflows/knapsack-release.yml/badge.svg)](https://github.com/faustbrian/golib/actions/workflows/knapsack-release.yml)

`knapsack` is an exact-unit, deterministic library for offline orthogonal
packing of rectangular items into rectangular containers. The sometimes-used
term “4D bin packing” means three geometric dimensions plus scalar weight
capacity; weight is not a fourth geometric axis. This differs from classical
knapsack selection, which maximizes value while intentionally omitting items.

The heuristic returns feasible or best-known plans and never claims
optimality. The bounded exact solver is for small instances and reports
`optimal` or `infeasible` only after exhaustive search. Every solver result is
checked by the structurally separate `verify` package.

## Quick start

Construct immutable `knapsack.Item` and `knapsack.ContainerType` values with
exact `measurement` quantities, then select an explicit length and mass
lattice. Normalization rejects mixed dimensions, inexact conversion, duplicate
IDs, overflow, and inputs outside the declared resource limits.

```go
request, err := knapsack.NewRequest(items, boxTypes, knapsack.Resolution{
    Length: oneCentimetre,
    Mass:   oneGram,
}, knapsack.DefaultLimits())
if err != nil { /* reject the request */ }

plan, err := (solver.Heuristic{}).PackAll(
    ctx, request.Normalized(), solver.Options{},
)
```

Fixed-container packing never invents another container:

```go
plan, err := (solver.Heuristic{}).PackFixed(ctx, request.Normalized(),
    []knapsack.ContainerInstance{{ID: "carton-on-bench", TypeID: "mailer"}},
    solver.Options{AllowUnpacked: true},
)
```

Verify a supplied or decoded plan without rerunning optimization:

```go
result := verify.Plan(request.Normalized(), plan, verify.RequireAll())
if !result.Valid() { /* inspect result.Violations() */ }
```

Custom feasibility rules implement `constraint.Placement` and are passed in
`solver.Options.Constraints`. Callbacks receive defensive immutable views, run
synchronously with the solver context, and are supported only as trusted
application code that remains pure, bounded, and cancellation-aware. The four
complete, compiled examples are in
[`example_test.go`](example_test.go).

## Guarantees and limits

- All authoritative geometry, mass, volume, and accounting use checked integer
  lattice arithmetic. Binary floating point is never a feasibility model.
- Occupied intervals are half-open. Face and edge contact is allowed;
  positive-volume overlap is forbidden.
- Optional content center-of-gravity bounds use exact mass moments and
  inclusive X/Y/Z parts-per-million coordinates.
- Input order and map iteration do not define output ordering.
- Search work, candidate placements, input counts, memory policy, and
  diagnostics are explicit in `knapsack.Limits`.
- Plans distinguish feasibility, exhaustive optimality, proven
  infeasibility, best-known heuristic output, and budget exhaustion.
- The physical model is discrete. It is not a general physics, crush,
  hazardous-goods, robotic-motion, pallet, or vehicle-axle simulation.

See [usage](docs/usage.md), [API](docs/api.md), [model](docs/model.md),
[algorithms](docs/algorithms.md), [recipes](docs/recipes.md),
[architecture](docs/architecture.md), [capabilities](docs/capabilities.md),
[migration](docs/migration.md), [adoption](docs/adoption.md),
[benchmarks](docs/benchmarks.md), [security](docs/security.md),
[FAQ and troubleshooting](docs/faq.md), and [release policy](docs/release.md).

## Development

Go 1.26.5 is the initial minimum toolchain. `make check` is the ordinary local
and pull-request gate. `make release-check` adds complete mutation and
benchmark comparison for the current monorepo workspace. `make publish-check`
additionally requires versioned dependencies without local replacements. No
benchmark or optimality claim is valid without the checked-in evidence
identified by those gates.

Licensed under Apache-2.0.
