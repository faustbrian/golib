# Verification

The module gate runs formatting, module tidiness, vet, architecture checks,
unit and example tests, race tests, exact statement coverage, fuzz targets,
mutation checks, leak tests, benchmarks, documentation links, API compatibility,
lint, static analysis, and vulnerability scanning. The repository release gate
adds secret, license, SBOM, provenance, supply-chain, and clean-consumer checks.

Deterministic clock tests control hedge and deadline races. Concurrency tests
exercise shared budgets, cancellation, late publication, disposal, and cleanup
waiting. Green coverage alone is not proof; each mutation target must be killed
by a behavioral assertion.
