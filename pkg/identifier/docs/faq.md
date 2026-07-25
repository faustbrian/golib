# FAQ

## Which ID is safest?

Safety depends on the requirement. UUIDv4 avoids timestamp leakage; UUIDv7 and
ULID improve locality; TypeID adds visible metadata; NanoID is compact. None is
a secret or authorization token.

## Are sortable IDs globally ordered?

No. Monotonic order is guaranteed only inside one generator. Different
generators and processes can interleave, and clocks can disagree.

## Why does rollback return an error?

Silently clamping time would make inspection misleading and can exhaust a
sequence while hiding an infrastructure fault. The caller must choose its
availability policy explicitly.

## Why reject lowercase ULID and uppercase TypeID?

Accepting multiple spellings creates ambiguous signatures, cache keys, and
database equality behavior. Parsers accept exactly one canonical form.

## Can I use a smaller NanoID?

Not through this package if the alphabet and size provide under 120 ideal bits.
Use a non-identifier code package when a short human-entered code has different
collision, rate-limit, and abuse requirements.

## Why no Snowflake?

Node assignment, durable leases, rollback, sequence overflow, epoch, and fleet
lifetime are deployment concerns. A generic library cannot safely guess them.
