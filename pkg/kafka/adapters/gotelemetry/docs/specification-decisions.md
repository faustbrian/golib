# Kafka OpenTelemetry adapter specification decisions

This register records observable choices where the pinned OpenTelemetry
messaging conventions, OpenTelemetry Go APIs, and the root Kafka observation
contract permit more than one interpretation. Exact revisions and source
digests are pinned in [`specification/manifest.json`](../specification/manifest.json).

## KAFKA-OTEL-DEC-001: Span semantics follow observed lifecycle boundaries

- **Status, owner, and classification:** `resolved`; Kafka OpenTelemetry adapter maintainers; semantic-convention mapping.
- **Source and issue:** The [OpenTelemetry Kafka messaging span conventions](https://opentelemetry.io/docs/specs/semconv/messaging/kafka/) define producer, receive, and process operations, but a root Kafka completion observation does not prove that those narrower lifecycle boundaries occurred.
- **Credible interpretations and known peer behavior:** The adapter could infer standard span kinds from operation names, emit only custom spans, or emit standard spans only when the observation proves the required timing. Peer instrumentation often starts spans inside client hooks that expose more lifecycle detail than this adapter receives.
- **Selected behavior:** Producer completion, poll, and record observations use span kind `NONE` unless their root observation proves the complete standard operation. Skipped and pre-handler replay outcomes remain outside application-processing semantics.
- **Security, resource, compatibility, and wire consequences:** Security improves because payload and unproved identity are never inferred; resource work stays bounded to one synchronous observation; compatibility follows the pinned 1.44.0 semantics without overstating them; wire behavior is unchanged because observation cannot alter Kafka requests.
- **Executable evidence and public surface:** `TestEveryObservationHasAnExactPublicSpanAndDurationContract`, `TestCompletionObservationsDoNotClaimUnprovedMessagingOperations`, and `TestObserverKeepsUnprocessedReplayOutcomesOutsideMessagingSemantics` cover `Observer` and the documented span contract.
- **Upstream record and reconsideration:** The upstream record is OpenTelemetry semantic conventions 1.44.0 and the root Kafka observation API. Reconsider when either supplies a distinct, proven send, receive, or process lifecycle boundary.

## KAFKA-OTEL-DEC-002: Identity and propagation are explicit and bounded

- **Status, owner, and classification:** `resolved`; Kafka OpenTelemetry adapter maintainers; cardinality, privacy, and trace propagation.
- **Source and issue:** The [OpenTelemetry messaging attribute conventions](https://opentelemetry.io/docs/specs/semconv/messaging/messaging-spans/) allow destination and consumer identities while W3C trace context permits multiple headers; neither source defines this adapter's disclosure, duplicate-header, or Kafka-header bounds.
- **Credible interpretations and known peer behavior:** The adapter could emit all identities, hash them, silently choose one duplicate trace header, or require explicit allowlists and canonical single values. Peer integrations vary between convenience defaults and high-cardinality suppression.
- **Selected behavior:** Standard metrics use only convention-defined dimensions. Topic and consumer-group identity require bounded allowlists. Trace propagation owns canonical `traceparent` and `tracestate` fields, rejects ambiguity, clears stale fields, and preserves unrelated Kafka headers.
- **Security, resource, compatibility, and wire consequences:** Security prevents payload and uncontrolled-identity disclosure; resource usage is bounded by Kafka and adapter limits; compatibility preserves valid W3C context while rejecting ambiguous inputs; wire changes are limited to the two explicitly owned trace headers.
- **Executable evidence and public surface:** `TestStandardMetricsUseOnlyPinnedConventionDimensions`, `TestTraceContextPropagationRejectsInvalidOrAmbiguousFields`, and `TestTraceContextPropagationEnforcesContextsAndKafkaLimits` cover `AttributePolicy` and `TraceContextPropagation`.
- **Upstream record and reconsideration:** The upstream record is the pinned messaging conventions and OpenTelemetry W3C propagator behavior. Reconsider on a convention stability change or a versioned root Kafka header-ownership contract.

## KAFKA-OTEL-DEC-003: Providers remain caller-owned and failure-contained

- **Status, owner, and classification:** `resolved`; Kafka OpenTelemetry adapter maintainers; provider lifecycle and failure isolation.
- **Source and issue:** The [OpenTelemetry Go SDK](https://github.com/open-telemetry/opentelemetry-go) permits global or caller-owned providers and exporters that may block or panic. The specification does not assign adapter ownership of provider shutdown or exporter queues.
- **Credible interpretations and known peer behavior:** Construction could install global providers, own exporter shutdown, run callbacks asynchronously, or remain a synchronous caller-owned adapter. Peer frameworks frequently install global telemetry state during bootstrap.
- **Selected behavior:** Construction requires explicit complete providers, installs no globals, starts no goroutines, never shuts down caller state, contains provider panics, and relies on caller-configured SDK queues for exporter backpressure.
- **Security, resource, compatibility, and wire consequences:** Security keeps telemetry authority explicit and diagnostics redacted; resource ownership stays with the caller and SDK; compatibility works with no-op and configured providers without lifecycle surprises; Kafka wire behavior remains unchanged.
- **Executable evidence and public surface:** `TestAdapterProductionStartsNoGoroutines`, `TestObserverContainsHostileProviderPanicsAndClosesStartedSpan`, and `TestObserverDefinesNoOpSampledOutAndShutdownOutcomes` cover `Config`, `New`, and `Observer`.
- **Upstream record and reconsideration:** The upstream record is OpenTelemetry Go 1.44.0 API and SDK behavior. Reconsider if the Go API introduces a mandatory adapter-owned lifecycle contract.

## Unresolved decisions

None. Unsupported semantic-convention versions and unproved operation spans
are explicit non-claims rather than unresolved behavior.
