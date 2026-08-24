# Continuous Integration

`.github/workflows/ci.yml` is the only owned workflow. Pull requests select
changed modules from the merge base and expand through reverse owned
dependencies. Pushes to `main`, schedules, releases, and manual runs select all
active modules.

Manual runs expose a `release_dry_run` mode. That mode selects only releasable
modules and gives each module an attributable job that starts its cataloged
pinned services and executes the exact initial `v1.0.0` release dry-run. It
does not repeat the strict module matrix, CodeQL, or the Kafka arm64 contract;
it is a release rehearsal to run only after the same source revision has a
successful complete CI run. The repository contract and stable `Required` job
remain mandatory, and every module uploads its release log.

The visible matrix has one job per selected module. Each job runs
`scripts/run-modules.sh check --modules <directory>`, starts cataloged pinned
services, and uploads attributable coverage, mutation, SBOM, conformance, and
failure evidence. Repository-contract validation runs independently so a root
metadata failure remains visible and fail-closed without suppressing module or
CodeQL results. A stable `Required` job fails unless selection, the repository
contract, every module, and CodeQL succeed.

Cancellation is limited to superseded pull-request runs. Actions are pinned to
immutable revisions. Forks receive no secrets. Before module verification, CI
scans trusted same-repository `main` verification artifacts from newest to
oldest and restores the newest mutation checkpoint that either matches each
package's complete current input fingerprint and verifier identity or has an
exact reviewed identity migration to them. Release-rehearsal artifacts use a
separate name and cannot shadow verification evidence. A partial or stale
newer artifact therefore cannot force execution when an older
content-identical checkpoint is available. The mutation gate independently
revalidates every restored checkpoint; missing, malformed, stale, or untrusted
checkpoints execute normally. Coverage, generated, conformance, and benchmark
evidence is never restored, and mutation results are never accepted from a
permissive cache key or another repository. The stock runner's `gh` and
`unzip` tools are mandatory for this restore path; their absence fails rather
than silently reverting every package to a fresh mutation campaign.

The root matrix entry provisions the pinned Node runtime and runs the same
documentation script as `make docs MODULES=.`. Its spelling and external-link
checks are therefore mandatory CI gates rather than advisory scheduled jobs.

Every module job provisions ripgrep 15.2.0 from its checksum-pinned static
Linux archive before executing package checks. This prevents a missing search
tool from being misread as an empty safety or documentation result.

The four workflow files inside the pinned upstream JSON Schema test corpus are
fixture provenance, not repository workflows, and GitHub does not execute them.
