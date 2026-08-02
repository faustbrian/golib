# Security and abuse resistance

Configuration rejects NaN, infinity, invalid gains, reversed bounds, excessive
sample or queue capacity, unbounded partition keys, and arithmetic step sizes
that could overflow. Runtime algorithm state must remain finite; invalid state
is ignored and counted. Counters saturate. Backward clock elapsed time becomes
zero. Observer and classifier panics are contained outside the state lock.

The queue has absolute count and time limits. Partition cardinality is fixed at
construction and priority cannot affect ordering. Snapshots and events retain
no results, errors, contexts, secrets, request bodies, or dynamic maps.

Callers remain responsible for authenticating partition identity, authorizing
priority, bounding work and context deadlines, preventing retry/hedge
amplification, and avoiding secret-bearing observer labels.

Threats not addressed include malicious in-process code, compromised observers,
distributed coordination, downstream identity, and workload authorization.
