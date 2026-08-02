# Extension guidance

Add an adapter only when its interface contract, ownership, cancellation,
partial results, and cleanup can be stated exactly.

1. Assign a bounded boundary and numeric operation mapping.
2. Inventory success, organic error, injected before/during/after error,
   partial-result, close, cancellation, and concurrent-use behavior.
3. Apply only fault kinds with valid semantics for that interface.
4. Close resources acquired before a selected after fault.
5. Hold no engine or adapter lock across caller callbacks, network IO, blocking
   channels, or unbounded work.
6. Bound retained bytes and never retain caller metadata or payloads.
7. Add adapter conformance, race, fuzz, cleanup, exact coverage, mutation, and
   benchmark evidence.

Database, cache, queue, Kafka, object-storage, and RPC clients usually bring
large dependency graphs and protocol-specific error contracts. Put those
adapters in independently releasable nested modules or downstream integrations.
The root production package must remain standard-library-only and must not pretend a
generic error is a valid protocol-specific transient failure.
