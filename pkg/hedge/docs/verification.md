# Verification

The module gate runs formatting, module tidiness, vet, architecture checks,
unit and example tests, race tests, exact statement coverage, fuzz targets,
mutation checks, leak tests, benchmarks, documentation links, API compatibility,
lint, static analysis, vulnerability scanning, secret and license checks, and a
clean external consumer. Repository release automation adds SBOM and provenance
checks.

Deterministic clock tests control hedge and deadline races. Concurrency tests
exercise shared budgets, cancellation, late publication, disposal, and cleanup
waiting. Green coverage alone is not proof; each mutation target must be killed
by a behavioral assertion. The canonical mutation gate requires every viable
mutant to be killed with no survivors, timeouts, or uncovered mutants.

Named deterministic-scheduling and fault-path gates repeat timing-independent
selection tests and exercise bounded factory, attempt, delay, cleanup, and
observer failures. The clean-consumer gate compiles and executes the public API
from a fresh module with workspace mode disabled. Supply-chain checks verify
module checksums, read-only dependency resolution, license disclosure, and the
absence of committed secrets.
