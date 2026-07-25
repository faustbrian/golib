# Troubleshooting And FAQ

- **Duplicate publication:** expected after ambiguous publisher acceptance,
  delivered-update failure, lease expiry, or replay. Verify consumer dedupe.
- **Later record not claimed:** an earlier scheduled non-terminal record can
  block its ordering key or topic.
- **`ErrLeaseLost`:** the token expired, was reclaimed, or already transitioned.
  Inspect state; do not force the update.
- **Healthy readiness but growing backlog:** readiness proves round trips, not
  capacity. Check latency, quotas, retries, ordering, connections, and plans.
- **`ErrNotWritable`:** readiness reached a read-only session or server in
  recovery. Route writers, claims, and readiness through the same writable
  primary endpoint.
- **`ErrReplayUnauthorized`:** replay has no configured authorizer or the hook
  denied/panicked. Do not bypass it; inspect restricted application policy
  diagnostics and retry only after authorization succeeds.
- **`ErrClaimBatchOverflow`:** a custom Store returned more claims than the
  requested relay batch. Fix the Store contract; unprocessed leases recover by
  expiry.
- **`ErrInvalidRetryDelay`:** a direct Store caller requested a negative delay
  or more than one minute. Pass a duration within the documented bound; do not
  convert it to a host timestamp.
- **Pool instead of transaction:** not supported. Application and outbox writes
  must use the exact same caller-owned `pgx.Tx`.
- **Idempotency key:** prevents duplicate non-empty inserts only; it does not
  cover broker acceptance or consumer effects.
