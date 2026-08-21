# Monorepo Migration

This page records Golib's repository-path migration. Application migration
guidance starts at the [migration index](migration/index.md), including
[Laravel and PHP](migration/laravel.md) and
[standalone Go](migration/standalone.md).

The libraries began as separate `go-*` repositories, were consolidated at the
repository root, and now live under `pkg/` to keep maintenance manageable. The
canonical paths are `github.com/faustbrian/golib/pkg/<library>`.

The former repositories never had real consumers, so no permanent compatibility
modules are maintained. During the initial unpublished migration, `go.work`
contains version-specific local replacements until the first dependency-ordered
`v1.0.0` tags are resolvable. Those replacements are temporary scaffolding and
must be removed before release readiness is claimed.

Package-local workflows were replaced by one root matrix. Package tools and
docs must target `.github/workflows/ci.yml` through the repository root.

Return to the [documentation index](index.md).
