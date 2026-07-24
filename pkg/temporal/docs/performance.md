# Performance

Values are small immutable structs. Normalized sets own copied slices.

| Operation | Complexity |
|---|---|
| period relation/membership | `O(1)` |
| set construction | `O(n log n)` |
| normalized set search | `O(log n)` |
| normalized union | `O((n+m) log(n+m))` |
| normalized intersection | `O(n+m)` |
| split/steps | `O(k)`, bounded by `Limits.Steps` |

Run `make bench` for relation, normalization, parser, and daily-set allocation
evidence. Benchmark output is an artifact, not a cross-machine hard threshold;
regression decisions compare like-for-like runners.
