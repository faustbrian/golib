# Capability matrix

| Capability | Status | Notes |
| --- | --- | --- |
| Pack all | Implemented | Variable container types and finite stock |
| Fixed containers | Implemented | Never invents an instance |
| Independent verification | Implemented | Geometry, identity, weight, stock, support, accounting |
| Orthogonal rotation | Implemented | Six physical-axis permutations, deduplicated |
| Exact small-instance oracle | Implemented | Bounded exhaustive compact search |
| Custom constraints | Trusted only | Immutable synchronous callback view; untrusted code requires process isolation |
| Content center of gravity | Implemented | Exact inclusive X/Y/Z PPM bounds |
| Monetary cost | Implemented | Additive `objective/gomoney` module |
| Subset selection | Deferred | Not advertised before pack-all stabilizes |
| Irregular geometry and cylinders | Out of scope | Caller must not treat bounding cuboids as exact geometry |
| Pallets, axle loads, robots, regulation | Out of scope | Separate domain-specific packages required |

## BoxPacker behavior assessment

| BoxPacker behavior | Classification | knapsack contract |
| --- | --- | --- |
| Rotation | Implemented | Six deduplicated physical-axis orientations |
| Item and box sortation | Intentionally different | Canonical IDs and explicit deterministic priorities |
| Weight capacity | Implemented | Exact scalar content and optional gross limits |
| Weight distribution | Intentionally different | Exact discrete support, transitive load, and content center-of-gravity bounds |
| Too-large items | Implemented | Explicit unpacked diagnostics or exact infeasibility proof |
| Positional information | Implemented | Lattice origin, oriented dimensions, and supporters |
| Limited box supply | Implemented | Finite stock checked by solver and verifier |
| Custom constraints | Trusted only | Typed immutable placement callback views |
| Linked items | Implemented | Required group co-location with rollback |
| Used and remaining space | Implemented | Exact plan volume and weight accounting |
| Single-pass online packing | Out of scope | Initial release is offline packing |
| Curved or irregular geometry | Out of scope | No implicit bounding-cuboid approximation |

Reference revisions, source hashes, license classifications, differential
semantic subsets, and corpus provenance are in `specification/references.tsv`,
`specification/corpora.tsv`, `integration/references`, and `NOTICE`.
