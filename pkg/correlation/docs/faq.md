# FAQ

## Is a correlation ID a trace ID?

No. A workflow can span traces, and one trace can contain work from multiple
application concepts. Telemetry links keep the relationship explicit.

## Is it an idempotency key?

No. Correlation proves neither request equality nor prior completion. Use
`idempotency` for those contracts.

## Why does every retry get a request ID?

Retries are distinct delivery attempts with separate timing, outcome, and
failure evidence. They share correlation and retain the message request as
their immediate cause.

## Why was my inbound ID replaced?

Preservation is disabled unless the immediate transport peer is explicitly
trusted. Malformed input is also replaced or rejected according to adapter
policy.
