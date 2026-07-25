# Verification

The local gates are:

- `make test`: unit, matrix, property, official-currency, SQL, JSON, and
  differential tests against `govalues/money` and `Rhymond/money`.
- `make coverage`: meaningful 100% production statement coverage; only
  invariant-proven defensive branches annotated in source are excluded.
- `make race`: shared immutable values and formatter concurrency checks.
- `make fuzz`: decimal, rate, allocation, JSON, PostgreSQL numeric, and locale
  formatting fuzzing.
- `make mutation`: Gremlins efficacy plus hand-selected mismatch, rounding,
  remainder, sign, rate, tax, discount, and conversion mutants.
- `make benchmark`: correctness-gated comparisons against maintained packages.
- `make check`: formatting, vet, static analysis, lint, NilAway, docs, API,
  dependencies, float contamination, and vulnerability checks.
- `make release-check`: every local release gate.

CI runs fast gates for every relevant change and scheduled fuzz, mutation, and
vulnerability jobs. A skipped required gate is not release evidence.
