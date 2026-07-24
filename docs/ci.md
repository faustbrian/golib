# Continuous Integration

`.github/workflows/ci.yml` is the only owned workflow. Pull requests select
changed modules from the merge base and expand through reverse owned
dependencies. Pushes to `main`, schedules, releases, and manual runs select all
active modules.

The visible matrix has one job per selected module. Each job runs
`scripts/run-modules.sh check --modules <directory>`, starts cataloged pinned
services, and uploads attributable coverage, mutation, SBOM, conformance, and
failure evidence. A stable `Required` job fails unless selection, every module,
and CodeQL succeed.

Cancellation is limited to superseded pull-request runs. Actions are pinned to
immutable revisions. Forks receive no secrets. Caches must never provide
coverage, mutation, generated, conformance, or benchmark evidence.

The four workflow files inside the pinned upstream JSON Schema test corpus are
fixture provenance, not repository workflows, and GitHub does not execute them.
