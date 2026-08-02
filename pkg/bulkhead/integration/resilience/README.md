# Bulkhead resilience composition

This non-releasable integration module proves the application-owned resilience
ordering through the public bulkhead, retry, and circuit-breaker contracts.
Local bulkhead admission failure remains a permanent retry outcome and occurs
before circuit-breaker admission, so it neither amplifies attempts nor records
a downstream failure.

Run from the repository workspace with:

```sh
go test ./pkg/bulkhead/integration/resilience/...
```
