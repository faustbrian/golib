# Architecture

The module is a terminal AWS adapter. Its `Client` contract contains only
`CreateSecret` and `PutSecretValue`; callers compose the official AWS SDK v2
client through the default credential chain.

Application code owns deterministic secret names, version identifiers, and
version ordering. The adapter maps those values to Secrets Manager without a
local registry or shared mutable state. It creates a container when absent and
adds an immutable version when present.

Each historical version uses a unique staging label. The application persists
and reads the returned explicit version identifier, so older migration work
cannot replace a newer version by moving `AWSCURRENT`.

The AWS SDK owns transport retries and cancellation. The adapter does not start
goroutines, retain request payloads, cache clients, or perform hidden reads.
