# Database behavior

Use PostgreSQL `uuid` for UUID values. UUIDv7 generally improves B-tree page
locality over UUIDv4, but the timestamp prefix exposes creation time and does
not make the value globally sequential. The `uuid.ID` pgx methods preserve
native binary values and SQL `NULL`.

Store ULID as `char(26)` or a binary 16-byte value only after choosing one
representation for every reader. A binary migration changes collation and
tooling behavior even when values round-trip. Canonical uppercase ULID text
sorts in bytewise/C collation; locale-specific collations must be tested.

Store TypeID in a text column up to 90 bytes. A mixed-prefix index sorts by
prefix before time. If time locality is required across types, store the UUID
suffix in a separate native UUID column and keep the prefix as constrained
metadata.

Store KSUID as `char(27)` under bytewise collation or as 20 binary bytes. Its
locality is only second-granular. Store NanoID as exact-case text under a
case-sensitive collation; it has no locality benefit.

Unique constraints remain required for every family. Collision probability,
monotonic generation, or node ownership never replaces a database constraint.
Never regenerate an identifier during replication, import, or retry.
