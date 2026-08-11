# Release-readiness findings

This is the required findings report for the pre-v1 hardening goal. It records
the current release decision, each material finding's severity and impact, its
disposition, and residual risk. The executable evidence remains authoritative;
this report does not turn a historical or partial result into a pass.

## Current decision

**Not ready for release.** The production API and documented Apache Kafka
policy are substantially implemented, but the release decision remains blocked
by the release-blocking findings below. The current tree has successful
content-matched exact mutation evidence, but it does not yet have a complete
frozen-tree release gate set or direct Amazon MSK evidence.

Severity means:

- **Critical**: independently blocks release because a mandatory correctness or
  evidence invariant is unproved.
- **High**: blocks the supplied production goal or a support claim.
- **Medium**: must be closed before making the affected optional capability or
  operational claim; it does not broaden the currently documented support
  matrix.
- **Low**: a deliberately bounded limitation or post-release evidence item that
  must remain visible.

## Release-blocking findings

| ID | Severity | Finding and impact | Disposition | Release condition |
| --- | --- | --- | --- | --- |
| KAF-F002 | Critical | The current release evidence set was collected across changing inputs. A pass whose complete gate fingerprint no longer matches the release candidate cannot establish release readiness, and not all nine manifest-derived reverse dependencies have final content-matched results. | Open. Reuse matching checkpoints and execute only stale Kafka gates and affected reverse dependencies. Do not restart unrelated modules or rerun a gate merely because `HEAD` changed. | Every mandatory Kafka gate and affected reverse dependency has a successful checkpoint for its complete final input fingerprint; no required result is failed, skipped, stale, missing, or warning-substituted. |
| KAF-F003 | High | Neither Amazon MSK Provisioned nor Serverless has been exercised. The optional IAM signer adapter has local contract, race, fuzz, cancellation, expiry, refresh, redaction, and allocation evidence, but that does not prove broker authentication, rolling operation, transactions, consumer groups, replay, or inspection on MSK. The supplied goal's tested-compatible-service requirement is not met. | Open. Documentation labels both modes unverified and unsupported, so no accidental support claim is permitted meanwhile. | Run the reviewed capability and authentication matrix against each MSK mode claimed as supported, record exact service/client/adapter/tool versions, and retain failure, rotation, lifecycle, and cleanup evidence. |

## Accepted conditional limitations

These findings are resolved for the current support matrix because the affected
optional profiles remain explicit non-claims. They become release blockers if
the corresponding support or performance claim is added.

| ID | Conditional severity | Finding and impact | Current disposition | Reopen condition |
| --- | --- | --- | --- | --- |
| KAF-F005 | Medium | The tiered-storage fixture uses Kafka's checksum-pinned test-only `LocalTieredStorage` plugin. It proves the package's inspection and replay behavior for that fixture, not a production remote-storage plugin or a managed tiered-storage service. | Accepted scope limitation; production remote storage remains unsupported. | Required only before adding a production plugin or managed-service support claim. |
| KAF-F006 | Medium | OAUTHBEARER is proven with local verified HTTPS JWKS and OAuth `client_credentials` fixtures, not a named external identity provider. External provider interoperability and performance are unknown. | Accepted scope limitation; the root contract remains provider-neutral and no external provider is claimed. | Required before naming an identity provider as supported or publishing provider-specific performance guidance. |
| KAF-F007 | Low | The observer reports the bounded package-local wait from franz-go's blocked-rebalance signal through poll-gate release, not complete broker-wide or multi-phase cooperative rebalance duration. Treating it as end-to-end rebalance time would mislead operators. | Accepted API limitation and documented telemetry boundary. | Keep metric names and documentation scoped unless an authoritative end-to-end signal becomes available. |
| KAF-F008 | Low | A previous released `kafka` version cannot yet be benchmarked because no prior release exists. | Deferred until the first upgrade comparison. | Add the previous release to equivalent benchmark captures before the next release for which one exists. |
| KAF-F009 | Low | The clean-consumer release dry run resolves a fresh `GOWORK=off` module through the repository's local source proxy. It proves module independence before publication, not public-proxy availability. | Expected pre-release boundary. | After publication, verify the immutable module version through the intended public or private proxy before advertising availability. |

## Resolved material findings

| ID | Former severity | Finding and impact | Disposition |
| --- | --- | --- | --- |
| KAF-F001 | Critical | Earlier mutation evidence was invalid after its aggregate output consumer disconnected and subsequent production and test input changes. | Resolved. The content-matched final campaign completed durably in 2 hours 26 minutes: the root package killed all 3,093 viable mutants and `adapters/golog` killed all 50, with zero lived, uncovered, timed-out, non-viable, or skipped mutants and exact 100% test efficacy and mutator coverage. The successful aggregate and per-package checkpoints remain valid for the current mutation input fingerprint. |
| KAF-R001 | Critical | A disconnected live output consumer could terminate a completed evidence gate with `SIGPIPE`, losing a valid aggregate result after a long mutation campaign. | Resolved in the shared evidence wrapper. Its regression closes stdout after the first gate line and proves durable log/checkpoint completion continues without a broken-pipe exit. The final 2-hour-26-minute Kafka campaign completed with durable aggregate and per-package checkpoints, proving the repair on the actual long-running path. |
| KAF-R002 | High | The draft exposed no single operator-ready set of capacity, alerting, rolling deployment, incident recovery, disaster recovery, migration, troubleshooting, MSK/ECS, and Kafka topic-design guidance. | Resolved in the documentation set and runnable examples. Documentation gates must still match the frozen release tree. |
| KAF-R003 | High | A shallow wrapper could have leaked franz-go clients, records, options, or administrator response types and allowed arbitrary options to bypass policy. | Resolved in the current public boundary: stable Kafka concepts are owned by this module and franz-go translation remains internal. Final API compatibility evidence must confirm that boundary. |
| KAF-F004 | High | PLAIN provider refresh alone did not prove zero-downtime broker credential rotation because Kafka 4.3.1's exercised default JAAS verifier does not reload in place. | Resolved with an overlap-first three-broker fixture. One producer adopts the new principal, preserves exact acks-all delivery across all three partitions while each broker restarts separately at RF=3 and `min.insync.replicas=2`, waits for ISR=3 between restarts, and proves every recovered broker accepts the new credential before all three reject the retired one. |

## Residual risks after the release blockers close

The following risks remain even for a release candidate that passes every
required gate:

- an acknowledgement can be lost after Kafka accepted a non-transactional or
  transactional operation, so bounded ambiguous outcomes still require
  application reconciliation rather than blind retry;
- at-least-once consumer handling can repeat successful external side effects
  when the process fails before source-offset settlement;
- dead-letter or retry-topic publication and source settlement are separate
  effects unless both are inside the supported Kafka consume-transform-produce
  transaction boundary;
- replay cannot reconstruct records already removed by retention, compaction,
  truncation, or an unclean election and intentionally fails closed for an
  exact unsatisfied range;
- ordering is per partition, not global, and partition expansion can change
  future key placement;
- local readiness hysteresis reduces restart storms but does not replace
  deployment-level disruption budgets, broker monitoring, capacity planning,
  or disaster-recovery exercises; and
- Kafka transactions do not make databases, HTTP calls, object storage,
  notifications, or any other external system atomic with Kafka.

These are Kafka or application-system boundaries, not waivers for the
release-blocking findings. Any new guarantee, broker, authentication mode,
adapter, or deployment profile requires its own directly attributable evidence
before this report can mark it supported.

## Final review procedure

Before changing the decision to ready:

1. freeze production code, tests, fixtures, generated files, manifests,
   dependency pins, gate scripts, documentation, and required service images;
2. compare every stored gate fingerprint with the frozen inputs and execute
   only stale Kafka gates and affected reverse dependencies;
3. run the single final mutation campaign after all faster checks pass;
4. verify the Apache Kafka minimum/current, authentication, failure,
   multi-process, replay, transaction, compatibility, and benchmark artifacts
   named by the compatibility matrix;
5. close every Critical and High finding and retain every unsupported profile
   as an explicit non-claim; and
6. review the complete release diff and update this report from the resulting
   content-addressed evidence.
