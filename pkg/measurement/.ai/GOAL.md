# Goal: Typed Logistics Measurements And Unit Conversion

## Objective

Build `measurement` as an immutable, exact, unit-safe measurement package for
Track, Postal, Location, and broader logistics services. It MUST use `math`
for decimal arithmetic and prevent incompatible dimensions from being combined.

## Scope

- Length, area, volume, mass, temperature where required, density, loading
  metre, dimensional/volumetric weight, and dimension triples.
- SI units and explicitly required logistics units with exact conversion ratios.
- Immutable quantities carrying dimension and unit identity.
- Parse, convert, compare, add/subtract compatible quantities, multiply/divide
  into derived dimensions, round, clamp, and format.
- Package dimension validation, cubic volume, floor/loading metres, stacking
  factors, quantity multiplication, truck-width calculations, and volumetric
  divisor/index calculations.
- Explicit conversion and rounding contexts; no hidden locale or preferred
  unit inference.
- Optional JSON, XML, SQL, and `wire` adapters preserving unit metadata.

## Boundaries

- No carrier pricing, packaging recommendation, route optimization, or business
  tariff rules.
- No generic symbolic algebra or unlimited physical-unit ontology in v1.
- `money` owns monetary values; `geo` owns coordinates/distances on earth.
- Unit symbols and aliases are parsed under explicit locale/profile policy.

## Quality

Require meaningful 100% production coverage, dimensional property tests,
conversion round trips, official SI constants, logistics fixtures, fuzzing,
race and alias tests, mutation testing, overflow/scale limits, and benchmarks.

Document API, supported dimensions/units, exactness, rounding, dimensional
analysis, logistics formulas, serialization, migration from
`shipit/measurements`, security, performance, cookbook, FAQ, compatibility, and
changelog. CI and local gates follow ecosystem standards.

## Acceptance Criteria

- Incompatible dimensions cannot be combined accidentally.
- Every conversion ratio and logistics formula is explicit and tested.
- No float conversion or duplicate decimal type is required.
- Loading metre, volume, mass, dimensions, and volumetric weight cover current
  Location and carrier payload needs.
- Meaningful 100% coverage and every blocking gate pass.
