# JSON-RPC and non-HTTP transports

The core `Authenticator` accepts a context and typed credential, so JSON-RPC,
message consumers, jobs, and commands do not need HTTP adapters. Extract
transport data at the boundary, construct `BasicCredential`,
`BearerCredential`, or `APIKeyCredential`, call `Authenticate`, and carry the
returned principal in the operation context.

Do not map missing credentials to a zero principal. Either return a classified
absent failure or explicitly create `AnonymousResult` under a documented
optional policy. Map failures to transport codes without exposing wrapped
causes. `ExampleAuthenticator_backgroundConsumer` is a compiled non-HTTP
example.
