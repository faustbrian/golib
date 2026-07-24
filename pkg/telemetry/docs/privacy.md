# Privacy

Telemetry is an external data flow. Treat every exported attribute, event, and
resource field as durable and accessible outside the application process.

## Default exclusions

The supplied instrumentation does not record:

- HTTP raw paths, queries, hosts, headers, client addresses, bodies, or
  arbitrary methods
- SQL text, query arguments, database messages, details, or hints
- cache keys, values, encoded payloads, or loader errors
- queue messages, handler errors, or panic values
- arbitrary baggage, resource overrides, or uncontrolled operation names

HTTP methods collapse to `_OTHER`; PostgreSQL operations require a construction
time allow-list; cache/queue labels are finite enums. SQLSTATE may be recorded
because it is a bounded protocol code and excludes query values.

## Data classification

Review each proposed field against:

1. Is it a secret, credential, personal datum, payload, or customer content?
2. Can an unauthenticated caller control it?
3. Is its value set finite and documented?
4. Does it need to be a metric attribute, or is a sampled span sufficient?
5. What are its backend retention, residency, and access controls?

If any answer is unknown, do not export the field.

## Baggage

Baggage is disabled by default because it crosses process boundaries and can be
copied into many telemetry records. Trusted extraction requires endpoint-level
authentication, an allow-list, byte and item bounds, and a documented purpose.
Do not use baggage for secrets or identifiers.

## Error handling

Raw `error.Error()` output often includes URLs, SQL, values, or credentials.
Adapters use fixed error descriptions and bounded outcome enums. Applications
adding custom events must classify errors rather than recording their text.

## Review evidence

Privacy regression tests inject strings containing `secret` into every
untrusted input and inspect recorded spans. Fuzzing covers malformed and
oversized propagation metadata. Add equivalent assertions with every new
instrumentation package.
