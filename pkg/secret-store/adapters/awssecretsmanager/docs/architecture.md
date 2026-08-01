# Architecture

The module is a terminal AWS adapter. Its public `Client` contract contains
only `CreateSecret` and `PutSecretValue`; callers compose the official AWS SDK
v2 client through the default credential chain. `GetVersion` separately
requires the client to implement the public `VersionReader` contract. This
keeps write-only test doubles and restricted clients compatible while making
version-pinned reads explicit.

Application code owns deterministic secret names, version identifiers, and
version ordering. The adapter maps those values to Secrets Manager without a
local registry or shared mutable state. It creates a container when absent and
adds an immutable version when present.

Each historical version uses a unique staging label. The application persists
and reads the returned explicit version identifier, so older migration work
cannot replace a newer version by moving `AWSCURRENT`.

The AWS SDK owns transport retries and cancellation. The adapter does not start
goroutines, retain request payloads, or cache clients. It performs an explicit
version-pinned read when confirming an existing immutable version or when the
caller requests that exact stored version.
