# Architecture

Dependencies point inward toward the root immutable domain and exact geometry.
Solvers generate candidates; the verifier does not call their placement
predicates. Optional money and visualization functionality remain additive.

The heuristic sorts by volume, weight, and stable ID, evaluates canonical
extreme points, and chooses containers by explicit priority, size, and ID. The
exact oracle enumerates compact coordinates and item/container permutations
under node, branch, candidate, context, and stock bounds. Its worst-case work
is exponential because orthogonal bin packing is NP-hard.

X is width, Y depth, and Z height from the lower-left-front origin. Cuboids use
half-open intervals. Stability is a discrete support-area and load contract,
not a continuous mechanics simulation.

The root package depends only on exact measurement, arithmetic, and geometry
contracts. `constraint` and `objective` consume immutable root views. `solver`
depends inward on those contracts. `verify` independently recomputes built-in
invariants and replays extension contracts through their immutable views; it
never calls solver predicates. `encoding`, `visualize`, and `knapsacktest` are
leaf consumers.

Monetary comparison is isolated in the nested `objective/gomoney` module and
differential dependencies in `integration/references`. Built-in geometry and
physical rules remain direct solver/verifier code rather than a slower generic
callback path. Extension interfaces never expose mutable bins or search state.
They are trusted in-process application extensions: the library bounds their
inputs and outputs but cannot preempt arbitrary Go code that ignores context.
