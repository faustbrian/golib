# Troubleshooting

- **Cursor rejected:** check expiry, tenant/query/sort/fingerprint binding,
  traversal budgets, PIT expiry, and key rotation.
- **Unknown bulk outcome:** do not convert it to failure; reconcile the stable
  ID/version, then replay only if safe.
- **Partial shards:** inspect diagnostics and cluster health; do not present the
  response as complete unless application policy allows it.
- **Discovery rejected:** verify the explicit DNS suffix/CIDR allowlist, node
  roles, scheme, port, and maximum-node bound.
- **Reindex does not complete:** persist the task cursor, inspect bounded task
  status, version conflicts, failures, and count verification before cutover.
- **Overload:** stop aggregate retries, reduce concurrency/bulk size, apply
  jittered idempotency-aware retry budgets, and preserve per-item outcomes.
- **Circuit open:** inspect the downstream transport/429/503 cause, leave
  admission bounded, and wait for the configured half-open probe; recreating
  clients only resets process-local evidence and can amplify load.
- **AWS authentication rejected:** confirm region, signing service, workload
  role policy, credential-provider refresh, clock synchronization, and that no
  proxy or redirect changed the signed authority. Errors are intentionally
  redacted.
- **TLS failure:** verify the endpoint name, trust roots, minimum TLS 1.2, and
  certificate rotation. Peer verification cannot be disabled.
- **Mapping rejection:** stop retrying, compare the canonical mapping and
  fingerprint, correct the projection or create a new generation, then replay.
- **Alias confusion:** resolve and authorize tenant/logical index again. Never
  reuse a cursor across an alias generation or manually rewrite its target.
- **Capacity report rejected:** ensure all selected shard/node stats responded
  successfully and the deployment did not exceed configured topology bounds.
