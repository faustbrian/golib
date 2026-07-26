# Architecture

The module owns client-side Kafka policy, not domain schemas or cluster
infrastructure.

- `Producer` validates bounded records and waits for broker delivery.
- `Consumer` uses a group, disables automatic commits, blocks rebalancing while
  a bounded poll is processed, runs a fixed-size worker set across independent
  partitions while keeping each partition sequential, and commits only each
  partition's contiguous durable success prefix.
- `FailureHandler` composes bounded per-record retry and terminal
  retry-topic, dead-letter, or delegated decisions without owning group
  offsets. Non-transactional target publication completes before the normal
  consumer submits its separate source commit.
- `Transaction` serializes a configured transactional producer and prevents a
  retained callback capability from publishing after completion.
- `TransactionProcessor` owns one read-committed group member and transactional
  producer; it commits one complete bounded source poll and its Kafka outputs
  together or aborts both.
- `ReplayReader` directly assigns explicit partition ranges and never commits.
- `Inspector` exposes read-only metadata and lag.

franz-go remains an implementation detail. The root module exposes owned TLS,
mTLS, PLAIN, SCRAM, and OAUTHBEARER policy contracts; optional vendor
authentication belongs in independently versioned adapters. Topic lifecycle,
ACLs, replication, ISR, retention, quotas, and destructive group operations
belong to infrastructure automation and audited operator procedures.
