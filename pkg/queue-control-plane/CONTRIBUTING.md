# Contributing

Contributions must preserve the strict data-plane/control-plane boundary. Do
not add raw Redis or Valkey queue commands, delivery semantics, retry logic,
acknowledgement logic, pending recovery, or worker process supervision here.

Before submitting a change:

```sh
make check
make nilaway
FUZZ_TIME=2s make fuzz
make integration-postgres
make disaster-recovery-postgres
make benchmarks
make security
make mutation
make api-compatibility
docker buildx build --check .
```

Behavior changes should follow a red-green-refactor cycle and prove failure,
authorization, idempotency, and boundary behavior where applicable. The
repository requires meaningful 100% production Go statement coverage; merely
executing lines is insufficient.

Keep public inputs bounded, errors secret-safe, tenant scope explicit, and
administrative mutations authenticated, authorized, idempotent, attributed,
reasoned, audited, and durably completed. New backend capabilities must arrive
through stable `queue` contracts.

Run `make api-compatibility` before changing an exported contract. Review every
reported change first; when it is intentional, update the baseline with
`UPDATE_API_BASELINE=1 make api-compatibility` in the same commit.

Use conventional commits with an explanatory body. Keep every commit-message
line at 72 characters or fewer. Stage only the files intended for the commit.
