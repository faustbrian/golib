# Owned Dependency Version Normalization Evidence

Observed at `2026-08-16T03:31:23Z` on `darwin/arm64`.

## Change Boundary

Eighteen non-specialist module manifests replaced canonical Golib owned-module
pseudo-versions with the repository's unpublished local version `v0.0.0`.
No module path, dependency edge, source file, test, fixture, generated file, or
public API changed. Tidying `pkg/outbox/adapters/otel` aligned its indirect
`golang.org/x/sync` and `golang.org/x/text` versions with the current local
`pkg/outbox` graph; its complete strict contract passed after that resolution.
This adapter is not part of a passed operational-assurance scenario migrated
by this evidence. The Kafka and lease specialist scopes were not modified.

## Content-Identity Proof

Two task-owned clean clones started from source commit
`65acb13e8166e27c8a9723416214556f07d5e674`. One clone retained the original
manifests. The other received only the eighteen normalized `go.mod` files.
Each clone independently built the complete 107-module local source proxy at
exact `v1.0.0`.

The complete proxy trees contained 428 files. Sorted per-file SHA-256
manifests were byte-for-byte identical. Their common manifest SHA-256 was
`3984396deec43710f61962486107d91a387a5c9c6f0264cffa0e1a83b0312882`,
which is also the manifest recorded by the original all-module clean-consumer
campaign.

This proves that release packaging, module archives, published manifests, and
the dependency graph consumed at the planned initial `v1.0.0` are unchanged
by the normalization. The clones and generated proxies were task-owned and
removed immediately after comparison.

## Mutation Identity

Mutation fingerprints canonicalize only owned-module version locators because
mutation dependency discovery replaces every owned module with its current
repository source before resolving observers. A focused regression proves
that changing an owned requirement between an immutable pseudo-version and
`v0.0.0` does not change the mutation fingerprint while source, tests,
fixtures, external dependencies, tools, and services remain tracked.

For `pkg/authentication`, the normalized current fingerprint and the retained
completed mutation fingerprint both equal
`1c4275741d97557117662446f43abeedd10c9775c26ea1861a99ba5f79c3faeb`.
No mutation campaign was restarted for this metadata-only change.

The corrected fingerprint algorithm was first evaluated against every current
mutation package in the seventeen mutation-required normalized modules. Six
modules already retained the canonical fingerprint. The remaining eleven
modules required 23 exact package-level identity migrations because their
completed checkpoints had fingerprinted the literal owned pseudo-version
before canonicalization:

| Module | Migrated package checkpoints |
| --- | ---: |
| `pkg/authorization` | 2 |
| `pkg/cache` | 1 |
| `pkg/config` | 1 |
| `pkg/config/adapters/awssecretsmanager` | 1 |
| `pkg/international` | 1 |
| `pkg/migrations` | 1 |
| `pkg/postgres` | 1 |
| `pkg/queue-control-plane` | 8 |
| `pkg/scheduler` | 2 |
| `pkg/service` | 4 |
| `pkg/telemetry` | 1 |

The complete affected reverse-dependant closure then identified another 25
checkpoints whose observers resolved one of those normalized manifests:

| Reverse-dependant module | Migrated package checkpoints |
| --- | ---: |
| `pkg/audit/postgres` | 1 |
| `pkg/authentication/authotel` | 1 |
| `pkg/authentication/jwt` | 1 |
| `pkg/authentication/oidc` | 1 |
| `pkg/cloudevents/adapters/golib` | 1 |
| `pkg/idempotency` | 1 |
| `pkg/knapsack/objective/gomoney` | 1 |
| `pkg/localized` | 10 |
| `pkg/money` | 3 |
| `pkg/opening-hours` | 1 |
| `pkg/queue/queueservice` | 1 |
| `pkg/rate-limit` | 1 |
| `pkg/temporal` | 1 |
| `pkg/webhook` | 1 |

Each of the 48 migrations binds the module, package, completed execution
revision, previous fingerprint, replacement fingerprint, Gremlins version,
and exact mutation report SHA-256. Every migrated checkpoint contains only
killed mutants. The sorted canonical set of 48 migration records has SHA-256
`24a831308a3c8c1f0d34f4b512c3c21d22e32254c14cc963a107c200d057ef0a`.
The remaining direct and reverse-dependant checkpoints already matched the
canonical result and required no migration.

## Aggregate Follow-Up

The strict 126-module non-specialist aggregate completed with exactly two
failed boundaries: `pkg/prompts` tidy verification observed the unrelated
working-tree-only `pkg/prompts/go.sum`, and `pkg/tenancy/http` reported 42 of
43 statements covered because map iteration made an existing unrelated-header
case nondeterministic. No other required aggregate gate failed.

`pkg/tenancy` was corrected with a deterministic behavioral assertion that a
request containing only an unrelated header returns
`ErrTenantMetadataMissing`. Its complete strict module contract then passed on
the current source tree. Coverage reported 43 of 43 statements for
`pkg/tenancy/http` and exact 100% coverage for every production package. The
changed HTTP package killed 28 of 28 viable mutants; the unchanged root,
JSON-RPC, and PostgreSQL package checkpoints were reused by exact content
identity.

`pkg/prompts` was verified separately in a task-owned clean clone containing
the current tracked source and retained gate artifacts but excluding the
unrelated working-tree `go.sum`. Its complete strict module contract passed.
Both mutation checkpoints were reused by exact content identity; no mutation
campaign was restarted. NilAway remained advisory and reported its existing
diagnostics as required by repository policy.

The strict affected and reverse-dependant closure then exercised 48
non-specialist modules from verification snapshots. Forty-six modules
completed their full contracts. No mutation campaign restarted: every
mutation-required package reused content-identical completed evidence. Two
boundaries failed before completing their contracts:

- `pkg/outbox/adapters/otel` required its indirect `golang.org/x/sync` and
  `golang.org/x/text` versions to match the normalized local `pkg/outbox`
  graph;
- `pkg/config` API analysis could not import the normalized local
  `pkg/service` while its retained sum still identified the obsolete
  pseudo-version graph.

All eighteen normalized modules were subsequently tidied through the local
`v0.0.0` source proxy. Their owned-module sum entries now identify the local
source archives and contain no owned pseudo-versions. A four-lane isolated
tidy verification passed for all eighteen modules. Focused isolated API
compatibility for `pkg/config` also passed against the resulting graph.

The complete strict contract was then rerun for all eighteen normalized
modules from parallel-safe verification snapshots. Every required gate passed,
including isolated tests, race detection, exact per-package coverage, lint,
static analysis, vulnerability scanning, secret scanning, licensing, SBOM,
fuzzing, mutation, documentation, API compatibility, interoperability policy,
benchmarks, and goal traceability. Every mutation-required package reused
content-identical or reviewed content-identical evidence; no mutation campaign
restarted. Task-owned Go caches and temporary trees were removed after the run.

## Operational-Assurance Identity

Operational-assurance fingerprints changed for 32 modules represented by
passed scenarios. Fifteen are normalized modules:

- `pkg/authentication`
- `pkg/authorization`
- `pkg/cache`
- `pkg/config`
- `pkg/config/adapters/awssecretsmanager`
- `pkg/correlation`
- `pkg/http-client`
- `pkg/international`
- `pkg/migrations`
- `pkg/postgres`
- `pkg/queue-control-plane`
- `pkg/retry`
- `pkg/scheduler`
- `pkg/service`
- `pkg/telemetry`

The other 17 are unchanged observers whose dependency closure includes a
normalized module:

- `pkg/audit/postgres`
- `pkg/authentication/authotel`
- `pkg/authentication/jwt`
- `pkg/authentication/oidc`
- `pkg/cloudevents/adapters/golib`
- `pkg/idempotency`
- `pkg/kafka/kafkaservice`
- `pkg/knapsack/objective/gomoney`
- `pkg/lease`
- `pkg/localized`
- `pkg/money`
- `pkg/opening-hours`
- `pkg/queue/queueservice`
- `pkg/rate-limit`
- `pkg/rule-engine/adapters/temporal`
- `pkg/temporal`
- `pkg/webhook`

For each module, the retained completed digest and current digest differ only
through the owned dependency locator and checksum normalization proved above.
The exact sorted set of 32 `{module, from_sha256, to_sha256}` records is stored
in `operational-assurance.json`; its canonical compact JSON has SHA-256
`17545b3a45aab0dae9bd88dde950ec3dbe31ff8328739195d71576f6c1108bc1`.
No passed scenario implementation, fixture, service version, runtime source,
or external dependency changed across these 32 identity transitions.

## Claim Boundary

This evidence authorizes exact input-digest migration only where the changed
owned-version text is non-behavioral under the repository workspace or local
source proxy. It does not replace module gates whose isolated resolution now
changes from immutable remote pseudo-versions to task-owned local `v0.0.0`
artifacts, and it does not authorize migration across source, test, tool,
service, configuration, or release-content changes.
