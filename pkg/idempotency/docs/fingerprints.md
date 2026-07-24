# Fingerprint policy

An idempotency key identifies the caller's intended operation. A fingerprint
identifies the stable business request associated with that key. Reusing a key
with a different fingerprint is always a conflict, including after completion
or terminal failure.

## Versioning

Every fingerprint has an application-selected, nonempty version. Change the
version whenever canonicalization rules or the selected business fields change.
During a rolling deployment, all versions that may already be persisted must
remain readable and comparable. A version change does not make an existing key
available for unrelated input; it produces a conflict for that key.

Recommended version names describe the policy rather than a deployment, for
example `create-order-jcs-v1`. Do not use a build identifier or current date
unless the business identity intentionally changes with every deployment.

## Selecting fields

Include fields that change the meaning or side effects of the operation. Exclude
transport details that can legitimately change between retries, including:

- tracing and correlation headers;
- connection addresses and user agents;
- request timestamps added by gateways;
- JSON whitespace and object-property order;
- retry counters and delivery attempt metadata.

Tenant, operation, and caller identity belong in the namespaced key even if they
also appear in the payload. Authentication context must be reduced to a stable
caller identity; never hash raw bearer tokens or credentials.

## JSON Canonicalization Scheme

`canonical.JSON` applies RFC 8785 JCS after enforcing caller-supplied byte and
nesting limits. It rejects malformed UTF-8, duplicate object names, invalid
Unicode surrogate escapes, numbers outside the IEEE-754 binary64 domain, and
negative zero. JCS does not normalize Unicode strings; canonically equivalent
Unicode sequences remain different unless the application explicitly normalizes
them before calling this package.

JCS treats JSON numbers as binary64 values. Financial amounts, identifiers, and
integers that must retain precision beyond 53 bits should be represented as
strings under an application schema.

## Raw bytes

`canonical.BytesFingerprint` hashes the exact supplied bytes and requires an
explicit positive maximum. Use it only when the encoding is already stable by
contract, such as a versioned protobuf deterministic serialization. It does not
remove headers, normalize text, or understand business fields.

## Bounds and secrecy

Canonicalization limits are application policy and must be chosen before input
is processed. Errors expose a stable reason and field but omit the payload and
dependency error text. Logs and telemetry should emit the fingerprint version
and a bounded hash prefix only when collision risk is acceptable for that
diagnostic use; raw keys, payloads, and tenant identifiers are not safe metric
labels.
