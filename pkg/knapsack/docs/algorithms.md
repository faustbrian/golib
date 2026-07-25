# Algorithms, determinism, and limits

## Deterministic heuristic

The production heuristic canonically sorts items and container types,
generates extreme points, enumerates allowed orientations, rejects infeasible
geometry and physical constraints, and compares complete trial plans with the
selected lexicographic objective. Canonical height, depth, width, origin,
orientation, type ID, and instance ID ordering resolves ties.

Pack-all performs one bounded global repacking pass with an independent
width-first placement order, then selects the canonical objective winner. This
can recover from container fragmentation caused by the primary height-first
order without weakening verification. Candidate work is shared across both
passes, `Limits.MaxImprovementRounds` bounds attempted repacking, and
`Work.ImprovementRounds` reports completed or interrupted passes.

If `n` items, `b` current bins, `p` retained extreme points, and `r`
orientations are considered, the placement phase is approximately
`O(n*b*p*r)` before objective and constraint cost. NP-hard solution quality is
not bounded by that expression and no heuristic optimality claim is made.

## Exact small-instance oracle

The exact solver enumerates bounded container multisets and compact Cartesian
products of placement coordinates derived from placed and reserved cuboid
faces. It explores item order, container, point, and orientation choices. It
uses finite-stock elimination, compact-coordinate completeness for orthogonal
cuboids, and canonical duplicate reduction. Worst-case work is exponential;
the solver is deliberately limited to small instances.

When a container has center-of-gravity bounds, compact face coordinates alone
are not complete: a balanced solution may require a non-contact coordinate.
Exact search therefore enumerates the full integer lattice for that container,
subject to the same pre-allocation memory and candidate budgets. Exhaustion is
reported as a resource limit, never as infeasibility.

The exact solver reports `optimal` only after the complete bounded relevant
space is exhausted. It reports `infeasible` only after the same proof search
finds no plan. Tests compare exhaustively enumerable cases and independently
verify every candidate result.

Pack-all starts from the maximum of admissible aggregate-volume and
aggregate-mass lower bounds, using the largest usable container capacity. The
bounds use arbitrary-precision totals and cannot prune a feasible solution.
Container multisets are enumerated in nondecreasing canonical type order, so
permutations of the same multiset are eliminated without losing a choice.
`solver/internal_test.go` compares both bounds and canonical multiset forms
against independent relaxed brute-force enumeration.

## Reproducibility

For equal normalized requests, options, version, and seed, output is canonical
and independent of input permutation, Go map iteration, CPU count, and
goroutine scheduling. Current solvers are sequential; a future parallel solver
must expose a bounded profile and reduce candidates canonically.
Elapsed wall time is not serialized into plans, avoiding timing-dependent
bytes before an explicit deadline is exhausted.

## Resource limits

`knapsack.Limits` bounds items, container types, orientations, candidates,
nodes, branches, improvement rounds, estimated memory, ID bytes, and
diagnostics. Solvers also accept `context.Context`.
Cancellation, deadlines, candidate, node, branch, and memory exhaustion return
the best independently verified retained plan when safe, with an exact
termination reason. Limits should be reduced before accepting untrusted work.
