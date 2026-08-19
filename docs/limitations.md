# Limitations And Intentional Boundaries

This register surfaces ecosystem-wide constraints that can change adoption or
architecture. Package-specific limitations remain in package documentation.

| Scope | Classification | Impact and response |
| --- | --- | --- |
| All modules | Unreleased | No public module versions exist yet. Use the workspace for development; wait for dependency-ordered `v1.0.0` releases before external adoption. |
| Release readiness | Temporary limitation | Operational assurance is `not ready`; nine scenarios and five residual risks remain. Do not treat package gates as deployment approval. |
| Nil analysis | Intentional policy | NilAway is advisory because unresolved diagnostics can be false positives. Mandatory compile, test, race, lint, coverage, mutation, and security gates remain fail-closed. |
| Service construction | Intentional boundary | There is no service container, controller injection, model binding, session, CSRF, view, or templating framework. Applications construct dependencies and transport mapping explicitly. |
| Timeout and fallback | Intentional boundary | Caller deadlines use `context`; fallback is application policy. Golib does not provide generic wrappers that can misstate cancellation or business safety. |
| Process-local resilience | Operational constraint | In-memory limits, breakers, caches, permits, throttles, and budgets multiply or reset with replicas. Cluster-wide guarantees require an explicit distributed backend. |
| Durable effects | Protocol constraint | Queues, outboxes, and workflows do not make arbitrary external side effects exactly once. Preserve unknown outcomes, idempotency, and reconciliation. |
| Platform evidence | Temporary limitation | Local Linux amd64/arm64 container proof exists; native managed-platform, production network, IAM, and Graviton evidence remains pending. |
| Secrets and telemetry | Deployment boundary | Infisical and Better Stack are deployment integrations, not mandatory core dependencies. Live rotation and export require provider-specific operational proof. |
| Compatibility | Pre-release | API baselines detect drift, but compatibility promises begin with each module's first public `v1.0.0` release. |

Security and operational risks are tracked in the
[residual-risk register](security/residual-risks.md) and
[operational assurance](operational-assurance.md). Package selection tradeoffs
are in [comparisons](comparisons/index.md).

Return to the [documentation index](index.md).
