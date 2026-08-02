# Contributing

Behavior changes require a failing observable test, exact statement coverage,
race and fuzz evidence, viable mutation kills, API compatibility review, and a
changelog entry. Algorithm changes must update the equation, deterministic
reference cases, workload simulations, tuning guidance, and benchmark report.

Run the module gates through the repository:

```sh
make check MODULES=pkg/concurrency-limit
```
