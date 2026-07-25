# Performance

Benchmarks live beside the hot paths and run with allocation reporting:

```sh
./scripts/check-benchmarks.sh
```

Static Basic and API-key validation hash incoming values once and compare fixed
digests across every entry. Work grows linearly with the configured set by
design to avoid early-match timing differences. Keep static sets small; use a
callback store for large fleets.

HTTP extraction is body-independent and allocates only typed credential data.
Opaque callback cost is dominated by the callback. JWT and OIDC costs are
dominated by compact decoding, JSON parsing, upstream claim validation, and
signature verification. Cached keys avoid a network call; unknown OIDC keys
and explicit JWT refresh perform synchronous network work bounded by context.

Treat benchmark values as machine-specific. Review relative changes, allocation
growth, and tail latency under realistic key counts and claim sizes. Never
weaken constant-time comparison, bounds, or signature validation for a
microbenchmark improvement.
