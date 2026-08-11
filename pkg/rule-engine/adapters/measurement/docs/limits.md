# Limits and security

Tagged inputs are rejected above `MaxTaggedValueBytes` (100,020 bytes). The
bound accommodates measurement quantities valid under the owned decimal
package's default 100,000-digit limit plus version and unit metadata. Decimal
parsing and conversion retain the measurement and math packages' coefficient,
output, exponent, intermediate-bit, and expansion limits.

Evaluation performs no network, filesystem, environment, clock, random, or
registry access. It starts no goroutine, holds no mutable shared state, and
uses no float conversion. Returned operator and signature slices may be
mutated by callers without affecting later calls.

Cancellation is checked before parsing, between operands, and after exact
comparison. Measurement conversion is bounded but currently synchronous, so a
cancellation arriving inside one bounded arithmetic operation is observed at
the next check rather than interrupting that operation.

Treat manually assembled tags as untrusted input. Prefer `Quantity` so amounts
and unit identities originate from already validated immutable quantities.
