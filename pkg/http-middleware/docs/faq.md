# FAQ

## Is CORS authentication or CSRF protection?

No. It controls browser response sharing. Authenticate, authorize, and defend
state-changing browser requests separately.

## Why is my forwarded header ignored?

The direct peer is not in `Policy.Trusted`, the selected syntax is different,
or one field is malformed/oversized. Direct connection data wins by default.

## Why did streaming stop working?

Buffered compression and buffered handler timeout intentionally expose no
streaming interfaces. Remove those layers or use a non-streaming endpoint.

## Does a deadline stop my handler?

No. It cancels the request context. Code must observe cancellation. A buffered
timeout can return early, but context-ignoring code may continue until it exits.
`MaxConcurrent` bounds those retained executions; excess requests fail before
another handler goroutine starts.

## Can request IDs be trusted for access decisions?

Never. They are correlation metadata. Authentication and authorization own
identity and access evidence.

## Why are duplicate names rejected?

Duplicate transport policy is often an ownership bug. Set `AllowDuplicate`
only for a middleware whose semantics explicitly support repetition.
