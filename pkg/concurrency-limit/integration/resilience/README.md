# Adaptive limiter resilience composition

This non-releasable integration module proves application-owned composition
through the public adaptive limiter, retry, and hedge contracts. Local
admission rejection remains a permanent retry outcome, and every hedge attempt
must acquire its own limiter permit before invoking downstream work.

Run from the repository workspace with:

```sh
go test ./pkg/concurrency-limit/integration/resilience/...
```
