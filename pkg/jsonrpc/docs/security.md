# Security

## Trust Boundary

Treat request bodies, IDs, method names, parameters, batch members, and remote
responses as untrusted. Bound HTTP bodies and batch sizes before work is
scheduled.

## Application Responsibilities

- authenticate and authorize methods before business execution;
- apply request, batch, concurrency, and rate limits;
- set transport and handler deadlines;
- avoid returning internal error details in JSON-RPC error data;
- define safe retry behavior for each client method.

Notifications require observability because they cannot return protocol
errors. Transport security and credential handling are outside this package.

## Reporting

Follow [SECURITY.md](../SECURITY.md) for private vulnerability reporting and
[hardening.md](hardening.md) for the current audit evidence.
