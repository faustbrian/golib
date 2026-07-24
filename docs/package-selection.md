# Package Selection

## API Protocols

Use JSON-RPC for command-oriented internal service calls and JSON:API for
resource-oriented external APIs. OpenRPC documents JSON-RPC contracts; OpenAPI
documents HTTP APIs. JSON Schema validates generic JSON instances and is also
used by OpenAPI/OpenRPC tooling.

These packages are composable but not interchangeable. Do not expose JSON-RPC
where clients require HTTP resource semantics, and do not force JSON:API onto
small internal commands merely for consistency.

## Service Runtime

`service` owns process lifecycle and server composition, `router` owns route
matching, and `http-middleware` owns transport middleware. `authentication`
establishes identity; `authorization` decides access. Business roles and
policies belong to applications, not authentication middleware.

## Durability

`queue` executes durable asynchronous jobs. `outbox` atomically records events
with database writes. `idempotency` prevents duplicate effects. `lease` and
`scheduler` coordinate distributed execution. These solve distinct failure
modes and should not be substituted for one another.

## Data And Formats

`wire` handles structured serialization, `tabular` handles row/column formats,
`xsd` handles XML Schema, and `wsdl` handles SOAP service descriptions.
`filesystem` supplies storage transports but does not own document semantics.

Consult each module README for exact support, deliberate tradeoffs, and
security/resource limits.
