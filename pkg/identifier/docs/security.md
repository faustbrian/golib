# Security

Default randomness comes from `crypto/rand.Reader`. Supplying another reader
transfers responsibility for independence, unpredictability, concurrency, and
failure behavior to the caller. `idtest.Reader` is deterministic and forbidden
for production.

Parsers reject non-canonical case, ambiguous Crockford symbols, malformed
prefixes, invalid UUID versions or variants where the UUID contract requires
them, Base32/Base62 overflow, duplicate NanoID alphabet bytes, non-ASCII NanoID
alphabets, and configurations below 120 entropy bits.

UUIDv7, ULID, TypeID, and KSUID leak creation time. Monotonic generators leak
relative issuance order within their time bucket. TypeID prefixes disclose
entity categories. KSUID payload increments and UUIDv7/ULID increments can
make neighboring values guessable after one value is observed. These are
database identifiers, not secrets.

The exposed timestamp is 48 bits at millisecond resolution for UUIDv7, ULID,
and generated TypeIDs, and 32 bits at second resolution after the KSUID epoch
for KSUID. A TypeID additionally exposes up to 63 ASCII prefix bytes. Generated
families contain no node or topology field. Parse-only UUIDv1 and UUIDv6 values
can retain an externally supplied 48-bit node field. UUIDv4 and NanoID expose
no timestamp or topology field.

Every identifier implements `slog.LogValuer` and emits `[REDACTED]` when passed
directly to Go's structured logger. Revealing one requires an explicit call to
`String`. Other logging systems and metric labels are caller-owned boundaries:
apply an explicit data-classification decision, retention policy, and
cardinality budget, and never put raw identifiers in metric labels by default.
The package emits no logs or metrics of its own and provides no automatic raw
identifier labels.

Generator rollback and overflow errors are operational signals. Do not retry in
a tight loop or silently switch algorithms. Repair the clock, rotate to a new
owned generator only when duplicate domains are understood, or apply bounded
backpressure according to the application's availability contract.
