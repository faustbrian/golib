# Administrative access

Cross-tenant maintenance, migrations, imports, support work, and analytics
begin with an application-authorized `SystemCapability`. Its reason contains
an actor, purpose, and optional ticket/reference. Construction records intent;
it never grants permission or tenant membership.

`IterateTenants` requires a mandatory audit callback, bounded page and total
limits, cancellation, and a resume token. Each tenant context is derived from
the original unscoped context. A callback must not retain that context or any
tenant-scoped database/cache session after it returns.

The pager must return stable snapshot cursors for the lifetime of an operation
and any resume. Repeated cursors are rejected, and one invocation inspects at
most `MaxTenants + 1` pages. A source whose pages can reorder between resume
attempts must encode its snapshot/version into the cursor; offset-only resume
cannot make a mutable source stable.

Support impersonation should remain tenant scope with separate audit metadata
when work is performed as one tenant. Use system scope only for genuinely
cross-tenant work. Keep administrative database roles, credentials, endpoints,
and telemetry separate from ordinary application paths.

Iteration is deliberately sequential: a resume token advances only after the
tenant callback returns successfully. A failed tenant is audited and invoked
again on resume, so imports and migrations must make the tenant operation
idempotent. Do not submit asynchronous fan-out work from the callback and treat
submission as completion. Applications that require fan-out must own durable
per-tenant completion, retry, and attribution records outside this helper.
