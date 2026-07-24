# Logical Operation Identity and Idempotency

Logical operation identity and provider idempotency are related but different.
Every `Client.Do` and `Client.DoWithMiddleware` receives an operation ID. An
idempotency key exists only when an endpoint explicitly registers idempotency
middleware. Neither value implies that an arbitrary request is safe to retry.

## Logical operation identity

The client generates a URL-safe 128-bit operation ID before all other request
policy. It is stored in the request context and remains unchanged across every
redirect or retry attempt within that call. A later, distinct call receives a
new ID, including repeated items submitted by a future request pool.

Middleware and transports can inspect it without replacing standard requests:

```go
identity, ok := httpclient.OperationIdentityFromContext(request.Context())
```

`OperationIdentity.Provenance` is `IdentityGenerated` by default. Callers that
already have a validated correlation identity can supply it explicitly:

```go
ctx, err := httpclient.WithOperationIdentity(ctx, operationID)
```

Caller IDs are limited to 128 ASCII characters from letters, digits, `-`, `.`,
`_`, and `~`. Generated identifiers must claim at least 96 bits of entropy. A
custom concurrency-safe generator can be configured through
`Config.OperationIdentityGenerator`; the default uses `crypto/rand`.

Operation identity assignment failures use `OperationIdentityError`, which
unwraps the generator or validation cause without rendering it.

## Endpoint idempotency policy

Providers disagree about which endpoints support idempotency, which header
they use, and what duplicate requests mean. Register policy only for an
endpoint whose contract is known:

```go
middleware, err := httpclient.NewIdempotencyMiddleware(
	httpclient.IdempotencyOptions{
		Name:  "create-widget-idempotency",
		Layer: httpclient.MiddlewareEndpoint,
	},
)
```

The constructor returns operation and attempt middleware. Append both values
to `Config.Middleware` or `Client.DoWithMiddleware`.

The operation stage selects exactly one key. The attempt stage applies that key
to `Idempotency-Key` only while the attempt policy says the request still
represents the same operation. Retry middleware can call `Next` repeatedly;
the key is selected outside that loop and reused.

## Caller and generated keys

A caller can supply one key through the configured header or through context:

```go
ctx, err := httpclient.WithIdempotencyKey(ctx, key)
```

Supplying both sources or multiple header values is an error, even if their
text matches, because provenance would be ambiguous. Provenance is one of:

- `IdempotencyGenerated`;
- `IdempotencyCallerHeader`; or
- `IdempotencyCallerContext`.

Retrieve resolved metadata with:

```go
key, ok := httpclient.IdempotencyKeyFromContext(request.Context())
```

`IdempotencyKey.String` and `GoString` redact the value. Code must access
`key.Value` deliberately when integrating with request policy. Do not use the
raw value as a log field, span attribute, or metric label.

`IdempotencyGenerateIfMissing` is the zero-value mode. The default generator
uses 128 bits from `crypto/rand` and unpadded URL-safe base64. Configure
`IdempotencyRequireCaller` when the provider or caller owns key allocation.

The default maximum is 255 bytes and can be reduced per endpoint. The hard
upper bound is 1024 bytes. Values must be non-empty printable ASCII without
spaces or control bytes. Generated candidates must meet the configured minimum
entropy, which defaults to 128 bits and cannot be lower than 96.

## Redirect and retry identity

Before entering `net/http`, the operation middleware removes the key header
from the redirect source request. Every physical attempt starts clean, and the
attempt middleware decides whether to reapply it.

The default `IdempotencyAttemptPolicy` preserves a key only when:

- the attempt method equals the original method; and
- the attempt has the same scheme, host, and effective port.

Consequences include:

- a same-origin `307` or `308` retains the key and replayed method;
- a `301`, `302`, or `303` that changes `POST` to `GET` drops the key;
- a cross-origin redirect drops the key even when it preserves the method; and
- an operation retry with the original method and origin retains the key.

A provider-specific cross-origin flow must opt in through
`IdempotencyAttemptPolicyFunc`. The callback receives independent bodyless
request snapshots. It should compare only stable provider trust and operation
semantics, never infer safety from the presence of a key.

## Custom headers and generators

Providers can replace the header and generation policy:

```go
middleware, err := httpclient.NewIdempotencyMiddleware(
	httpclient.IdempotencyOptions{
		Name:               "vendor-create-idempotency",
		Layer:              httpclient.MiddlewareEndpoint,
		Header:             "X-Vendor-Idempotency",
		MaximumLength:      64,
		MinimumEntropyBits: 128,
		Generator:          generator,
	},
)
```

`IdentifierGenerator` returns a value and explicit entropy claim and must be
safe for concurrent use. Generator and policy failures use `IdempotencyError`.
It unwraps the cause for programmatic checks while never rendering a key or
generator error.

## Retry safety remains separate

An idempotency key is provider input, not proof. A future retry policy must
still require all of the following:

- the endpoint contract says the operation and key semantics are retry-safe;
- the method and status or transport error are eligible;
- the body can be replayed through `GetBody` without aliasing state;
- the caller context and deadline still permit another attempt; and
- the bounded attempt and elapsed-time budgets remain available.

Direct calls through `Client.HTTPClient().Do` bypass operation identity and
idempotency middleware. Use `Client.Do` for the complete policy lifecycle.
