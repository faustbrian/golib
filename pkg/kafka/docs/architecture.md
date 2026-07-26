# Architecture

The module owns client-side Kafka policy, not domain schemas or cluster
infrastructure.

- `Producer` validates bounded records and waits for broker delivery.
- `Consumer` uses a group, disables automatic commits, blocks rebalancing while
  a bounded poll is processed, and commits after durable handlers succeed.
- `Transaction` serializes a configured transactional producer and prevents a
  retained callback capability from publishing after completion.
- `ReplayReader` directly assigns explicit partition ranges and never commits.
- `Inspector` exposes read-only metadata and lag.

franz-go remains an implementation detail except for the caller-supplied SASL
mechanism. Topic lifecycle, ACLs, replication, ISR, retention, quotas, and
destructive group operations belong to infrastructure automation and audited
operator procedures.
