# Security

- Do not expose arbitrary task registration or shell execution over HTTP.
- Authenticate and authorize inspection and recovery endpoints externally.
- Keep database and Valkey credentials in Kubernetes Secrets and rotate them.
- Use TLS and least-privilege database roles limited to the lease table.
- Configure Valkey with authentication, TLS where available, and `noeviction`.
- Bound parameters, metadata, catch-up, history, timeouts, and output.
- Treat task parameters and trace baggage as untrusted input in workers.
- Fence ownership-sensitive writes and make every durable job idempotent.

Definition and runtime limits are listed in the
[hardening budget matrix](hardening.md#resource-budgets). Applications must
treat conditions, hooks, observers, and in-process executors as trusted code.
The runner bounds their wait and concurrency, but Go cannot forcibly terminate
an arbitrary goroutine. See the [threat model](threat-model.md).

The core has no shell adapter. An application that adds one must avoid string
concatenation, use an explicit executable and argument vector, isolate the
process, bound output, and enforce cancellation.
