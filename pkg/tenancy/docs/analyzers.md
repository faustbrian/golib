# Static analysis

Runtime and type-system enforcement remain authoritative. Static analysis can
supplement them when an application declares exact sinks:

- require tenant-owned interfaces to accept `tenancy.Scope` or
  `tenancy.TenantID`; omitted arguments then fail at compile time;
- configure `analysis/context/no-background` to report replacement contexts
  outside composition roots;
- configure `analysis/api/forbidden-call` to prohibit direct transaction or
  connection constructors outside reviewed PostgreSQL adapters;
- configure `analysis/observability/high-cardinality-label` with
  `tenancy.TenantID` and exact telemetry sinks;
- use architecture rules to require cache and idempotency adapters rather than
  provider clients in domain packages.

This module's [`analysis.yml`](../analysis.yml) is an executable reference
policy. `make analyzers` builds the repository's pinned `golib-analysis`
command and proves that an ordinary consumer is blocked from constructing the
first-party cache, audit, queue, workflow, and telemetry providers directly,
replacing its context, or sending `TenantID` to an exact metric-label sink. A
separate reviewed adapter fixture proves that provider-construction exceptions
are package-exact rather than repository-wide.

There is no sound generic analyzer for “this string key should contain a
tenant” or “this operation should require a tenant” without application-owned
sink declarations. Naming heuristics would miss aliases and wrappers while
flagging legitimate system keys. These checks therefore rely on typed consumer
interfaces and exact configurable sinks. They have false negatives across
reflection, generated calls, dynamic SQL, and undeclared wrappers, and false
positives when a configured sink is intentionally system-wide. Reviewed
exceptions must remain explicit. Analyzers do not replace runtime assertions,
RLS, negative tests, or authorization.
