# Physical and result model

## Coordinates and cuboids

The origin is the lower-left-front corner. X is width, Y is depth, and Z is
height. Origins and dimensions are integer lattice coordinates. Occupied
intervals are half-open: `[x,x+width)`, `[y,y+depth)`, and `[z,z+height)`.
Touching faces and edges are allowed; positive-volume intersection is not.

The six orientation names describe where the original physical X, Y, and Z
axes are mapped. Repeated dimensions deduplicate equivalent rotations.
Rotation restrictions therefore refer to physical axes, never enum position.

## Units, exactness, and tolerance

Public length and mass quantities use `measurement`; exact conversion uses
`math`. Geometry, mass, volume, coordinates, scores, and accounting use
checked integers. `float32` and `float64` are not authoritative feasibility
values. Mixed units are converted, never treated as normalized values.

Physical measurement tolerance is an application input-normalization policy.
Search tolerance is a work or comparison policy. The library does not merge
them and does not apply a silent epsilon to overlap, containment, or support.

## Weight and the term 4D

Weight is a scalar container capacity and load-bearing quantity, not a fourth
geometric axis. “4D bin packing” is common e-commerce shorthand for three
dimensions plus weight. This operation packs required item instances and is
different from classical value-maximizing knapsack subset selection.

## Stability and load

`MinimumSupportPPM` is an exact parts-per-million ratio of supported base area.
Zero disables the minimum-ratio rule. Positive values require geometric
support from item top faces. `FragileTop`, `MaxSupportedWeight`, and
`MaxStackCount` constrain actual support relationships. Loads propagate
transitively and split by exact rational support area.

`CenterOfGravityBounds` optionally constrains the combined content-mass center
on X, Y, and Z as inclusive parts-per-million positions within the container's
internal dimensions. Each item is modeled as a uniform cuboid whose mass is at
its geometric center. Comparisons use exact doubled-coordinate integer moments;
tare mass is not included. These bounds are a discrete loading rule, not a
vehicle, axle-load, dynamic transport, or general physics simulation.

## Plans and proof status

Plans contain immutable container instances, placements, explicit unpacked
IDs, exact statistics, lexicographic score components, solver work, seed,
termination, and bounded diagnostics. `optimal` and `infeasible` are exhaustive
proof states. `feasible`, `best_known`, and `budget_exhausted` make no such
claim. A plan is not trusted until `verify.Plan` accepts it.
