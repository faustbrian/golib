# Administrative access

Cross-tenant maintenance, migrations, imports, support work, and analytics
begin with an application-authorized `SystemCapability`. Its reason contains
an actor, purpose, and optional ticket/reference. Construction records intent;
it never grants permission or tenant membership.

`IterateTenants` requires a mandatory audit callback, bounded page and total
limits, cancellation, and a resume token. Each tenant context is derived from
the original unscoped context. A callback must not retain that context or any
tenant-scoped database/cache session after it returns.

Support impersonation should remain tenant scope with separate audit metadata
when work is performed as one tenant. Use system scope only for genuinely
cross-tenant work. Keep administrative database roles, credentials, endpoints,
and telemetry separate from ordinary application paths.
