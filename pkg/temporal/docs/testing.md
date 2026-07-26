# Testing evidence

Production statement coverage is exactly 100%, enforced by `make coverage`.
Confidence additionally comes from:

- all 13 Allen relations across all four bound modes;
- randomized union/intersection/difference conservation;
- exhaustive hourly circular complements across all bounds;
- fixed regression tests for surviving singleton subtraction boundaries;
- strict notation round trips and generated PHP fixtures for bounds, local
  values, duration arithmetic, time arithmetic, interval kinds, predicates,
  algebra, complement, splitting, and stepping;
- fuzz targets for instant/date/daily/duration/time notation, split progress,
  set normalization, versioned JSON, and PostgreSQL range text;
- race tests with concurrent reads over shared immutable values;
- PostgreSQL 18 range and multirange integration;
- Gremlins arithmetic and conditional mutation operators;
- allocation-reporting benchmarks for relations, parsing, 1,000-period
  normalization, splitting, daily algebra, and early limit rejection.

Every CI command has a matching `make` target. NilAway is advisory because the
upstream analyzer explicitly permits false positives; other configured gates
block releases.

The requirement-by-requirement truth tables, algebra laws, resource budgets,
interoperability runs, and mutation classifications are recorded in the
[hardening report](hardening.md).
