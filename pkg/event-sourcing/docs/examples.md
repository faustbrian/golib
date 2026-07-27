# Runnable workflow examples

Every primary adoption and deployment workflow has a source-controlled
executable artifact. Go examples run as part of ordinary module tests.
Integration examples are ordinary `testing` tests behind the `integration`
build tag because they start real PostgreSQL, Kafka, or Valkey containers.

Run commands from the repository root. Integration commands require a
Docker-compatible container runtime.

| Workflow | Executable artifact | Command |
| --- | --- | --- |
| Decide between conventional persistence and event sourcing | [`Example_adoptionPersistenceChoice`](../adoption_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_adoptionPersistenceChoice$' .)` |
| Create, persist, dispatch, and reconstitute an aggregate | [`Example_fiveMinuteQuickstart`](../quickstart_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_fiveMinuteQuickstart$' .)` |
| Evolve and decode a stored event schema | [`Example_eventSchemaEvolution`](../evolution_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_eventSchemaEvolution$' .)` |
| Dispatch synchronously to an explicit consumer | [`Example_synchronousDispatch`](../dispatch_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_synchronousDispatch$' .)` |
| Test an aggregate scenario | [`Example_scenarioTesting`](../scenario_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_scenarioTesting$' .)` |
| Refresh and restore a snapshot | [`Example_snapshotRestoration`](../snapshot_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_snapshotRestoration$' .)` |
| Replay a projection with a durable checkpoint | [`Example_replayProjection`](../replay_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_replayProjection$' .)` |
| Plan process-manager commands without executing them | [`Example_processManagerPlanning`](../process_manager_example_test.go) | `(cd pkg/event-sourcing && go test -run '^Example_processManagerPlanning$' .)` |
| Verify a custom store and dispatcher | [`eventtest` conformance examples](../eventtest) | `(cd pkg/event-sourcing && go test ./eventtest -run '^(TestEventStoreConformanceAcceptsMemoryStore|TestSynchronousDispatcherConformanceAcceptsCoreDispatcher)$')` |
| Persist and globally read PostgreSQL event streams | [`TestPostgreSQLEventStoreConformance`](../postgres/integration_test.go) and the global-reader and caller-owned-transaction scenarios in the same suite | `(cd pkg/event-sourcing/postgres && make integration)` |
| Publish and settle Kafka event deliveries | [`TestEventDeliveriesRoundTripThroughKafka`](../adapters/gokafka/integration_test.go) | `(cd pkg/event-sourcing/adapters/gokafka && make integration)` |
| Publish and settle a compatible durable queue | [`TestValkeyStreamRetainsAndSettlesCompleteDelivery`](../adapters/goqueue/durable_integration_test.go) | `(cd pkg/event-sourcing/adapters/goqueue && make integration)` |
| Commit events and outbox envelopes atomically | [`TestStagerCommitsAndRollsBackEventsWithOutboxEnvelopes`](../adapters/gooutbox/stager_integration_test.go) | `(cd pkg/event-sourcing/adapters/gooutbox && make integration)` |
| Relay committed outbox envelopes with durable retry | [`TestCommittedStoreRelaysWithDurableRetryAndReplayIsolation`](../adapters/gooutbox/stager_integration_test.go) | `(cd pkg/event-sourcing/adapters/gooutbox && make integration)` |
| Map an outbox envelope to a Kafka record | [`TestPublisherMapsEnvelopeToKafkaMessage`](../../outbox/adapters/gokafka/publisher_test.go) | `(cd pkg/outbox/adapters/gokafka && go test ./...)` |
| Instrument dispatch and consumption without exposing data | [`TestInstrumentationTracesAndMeasuresDispatchAndConsumption`](../adapters/gotelemetry/instrumentation_test.go) | `(cd pkg/event-sourcing/adapters/gotelemetry && go test ./...)` |

The real-service examples are correctness evidence, not deployment scripts.
Applications still own pool and client configuration, credentials, timeouts,
shutdown, consumer idempotency, schema rollout, and operational recovery.
The direct Kafka example is intentionally separate from the PostgreSQL-outbox
workflow: neither Kafka idempotence nor Kafka transactions make the database
append and broker publication atomic.
