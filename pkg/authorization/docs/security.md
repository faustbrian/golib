# Security model

The library authorizes an already authenticated subject against trusted policy
and application-mapped facts. It does not authenticate identities, secure
transport, authorize policy administrators, or decide which database fields
are trustworthy.

## Security invariants

- Only an explicit final `Allow` grants access.
- Invalid, missing, canceled, errored, panicking, or over-budget evaluation
  fails closed.
- Tenant policy is isolated unless global inheritance is explicitly enabled.
- One decision or batch observes one complete immutable revision.
- Stored and active revisions move only forward.
- Synchronizer authorization fails closed outside the repository-verification
  freshness window.
- Cache and invalidation data cannot directly activate policy.
- Work, batch, trace, match, set, depth, and document cardinality are bounded.
- Default errors, traces, logs, metrics, and spans avoid request and attribute
  values.

## Trust boundaries

Authentication adapters must verify subject identity before mapping it.
Resource and ownership attributes must come from authoritative application
state. Tenant, roles, scopes, network identity, and environment facts supplied
by a client are untrusted until independently verified.

Policy publishers are privileged. Restrict manifest update credentials,
separate approval from ordinary application roles, record optimistic revision
conflicts, and retain an immutable administrative audit trail.

PostgreSQL is authoritative. Valkey and `cache` contain hints or advisory
copies and may be stale, missing, duplicated, delayed, or attacker-influenced.
Always verify and compile repository state before activation.

Sensitive revocations require protected operations to use
`policy.Synchronizer` as their authorizer. Direct calls to the underlying
`Engine` preserve snapshot coherence but do not enforce repository freshness.

## Sensitive data

Use opaque stable IDs for subjects, resources, and policies. Never use bearer
tokens, API-key secrets, credentials, personal attributes, or complete policy
documents as IDs, reason codes, metric labels, or span attributes.

`PolicyEvaluationError.Error` omits the underlying evaluator error. An
application can inspect the wrapped error programmatically, but must apply its
own redaction before logging it. Panic values are discarded entirely.

See the [threat model](threat-model.md) for attacker capabilities and residual
responsibilities, and [SECURITY.md](../SECURITY.md) for private reporting.
