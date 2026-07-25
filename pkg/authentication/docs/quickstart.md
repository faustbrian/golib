# Five-minute quickstart

1. Install the root module with `go get github.com/faustbrian/golib/pkg/authentication`.
2. Choose exactly the credential sources the endpoint accepts.
3. Construct the matching authenticator.
4. Wrap the handler with `authhttp.NewMiddleware`.
5. Read the immutable principal from the request context.
6. Pass that principal to application authorization; do not infer permission
   from successful authentication alone.

For an opaque service token, combine `authhttp.BearerAuthorization`,
`bearer.ValidatorFunc`, and `authhttp.NewMiddleware`. The compiled implementation
is `ExampleNewMiddleware` in `authhttp/example_test.go`.

Always return `FailureRejected` for a well-formed credential that is not
accepted, `FailureInvalid` for malformed protocol data, and
`FailureUnavailable` for a dependency outage. Optional routes may allow only
`FailureAbsent`; invalid or rejected credentials still fail closed.

Before production, configure HTTPS, token or key rotation, bounded callback
latency, fixed diagnostic messages, and either `authlog` or `authotel`.
