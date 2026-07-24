# Changelog

All notable changes follow Keep a Changelog. The project uses semantic
versioning after the first stable release.

## Unreleased

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Immutable quantities backed exclusively by `math/decimal`.
- Closed dimensions and explicit exact or rounded conversion contexts.
- SI and logistics units for length, area, volume, mass, temperature, density,
  and loading metre.
- Compatible arithmetic, comparison, rounding, clamping, and package counts.
- Validated dimension triples, volume, floor area, loading metre, volumetric
  divisor, and volumetric index formulas.
- Lossless JSON, XML, SQL, and bounded `wire` adapters.
- Property, fixture, fuzz, race, mutation, coverage, and benchmark gates.

- `NewProfile` now returns an error and rejects oversized or invalid alias
  catalogs.
- JSON and XML decoding rejects duplicate fields; direct constructors enforce
  the default `math` decimal limits.
