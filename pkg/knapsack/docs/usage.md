# Usage

## Normalize once

Public dimensions and mass use `measurement`. A request chooses positive
length and mass resolutions before search. Every quantity must be an exact
integer multiple of its resolution; there is no implicit rounding policy.
Callers that need rounding must do it explicitly before constructing the
request and document that application policy.

`knapsack.NewRequest` validates, normalizes, owns defensive copies, and sorts
stable IDs. `Request.Normalized` returns another immutable copy suitable for a
solver, verifier, encoder, or benchmark.

## Pack all

Use `solver.Heuristic.PackAll` for ordinary e-commerce orders. The heuristic
selects from the supplied container types, respects finite stock, and returns
`feasible`, `best_known`, or `budget_exhausted`; it never reports `optimal` or
proven `infeasible`.

Use `solver.Exact.PackAll` only for bounded small inputs. It exhaustively checks
the relevant compact placement and container-multiset search. An `optimal` or
`infeasible` result is truthful only when termination is `completed`.

Set `ContainerTypeSpec.CenterOfGravity` when packed content must remain inside
inclusive X/Y/Z parts-per-million bounds. The solver and independent verifier
use exact mass moments and uniform-cuboid geometric centers. Exact search
enumerates the full bounded lattice for such containers because ordinary
compact-placement reductions do not preserve every balanced solution.

## Pack fixed containers

`PackFixed` takes stable container instance IDs and existing type IDs. It
cannot create another instance. Set `AllowUnpacked` when a partial result is an
accepted outcome. Without that option, unpacked items remain explicit and the
result is not converted into a proof of infeasibility.

## Verify plans

`verify.Plan(request, plan, verify.RequireAll())` checks a complete plan.
`verify.AllowUnpacked()` accepts explicitly listed unpacked IDs but still
rejects loss, duplication, geometry, weight, stock, support, relationship, and
accounting defects. Verification is structurally separate from placement
generation and is also suitable for plans produced by another system.

## Custom constraints and objectives

Implement `constraint.Placement` for application-specific placement rules.
Implement `objective.PlanObjective` for a pure lexicographic complete-plan
comparison, or use `objective.New` with built-in criteria. Callback panics are
converted to stable callback errors. Callbacks must honor cancellation and
must not retain views or perform unbounded work. They are trusted application
code; isolate any third-party implementation in a separately sandboxed
process before invoking the library.

Monetary container costs are additive through the nested
`github.com/faustbrian/golib/pkg/knapsack/objective/gomoney` module. The root module
does not invent a money type or require `money`. `gomoney.New` accepts at
most 1,000 costs with 1,024-byte type IDs. Use `gomoney.NewWithLimits` when a
larger trusted request has an explicit nonzero cost-map policy.

Runnable pack-all, fixed-container, verification, and custom-constraint
examples are maintained in `example_test.go` and checked by `make docs`.
