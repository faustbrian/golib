# Compatibility

The module currently requires Go 1.26.5 because its pinned `idempotency`
dependency requires that toolchain. Cron behavior is pinned to
`robfig/cron/v3`. PostgreSQL support targets currently maintained releases 14
through 18 and is enforced by the CI adapter matrix. Valkey support requires
major version 9 or later.

Public API compatibility is checked before release. Pre-v1 releases may make
breaking changes, which are recorded in `CHANGELOG.md`. Time-zone behavior also
depends on the zone database shipped with the Go runtime or host.
