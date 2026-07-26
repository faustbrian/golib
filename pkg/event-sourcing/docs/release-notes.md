# Release notes and compatibility

The changelog in each independently releasable module is the authoritative
release-note source for that module:

- [core event sourcing](../CHANGELOG.md);
- [PostgreSQL](../postgres/CHANGELOG.md);
- [Kafka](../adapters/gokafka/CHANGELOG.md);
- [queue](../adapters/goqueue/CHANGELOG.md);
- [outbox](../adapters/gooutbox/CHANGELOG.md); and
- [OpenTelemetry](../adapters/gotelemetry/CHANGELOG.md).

Until a first versioned release is published, these changelogs describe
unreleased behavior and no compatibility promise applies. A release note must
not imply that a local check, adapter test, or benchmark is deployed or
production-verified.

## Semantic-versioning surfaces

Treat the following as compatibility-sensitive even when their Go declarations
do not change:

- event names, schema versions, aliases, encoded payloads, and message codecs;
- envelope validation, ownership, metadata reservations, and redaction;
- store ordering, expected versions, commit outcomes, iterator behavior, and
  error inspection;
- PostgreSQL schemas, migrations, indexes, transaction and position semantics;
- snapshot and checkpoint encodings;
- queue and Kafka record mappings, keys, headers, settlement, and replay modes;
- metrics, span names, attributes, and cardinality limits;
- module paths, package contracts, minimum Go version, and generated artifacts.

A breaking change to one nested adapter does not require coupling the core
module's release, but every affected module needs its own changelog entry and
directory-prefixed semantic-version tag.

## Release evidence

Before publishing release notes as final, bind them to the immutable release
revision and require the repository's inventory, formatting, tidy, vet, lint,
static analysis, tests, race, exact coverage, mutation, fuzz, vulnerability,
secret, license, SBOM, provenance, docs, API compatibility, generated-code,
integration, clean-consumer, and affected-module gates. Missing or skipped
evidence is a release blocker, not a warning.

Record exact dependency and service-image versions, compatibility-matrix
baseline, migration requirements, known limitations, deprecations, security
impact, rollback boundaries, and raw performance evidence. PostgreSQL, queue,
Kafka, outbox, and telemetry guarantees must remain separate from application
and deployment responsibilities.

## Upgrade workflow

1. read every affected module changelog between the current and target tags;
2. inspect event, wire, schema, error, metric, and repository-contract changes;
3. back up authoritative history and rehearse migrations on a restored copy;
4. run application codecs, upcasters, snapshots, projections, process managers,
   stores, dispatchers, and adapters through their conformance suites;
5. rebuild derived data where the release note requires it;
6. deploy with bounded observation and a rollback or reconciliation procedure;
7. retain old decoders and keys while any live history or backup needs them.

EventSauce conceptual compatibility is tracked separately in the
[versioned matrix](compatibility/eventsauce-3.9.1.md). It does not imply PHP
source compatibility or unspecified wire compatibility.
