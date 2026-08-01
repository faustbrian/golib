# Architecture

The module is a terminal AWS adapter. Its public `Client` contract contains
only `CreateSecret` and `PutSecretValue`; callers compose the official AWS SDK
v2 client through the default credential chain. Clients that also implement
version-pinned `GetSecretValue` enable exact verification after an ambiguous
existing-version response without widening the required write contract.

Application code owns deterministic secret names, version identifiers, and
version ordering. The adapter maps those values to Secrets Manager without a
local registry or shared mutable state. It creates a container when absent and
adds an immutable version when present.

Each historical version uses a unique staging label. The application persists
and reads the returned explicit version identifier, so older migration work
cannot replace a newer version by moving `AWSCURRENT`.

The AWS SDK owns transport retries and cancellation. The adapter does not start
goroutines, retain request payloads, or cache clients. It performs one explicit
version-pinned read only when a provider reports that the requested immutable
version already exists.
