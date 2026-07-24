# API and option reference

The root package owns immutable items, container types, normalized requests,
plans, statuses, typed errors, and resource limits. `geometry` owns checked
integer cuboids and physical-axis rotations. `solver` exposes heuristic and
exact pack-all and fixed-container operations. `verify` validates supplied
plans independently. `constraint` owns immutable callback views. `objective`
defines exact lexicographic comparisons; its nested `gomoney` module adds
`money` costs without burdening root consumers. `encoding` owns strict
versioned JSON.

Zero-value items, containers, requests, plans, objectives, and limits are
invalid. Constructors defensively copy slices and maps. Solvers take
`context.Context`; cancellation and work exhaustion never become an
infeasibility proof. Public solver, objective, and verifier context entry
points reject a nil context with `knapsack.ErrInvalidOptions`.

## Root constructors and values

- `NewItem` validates stable identity and physical-axis orientations.
- `ExpandQuantity` creates deterministic `id#000001` instance identities.
- `NewContainerType` distinguishes usable dimensions, stock, content/gross
  weight, eligibility, reserved cuboids, and optional content center-of-gravity
  bounds.
- `NewRequest` converts measurements into an immutable normalized lattice
  request under `Limits`.
- `NewNormalizedRequest` is the validated encoding and fixture boundary;
  applications normally prefer `NewRequest`.
- `NewPlan` owns immutable result state but does not prove feasibility.

All collection accessors return copies. `NormalizedRequest.WithLimits` may
reduce work limits without rebuilding values; solvers reject a memory limit
below `MemoryBytes`.

## Solver options

`Seed` is serialized for reproducibility. `AllowUnpacked` selects partial
semantics. `Constraints` contains trusted typed placement callbacks; arbitrary
untrusted code requires an external process-isolation boundary. Choose either
a built-in `Objective` or a custom `PlanObjective`, never both.
Solver and verifier operations accept at most 32 non-nil placement callbacks.
An invalid verifier callback list is reported by `Result.Err`.

## Error handling

Use `errors.Is` for stable categories and `errors.As` for `*FieldError`.
Categories distinguish invalid domain values, duplicate IDs, inexact units,
overflow, impossible and overweight items, stock, constraint conflicts, no
heuristic placement, proven infeasibility, work and memory exhaustion, and
internal verification failure. Human messages are not machine contracts.

`constraint.NewPlacementView` returns an error and rejects more than 10,000
prior placements or a conservative owned-copy estimate above 16 MiB before it
clones callback-visible data. This is an intentional pre-v1 signature change;
most applications receive views through solver callbacks rather than building
them directly.

`objective.New` accepts at most the seven distinct built-in metrics and rejects
longer input before allocating validation state. `objective/gomoney.New` uses
safe defaults of 1,000 cost entries and 1,024 bytes per type ID;
`NewWithLimits` permits an explicit nonzero policy for larger trusted maps.

## Serialization

`encoding.MarshalRequest` and `MarshalPlan` are canonical. Decoders are
strict, versioned, bounded by bytes/depth/collections, reject unknown fields
and duplicate keys, reconstruct exact units, and validate before returning.
Persisted v1 request and plan fixtures under `encoding/testdata/v1` lock exact
bytes and are decoded, re-encoded, and independently verified in every check.
