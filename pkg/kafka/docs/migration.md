# Migration, deprecation, and upgrades

The module is pre-v1. Delivery semantics, defaults, record ownership, error
identity, ordering, offset settlement, transaction behavior, replay behavior,
metrics, and configuration are still treated as compatibility-sensitive
contracts. A pre-v1 version number is not permission to change them silently.

## Adopt from raw franz-go

Do not translate options mechanically. Start from the observable application
contract:

1. inventory every topic, keying rule, explicit partition, group, reset policy,
   transactional ID, isolation level, retry, timeout, buffer, callback, and
   shutdown path;
2. identify where the application currently depends on `kgo.Record`, `kgo.Opt`,
   client hooks, administrative responses, or error values;
3. select the package producer, consumer, transaction processor, replay reader,
   or inspector only where its explicit policy matches the required Kafka
   semantics;
4. keep unsupported advanced capabilities on a directly owned franz-go client
   instead of bypassing package invariants; and
5. migrate one workflow at a time with broker-visible delivery, settlement,
   ordering, and cleanup evidence.

The root policy requires bounded topic allowlists, keyed records by default,
all-ISR acknowledgements, idempotent production, explicit offset reset, manual
durable settlement, verified TLS, bounded callbacks, and error-returning close.
A migration that changes any of those behaviors needs an application decision,
not only a compilation fix.

## Upgrade this module

Before changing the module version:

1. read every Unreleased and intervening changelog entry;
2. compare the exported API and defaults, including nested adapters;
3. validate configuration without constructing clients;
4. review error categories and every `errors.Is`/`errors.As` dependency;
5. review byte ownership and callback lifetime rules;
6. run the affected module contract and every manifest-derived reverse
   dependency whose complete input fingerprint changed;
7. run real-broker scenarios for every changed delivery, settlement,
   transaction, replay, inspection, security, or hook boundary; and
8. deploy canaries with readiness hysteresis, bounded buffers, and rollback
   criteria before a broad rollout.

Do not rerun unrelated repository modules or invalidate expensive evidence only
because Git history changed. Reuse is valid only when the complete gate-input
fingerprint proves identity.

## Breaking pre-v1 changes

A breaking correction must include:

- the unsafe or ambiguous old contract and the replacement contract;
- affected release units and reverse dependencies;
- source migration with before/after examples;
- runtime rollout and rollback guidance;
- broker and application compatibility boundaries;
- changelog and API compatibility records; and
- fresh final-tree evidence for every affected module and broker scenario.

Owned CloudEvents, outbox, event-sourcing, telemetry, MSK IAM, service,
benchmark, and adoption modules must move in the same coherent change when they
consume the corrected contract. Passing only the root Kafka tests is not a
complete migration.

## Deprecation policy

Before v1, prefer one direct correction over a long-lived unsafe alias. When a
safe transition can be supported, a deprecation must name the replacement,
reason, semantic difference, and earliest removal release in public Go docs and
the changelog. Deprecated behavior must remain tested until removal.

After v1, remove or incompatibly change public behavior only in a major
release. Security or correctness defects may require a faster response, but the
release must still document impact, migration, rollback limitations, and every
known ambiguity. Never preserve an unrestricted franz-go option escape hatch
for compatibility.

## franz-go upgrade

Treat franz-go as an implementation dependency whose behavior can change even
when the package API still compiles. For each upgrade:

1. record the exact tag and revision and review release notes plus relevant
   source changes;
2. diff every selected producer, consumer, transaction, protocol-version,
   decompression, security, callback, and admin option;
3. verify idempotence, all-ISR acknowledgements, partitioning, retry timing,
   buffering, manual commits, isolation, rebalance callbacks, and close
   behavior remain equivalent to package policy;
4. rerun producer ambiguity, contiguous settlement, rebalance fencing,
   transaction unknown outcome, replay gaps, inspection normalization,
   authentication, and resource-leak evidence; and
5. refresh equivalent-client benchmarks without claiming improvement from
   changed defaults.

Do not infer a new broker support claim from franz-go's protocol table.

## Kafka broker upgrade

Upgrade operators own broker sequencing and rollback. The application review
must still:

- record the source and target versions, KRaft topology, inter-broker and
  metadata compatibility settings, and immutable image digests;
- review producer, consumer, transaction, security, quota, retention,
  compaction, remote-storage, and group-protocol release notes;
- verify the current client against both the minimum and target broker under
  the exact supported scenarios;
- inspect cluster identity, controller, leaders, replica/ISR state, topic
  policy, offsets, groups, and lag before and after each stage; and
- preserve rollback until new log, metadata, or protocol features make it
  impossible.

Enabling the KIP-848 consumer protocol is a separate application and group
migration. The classic cooperative/eager client path does not silently switch
to it.

## Authentication upgrade or rotation

Change one boundary at a time: trust roots, broker certificate, client
certificate, SASL credential, OAuth signing key, OAuth endpoint, or IAM role.
Use overlap-first trust and identity where the server supports it. Require a
successful new connection and an acknowledged/settled application operation
before retiring the old identity.

Never place credentials in a migration file, command history, broker URL,
fixture, changelog, or generated evidence. A provider change affects future
connections; it does not rewrite an established TLS or SASL session.

## Topic and keying migration

Topic names, partition counts, and key selection are data contracts. Changing a
key can reorder one logical entity across partitions. Expanding partitions can
also change automatic key-to-partition mapping. Renaming or versioning a topic
creates a new independent log and consumer history.

Plan dual publication or backfill only with explicit duplicate, ordering,
retention, and rollback rules. The package does not create topics, expand
partitions, copy records, or mutate offsets. Use the replay reader only for
explicit source ranges and application-approved side effects; it is not a
generic topic migration engine.

## Consumer rollout migration

When changing group ID, the new group has an independent history. Select and
document its initial offset policy; do not assume the old group's committed
position transfers. When keeping the group ID, preserve handler idempotency and
use the documented eager-to-cooperative two-deployment sequence if protocol
compatibility changes.

Static membership changes require one unique stable instance ID per live
member. Lowering handler or rebalance deadlines can turn previously successful
work into redelivery; increasing concurrency can expose previously hidden
application races.

## Rollback

Rollback is safe only while the old application understands every record,
header, topic, configuration, group protocol, and transaction behavior emitted
or selected by the new version. Before rollout, state whether rollback:

- can reuse the same consumer group and transactional identities;
- understands newly produced records and headers;
- preserves topic allowlists and keying;
- can authenticate with the current credential and trust set; and
- retains compatible broker and module versions.

If any answer is unknown, use a forward fix or a separately reviewed recovery
plan rather than an automatic rollback.
