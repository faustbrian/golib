# Event-sourcing adoption guide

Event sourcing is a persistence model for domains where the ordered business
transitions are the authoritative record. It is not a default replacement for
storing current state, and this package does not make its operational or
modeling costs disappear.

## Start with the decision

Event sourcing is worth evaluating when several of these are true:

- the domain must explain how and why current state was reached;
- historical reconstruction at a specific business time is valuable;
- business transitions and their ordering are stable domain concepts;
- multiple derived views must be rebuilt from the same authoritative history;
- concurrent decisions require explicit optimistic-conflict handling;
- corrections must preserve the original record and add an auditable change;
- the team can own schema evolution, replay, retention, and recovery for the
  lifetime of the data.

Conventional state persistence is usually the better choice when:

- only the latest state matters;
- an audit log can be a separate, non-authoritative record;
- CRUD changes are more natural than durable business transitions;
- histories would contain sensitive data that cannot be retained safely;
- replay would call external systems or depend on nondeterministic state;
- the team cannot operate append-only storage, evolution, and repair;
- the expected value does not justify the additional code and operational
  surface.

Choosing current-state persistence is not a failure to adopt a more advanced
architecture. It is the pragmatic choice for most application data.

## Compare the persistence outcomes

The [runnable persistence-choice example](../adoption_example_test.go) performs
the same initial account decision in two ways. Conventional persistence owns a
single current-state value. The event-sourced aggregate records the stable
`account.opened` fact, applies it immediately, and exposes that fact as pending
history for persistence.

The example is intentionally small: identical current state does not make the
operational models equivalent. Choose event sourcing only when retaining and
evolving the ordered fact is itself valuable. Otherwise persist the current
state directly.

## Adopt one boundary at a time

Begin with one aggregate or bounded context whose event history has clear
business value. Keep commands, queries, HTTP handlers, queues, and framework
wiring application-owned. The package requires only:

1. an application aggregate with explicit event application;
2. an `EventStore`;
3. an `AggregateRepository`; and
4. an explicitly selected post-persistence dispatcher.

The rest is optional. CQRS, projections, snapshots, process managers, Kafka,
and an outbox should be introduced only when their specific problem exists.

Before the first production event, define:

- stable aggregate types, aggregate ID encoding, event names, and schema
  versions;
- payload and metadata size limits and sensitive-data policy;
- optimistic-concurrency behavior visible to callers;
- unknown, malformed, and incompatible history handling;
- backup, restore, replay, retention, deletion, and repair procedures;
- ownership of dispatch failures and duplicate delivery;
- the event and schema migration review process.

These persisted decisions are SemVer-sensitive and often outlive the Go code
that first wrote them.

## Run a suitability exercise

Model representative commands and events on paper before writing infrastructure.
For each event, ask:

- Is this a business fact or merely a field update?
- Can its name and meaning remain stable for years?
- Can it be applied deterministically without external calls?
- Can future code still interpret or explicitly upcast it?
- Does retaining it create privacy, security, or legal risk?
- How will an operator diagnose and repair a corrupt history?

Build a thin vertical slice with the in-memory store, scenario tests, and a
model-based live-versus-replay check. Then repeat the same store conformance
and operational exercises against PostgreSQL. Measure representative stream
lengths and contention rather than assuming snapshots or partitioning are
needed.

## Anti-patterns

Avoid these designs:

- **events as row-change logs**: `FieldChanged` events usually preserve storage
  mechanics rather than domain meaning;
- **reflection-derived event names**: Go package paths and type names are not
  stable persisted identities;
- **side effects during apply**: replay must not send mail, call APIs, publish
  messages, or read mutable external state;
- **one giant aggregate**: long hot streams serialize unrelated decisions and
  expand conflict scope;
- **events as mutable integration contracts**: publish an explicit translated
  integration envelope when external consumers need a different lifecycle;
- **upcasters that rewrite history**: transform only at the read boundary and
  preserve the stored record;
- **snapshots as truth**: snapshots are deletable derived acceleration data;
- **direct database-to-broker atomicity claims**: use a transactional outbox
  and still design for at-least-once delivery;
- **replay through live side effects**: replay must use explicit delivery mode,
  filters, and separately named operations;
- **deleting inconvenient events**: history repair requires a reviewed,
  auditable procedure, not ad hoc SQL.

## Exit criteria

Do not put the first aggregate into production until:

- live execution and replay produce equivalent aggregate state;
- optimistic conflicts and ambiguous commits have application behavior;
- hostile stored input fails without panic or partial construction;
- PostgreSQL migration, backup, restore, and contention evidence exists;
- event evolution and replay are tested on a production-shaped corpus;
- operators can identify checkpoints, poison events, and partial delivery;
- the team accepts that exactly-once end-to-end delivery is not guaranteed.

If the exercise does not demonstrate enough value, retain conventional state
persistence. The package is designed so trying event sourcing in one bounded
context does not require an application-wide rewrite.
