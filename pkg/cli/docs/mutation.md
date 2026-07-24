# Mutation testing

`make mutation` runs pinned Gremlins against production command and parser
code. The gate requires 100% efficacy: every executed mutant must be killed.
It also requires at least 98.5% mutator coverage so ordinary production logic
cannot silently fall outside the behavioral suite.

The test-support package, checked-in reference application, and reference
generator are excluded from mutation scoring. They are development tooling,
remain covered by ordinary tests and generated-file drift checks, and are not
part of the runtime command framework.

The final reviewed run on 2026-07-22 killed all 674 covered mutants. There were
no survivors or timeouts. Seven mutants were not covered, producing 100% efficacy
and 98.97% mutator coverage:

| Location | Mutation | Equivalent assertion |
| --- | --- | --- |
| `argument.go:10` | arithmetic change to enum `iota` base | public argument constants and behavior tests |
| `command.go:203` | arithmetic change to option-group enum base | option-group public constants and exhaustive behavior tests |
| `compile.go:543` | negated enum-default guard | valid and invalid enum default regressions |
| `run.go:16` | arithmetic change to cleanup timeout | bounded cleanup deadline assertion |
| `run.go:452` | negated default-state branch | omitted, defaulted, and explicit value-state tests |
| `run.go:452` | boundary change to default-state branch | omitted, defaulted, and explicit value-state tests |
| `shutdown.go:17` | arithmetic change to shutdown action enum base | public action constants and shutdown policy tests |

Gremlins cannot associate these declaration and control-flow mutations with
their consuming assertions, but each contract is directly exercised. Any
future survivor is a release blocker. A future uncovered mutant also blocks a
release unless this report records a precise equivalent or unkillable rationale
reviewed by a maintainer.
