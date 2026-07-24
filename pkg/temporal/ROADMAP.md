# Roadmap

## v1

- Complete bounded instant/date/daily algebra and normalized sets.
- Publish strict encoding and PostgreSQL losslessness guarantees.
- Maintain 100% meaningful production statement coverage and release gates.
- Publish complete migration and adoption documentation.

## Later

- Consider an optional `temporalchart` module after its rendering contract,
  terminal capability policy, accessibility, and fixtures are independently
  specified. Rendering will consume public immutable values and will not enter
  core packages.
- Add adapters only where another owned module has a stable explicit contract.

Charting remains a known unsupported compatibility gap, not an implicit v1
promise.
