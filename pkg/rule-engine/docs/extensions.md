# Extensions

`Operator` exposes a name, exact kind signatures, and a context-aware evaluate
method. Registries belong to a compiler instance. Built-in names and duplicate
custom names cannot be replaced, and no process-global registry exists.

`PredicateFunc` is available for bounded logic that cannot be represented by
built-ins. It cannot be serialized, and its fact dependencies are opaque to
static cycle analysis. Avoid it for stored definitions.

`FactResolver` receives one explicit path at a time through
`EvaluateResolved`. Resolver failures are redacted. Implementations must avoid
side effects and return stable snapshots.

`PlanCache` stores immutable plans by canonical hash. `MemoryPlanCache` is a
bounded, locked LRU implementation. Cache failures stop compilation; entries
whose embedded hash differs from the requested hash are ignored.

Authorization and feature-flag adapters must preserve their own fail-closed
behavior. They must map `Indeterminate` to denial or disabled state, never to
success.

Exact optional domain adapters are isolated nested modules:

- [`adapters/gomath`](../adapters/gomath) provides exact decimal ordering.
- [`adapters/gotemporal`](../adapters/gotemporal) provides period relations
  and instant membership.
- [`adapters/gomeasurement`](../adapters/gomeasurement) provides exact
  compatible-unit quantity ordering.

Each module uses tagged canonical string values plus namespaced typed
operators. Their dependencies are not present in the core module graph.
