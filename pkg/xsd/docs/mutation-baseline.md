# Mutation baseline

The release mutation scope is the complete module, including the parser,
compiler, datatypes, resolvers, validator, serializer, builders, and XSTS
harness. Gremlins v0.6.0 runs the complete test suite with the pinned XSTS
archive available so official conformance expectations exercise every
supported mutant.

- Date: 2026-07-20
- Command: `make mutation`
- Total mutants: 2,625
- Killed: 2,625
- Survived: 0
- Not covered: 0
- Timed out: 0
- Not viable: 0
- Skipped: 0
- Mutation coverage: 100.00%
- Test efficacy: 100.00%
- Elapsed: 744 seconds

`mutation-results.json` records the machine-readable result. The gate fails
if any supported mutant is not exercised or if efficacy falls below the
configured threshold.
