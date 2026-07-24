# Serialization

Every family supports canonical text, JSON strings, binary encoding,
`database/sql.Scanner`, and `driver.Valuer`. An unassigned Go value generally
maps to SQL `NULL` and JSON `null`. TypeID follows the official Go
implementation instead: its zero value is the valid unprefixed all-zero TypeID
and serializes as `00000000000000000000000000`. Invalid values are rejected
before replacing the receiver.

UUID, ULID, and KSUID binary encodings are their raw 16-, 16-, and 20-byte
representations. TypeID binary encoding is canonical text because the prefix
is part of the value. NanoID binary encoding is its configured text.

UUID additionally implements `pgtype.UUIDScanner` and `pgtype.UUIDValuer` for
PostgreSQL UUID columns. Other families use text columns because PostgreSQL's
UUID type cannot preserve their text encoding or prefix.

Custom NanoID decoding needs its configuration. Call `nanoid.Prepare(config)`
before JSON, binary, or SQL decoding; a zero `nanoid.ID` uses the default
alphabet and size.

Marshal methods return newly allocated byte slices. `Bytes` methods return
arrays by value, so callers cannot mutate an identifier through an alias.
