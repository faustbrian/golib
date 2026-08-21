# OA OpenSearch Operations Evidence

Observed at `2026-08-13T12:09:43Z` on Darwin/arm64 with Go 1.26.5. The focused
operations contract passed with task-owned disposable Go caches.

The campaign validated these pinned repository artifacts:

- operations test: `aab170cddf82b82abeaf08bbf0879e8285a3c8d5e5e9ec17a2ee5f7348a103fc`;
- dashboard: `7b226a6a4e1c7f9f0bc2b5aa0a966e8ca2198aa295d6ff7cd399ecd7d9d1e87b`;
- alerts: `6bf48e632567e3823af4484e2cb28f82f1e4913917452d314e8fc1f29861ad3a`;
- incident drills: `10ed8c9f03abd2efc1f7316e273c89d7702c1f9e01dc4997fbaf145b4fae3738`;
  and
- runbook: `db646bcdfcf4ef6f590d5a7ce469fee22b831c4842977121fb6fceb4275e5a30`.

The executable contract proved that dashboard panels cover traffic, admission,
cluster health, capacity, data safety, and reconciliation signals; forbid
tenant, index, query, document, node, task, cursor, endpoint, and credential
labels; and use bounded grouping dimensions. Every alert has a severity,
duration, condition, and matching runbook procedure. Every declared incident
drill has a matching runbook procedure, including overload, cluster loss,
unknown write outcomes, PIT expiry, migration rollback and ambiguity,
generation cleanup, drift repair, full rebuild, and resource exhaustion.

The real OpenSearch version, rolling-recovery, and security campaigns recorded
separately exercise many of these underlying failure and recovery behaviors.
This artifact check does not prove dashboard installation, alert delivery,
production SLOs, or a human operator drill. `OA-OBSERVABILITY-OPERATIONS`
therefore remains pending.
