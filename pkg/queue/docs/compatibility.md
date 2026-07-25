# Compatibility policy

The project uses semantic versioning.

- Before v1, minor releases may change APIs, but release notes identify every
  delivery, ack, retry, or shutdown semantic change.
- At v1, exported APIs and documented semantics are stable within the major
  version.
- Backend client and server version constraints are recorded in setup guides.
- Security fixes may require dependency minimum bumps in patch releases.
- Silent changes to acknowledgement or retry behavior are prohibited.
- Redis Streams remains source-compatible and backed by `go-redis/v9`.
  Valkey Streams is additive, backed by `valkey-go`, and does not alias or
  replace Redis.
- Security bounds are contracts: encoded messages default to 1 MiB, retry count
  is at most 100, and the in-memory queue defaults to 10,000 pending jobs.

Deprecated APIs remain for at least one minor release before removal. The
compatibility `NewWorker` constructors are retained through v1; production code
should use `NewWorkerE`.

`WithQueueSize(0)` retains the upstream unlimited ring behavior as an explicit
escape. It is not recommended for untrusted or bursty producers.

The tested Valkey server line is 9.x, currently 9.1.0. Only standalone
operation is supported. Valkey cluster or managed failover support requires a
separate SemVer-reviewed API and native integration evidence. Public Valkey
signatures are compile-frozen without native client types.
