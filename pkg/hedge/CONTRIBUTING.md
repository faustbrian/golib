# Contributing

Use Go 1.26.5. Preserve the distinction between concurrent hedges and
sequential retries. Every behavior change needs a failing behavioral test,
exact coverage, race evidence, and a changelog entry. Run `make check` before
submission; public API changes also require `make api-update`.
