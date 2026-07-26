# Migration From shipit/measurements

Inventory every legacy numeric field together with its implicit unit before
changing types. Replace floats or bare decimals with `Quantity` at service
boundaries, using the payload's declared unit. Replace implicit conversions
with `ExactConversion` or a documented `RoundedConversion` context.

Map legacy width, length, and height structures to `NewDimensions`; reject
missing, zero, negative, or mixed non-length fields rather than preserving an
invalid zero value. Move loading-metre defaults, stacking decisions, and
volumetric divisors into caller configuration because they are carrier policy.

For persisted values, migrate to `{value,unit}` JSON or two explicit columns.
Do not assume historical rows use the current preferred unit. Dual-read old and
new representations during rollout, compare exact canonical values, then stop
writing the old shape. Preserve golden fixtures from Track, Postal, and
Location during migration.
