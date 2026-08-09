# Adoption patterns

Construct the audit record at the application boundary that already knows the
authenticated actor, stable subject, business action, outcome, and tenant.
The package does not authenticate, authorize, invent action names, discover
tenancy, or capture arbitrary state.

Choose one delivery policy explicitly:

1. Fail closed for actions that must not proceed without confirmed audit
   acceptance.
2. Fail open with an independently durable or operational alert when business
   availability outweighs immediate audit durability.
3. Durable buffer when an independently durable bounded sink can accept the
   redacted record during primary failure.

For atomic business and audit writes in PostgreSQL, construct a `postgres.TxWriter`
from the caller-owned transaction and call `Stage` before commit. An outbox may
carry the same contract through an application-owned coordinator; core does not
depend on an outbox implementation and does not call an event store an audit
trail.
