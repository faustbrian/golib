# Glossary

**Aggregate**  
A consistency boundary that owns domain invariants and changes state by
recording events.

**Aggregate root ID**  
The application-defined stable identity of one aggregate stream. It need not be
a UUID.

**Aggregate type**  
The stable application-defined category paired with an aggregate root ID to
form a stream identity.

**Causation ID**  
The message ID of the immediate input that caused another message.

**Checkpoint**  
A durable global position through which a projection has successfully
processed or explicitly skipped messages.

**Commit ambiguity**  
A failure where the caller cannot prove whether a transaction committed. It
requires reconciliation, not blind retry.

**Correlation ID**  
An identifier used to associate messages in one business interaction without
changing their aggregate identity.

**Delivery**  
A persisted event message plus an explicit live or replay mode supplied to a
consumer.

**Dispatcher**  
The replaceable responsibility that delivers persisted messages to consumers.

**Event**  
An immutable statement of a domain transition that has already occurred.

**Event name**  
The explicit stable persisted identity of an event. It is independent of Go
package paths and type names.

**Event schema version**  
The positive version describing the persisted payload contract for an event
name.

**Event store**  
The replaceable responsibility that atomically appends and reads immutable
messages with expected-version semantics.

**Expected version**  
The concurrency precondition for an append: new, existing, exact, or explicit
any-version mode.

**Global position**  
An optional one-based store-wide ordering position assigned by capable stores.

**Live delivery**  
Delivery of a message after its normal durable append path.

**Message**  
The immutable persisted envelope containing event data, stream identity,
versions, time, metadata, and causal identifiers.

**Message ID**  
The stable unique identity used for duplicate detection and reconciliation.

**Optimistic concurrency**  
Rejecting an append when the actual stream version no longer matches its
declared precondition.

**Outbox**  
Derived durable publication work committed atomically with application or event
data and relayed at least once after commit.

**Pending message**  
A validated event envelope before the store assigns stream and optional global
positions.

**Process manager**  
A consumer that reacts to events by planning explicit commands or messages.
Effects and persistence remain application-owned.

**Projection**  
Derived state built by consuming ordered messages, commonly with a durable
checkpoint. Event sourcing does not require projections or CQRS.

**Replay delivery**  
Explicit historical delivery used for rebuild or analysis. Safe compositions
isolate external side effects and process managers by default.

**Snapshot**  
Replaceable derived aggregate state at a known aggregate version. Event history
remains authoritative.

**Stream**  
The ordered event history for one aggregate type and aggregate root ID.

**Stream version**  
The one-based position of a message within its aggregate stream.

**Upcaster**  
A deterministic read-boundary transformation from stored event identity,
payload, and metadata to newer logical events without rewriting history.
