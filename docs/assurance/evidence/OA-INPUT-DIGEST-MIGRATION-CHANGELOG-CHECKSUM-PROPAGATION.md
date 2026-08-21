# Changelog And Checksum Propagation Evidence Migration

Observed at `2026-08-18T16:09:16Z` on `darwin/arm64`.

## Change Boundary

The owned dependency version-normalization batch added required Unreleased
entries to the affected module changelogs. Those changelogs are intentionally
excluded from operational and mutation behavior fingerprints, but they are
part of each module's published source archive. Rebuilding the task-owned
local `v0.0.0` source proxy therefore changed module archive checksums, and
tidying reverse dependants propagated the new checksums into their `go.sum`
files.

No production source, test behavior, runtime configuration, service image,
external dependency, public API, or owned dependency edge changed through
this propagation. The only production-test change in the wider batch is the
separately evidenced tenancy unrelated-header regression.

## Affected Inputs

The propagation changed operational-assurance fingerprints for 28 modules:

- `pkg/audit/postgres`
- `pkg/authentication/authotel`
- `pkg/authentication/jwt`
- `pkg/authentication/oidc`
- `pkg/authorization`
- `pkg/cache`
- `pkg/cloudevents/adapters/golib`
- `pkg/config`
- `pkg/config/adapters/awssecretsmanager`
- `pkg/idempotency`
- `pkg/international`
- `pkg/kafka/kafkaservice`
- `pkg/knapsack/objective/gomoney`
- `pkg/lease`
- `pkg/localized`
- `pkg/migrations`
- `pkg/money`
- `pkg/opening-hours`
- `pkg/postgres`
- `pkg/queue-control-plane`
- `pkg/queue/queueservice`
- `pkg/rate-limit`
- `pkg/rule-engine/adapters/temporal`
- `pkg/scheduler`
- `pkg/service`
- `pkg/telemetry`
- `pkg/temporal`
- `pkg/webhook`

The exact sorted set of 28 `{module, from_sha256, to_sha256}` records is
stored in `operational-assurance.json`. Its canonical compact JSON has SHA-256
`4d04dff5068766be784e01271d63f7c9d33f79c2fcf8bf716cae941e92ee79fb`.

## Verification

The complete 48-module direct and reverse-dependant closure passed its final
strict contract after checksum propagation. This included isolated tests,
race detection, exact per-package statement coverage, lint, static analysis,
vulnerability and supply-chain gates, fuzzing, documentation, API
compatibility, interoperability policy, benchmarks, and goal traceability.

Mutation-required packages reused completed checkpoints only where the
canonical mutation fingerprint proved identical production source, tests,
fixtures, external dependencies, tools, and service inputs. Changelogs remain
excluded from mutation behavior inputs, while owned dependencies are resolved
from current repository source. No mutation campaign was restarted for this
documentation-only archive change.

## Release Claim Boundary

This migration does not claim that release archives remain byte-identical.
The prior all-module proxy manifest SHA-256
`3984396deec43710f61962486107d91a387a5c9c6f0264cffa0e1a83b0312882`
is superseded because module changelogs legitimately changed published source
archives. Final dependency-ordered `v1.0.0` release and clean-consumer proof
must be regenerated against the current complete tree and bound separately.

This evidence authorizes only the exact one-way digest migrations above. It
does not authorize migration across future production source, tests, runtime
configuration, dependencies, tools, services, or release-content changes.
