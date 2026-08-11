# Packages

Use the [generated package catalog](package-catalog.md) for the authoritative
list of independently releasable modules, import paths, lifecycle, owned
dependencies, required services, and specifications. Use the
[engineering inventory](engineering-inventory.md) when you also need internal
tools, fixtures, examples, interoperability modules, and benchmark harnesses.

## Selection Workflow

1. Start with the application concern in [choosing packages](choosing-packages.md).
2. Confirm the package responsibility and deliberate non-goals in its README.
3. Review its compatibility, security, performance, and hardening documents
   where present.
4. Check required services and owned dependencies in the generated catalog.
5. Follow [versioning](versioning.md) before adding an unreleased module or
   combining independently released modules.

## Package Families

| Need | Typical starting modules |
| --- | --- |
| Service runtime | `service`, `router`, `http-middleware`, `config`, `log`, `telemetry` |
| API protocols | `jsonrpc`, `jsonapi`, `openapi`, `openrpc`, `api-query`, `webhook` |
| Identity and access | `authentication`, `authorization`, `password`, `capability`, `tenancy` |
| Durable data | `postgres`, `migrations`, `outbox`, `idempotency`, `settings` |
| Async execution | `queue`, `queue-control-plane`, `scheduler`, `workflow`, `sequencer` |
| Resilience | `retry`, `rate-limit`, `circuit-breaker`, `bulkhead`, `hedge`, `adaptive-throttle` |
| Integration | `http-client`, `filesystem`, `cache`, `schema-registry` |
| Data formats | `wire`, `tabular`, `json-schema`, `xsd`, `wsdl` |

These are starting points, not mandatory bundles. A direct standard-library or
backend client remains preferable when Golib adds no required contract or
policy. See [recommended stacks](recommended-stacks.md) for compositions.

Return to the [documentation index](index.md).
