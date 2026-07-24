# E-commerce recipes

## Multiple box types and finite stock

Supply every eligible `ContainerType` with a stable ID and either
`UnlimitedStock()` or `FiniteStock(n)`. Priority is a deterministic tie-break,
not a hidden objective. Choose container count, monetary cost, or unused
capacity precedence explicitly in the objective.

## Upright and keep-flat items

Pass only the physical-axis orientations that are allowed. An upright item may
use just `geometry.OrientationXYZ`; a keep-flat item may allow rotations that
leave its original Z axis mapped to Z. The verifier rejects any forbidden
orientation even in an externally supplied plan.

## Fragile and load-bearing items

Use `FragileTop` to prohibit another item from using that top face. Use
`MaxSupportedWeight` for an exact transitive load limit and
`MinimumSupportPPM` for a base-area rule. Set `MaxStackCount` when depth, rather
than weight alone, is constrained.

## Linked and incompatible items

Items with the same non-empty `Group` remain in one container or are rolled
back together. `IncompatibleGroups` prevents named groups from sharing a
container. These are requirements, not soft preferences.

## Keep-out regions and eligibility

Reserved cuboids remove exact usable space. `AllowedClasses` consumes the
explicit `class` item attribute. A non-rectangular container is not silently
treated as its bounding cuboid; applications must model safe rectangular
regions or use a separately specified constraint package.

## Impossible items

Inspect typed errors with `errors.Is` and structured unpacked diagnostics.
Distinguish impossible dimensions, overweight items, insufficient stock,
heuristic no-placement, proven infeasibility, and budget exhaustion. Never map
all of these conditions to `infeasible`.
