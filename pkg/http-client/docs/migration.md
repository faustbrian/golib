# Migration Guide

## Adopting the module

1. Keep vendor DTOs and endpoint methods in the existing vendor package.
2. Replace ad hoc transports with one shared `httpclient.Client` and preserve a
   close path owned by that wrapper.
3. Move credentials into attempt-scoped authentication middleware.
   Credential origins now require HTTPS by default. Use `AllowInsecure` only
   for local test servers and migrate production HTTP endpoints before adding
   authentication.
4. Define endpoint retry and idempotency policy explicitly; remove nested retry
   loops from generated clients or outer application wrappers.
5. Classify status before bounded success decoding and make response ownership
   visible on every exit.
6. Add scope dimensions before enabling cache, cookies, OAuth refresh,
   coalescing, limiters, or breakers in multi-tenant code.
7. Prove the integration with strict scripted fixtures and `httptest.Server`.

## Upgrading releases

Read every changelog entry between versions, then review the compatibility
surfaces listed in `compatibility.md`. Re-run contract fixtures, redirect and
credential tests, retry/idempotency tests, race tests, and downstream compile
tests. Do not copy profile defaults into application code; inspect resolved
values and provenance instead.

Persisted fixture schemas never migrate implicitly. Register an explicit
`FixtureMigrator` for each understood source version, sanitize its output, and
rewrite it using the current recorder format. Expiry remains enforced after
migration.
