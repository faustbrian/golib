# Persistence

`Schedule.Value` emits canonical JSON bytes and `Schedule.Scan` accepts JSONB
bytes, strings, or SQL `NULL`. `NULL` becomes the fail-closed zero schedule.
Invalid values leave a typed error and do not expose input.

The `postgres.JSONB` wrapper distinguishes nullable database state from a valid
zero schedule. The root type implements the interfaces selected by pgx JSONB's
native codec, so no global connection registration is necessary.

Recommended schema:

```sql
create table resource_availability (
    resource_id bigint primary key,
    schedule jsonb not null,
    revision text not null,
    check (jsonb_typeof(schedule) = 'object')
);
```

The application owns migrations from old columns. Decode legacy values, build a
schedule, canonicalize it, verify representative instants, then write JSONB in
a reversible migration. See [legacy migration](legacy-migration.md).

## Round-trip matrix

| Surface | Accepted representation | Preserved contract |
| --- | --- | --- |
| canonical JSON | version 1 object | timezone, weekly rules, exceptions, metadata, effective dates, precision |
| canonical text | the same bounded JSON bytes | byte-stable canonical form |
| `database/sql` value | canonical `[]byte` | complete non-null schedule |
| `database/sql` scan | `[]byte` or `string` | detached immutable schedule |
| SQL `NULL` | `nil` | fail-closed zero schedule |
| `postgres.JSONB` | valid or nullable wrapper | validity flag plus complete schedule |
| pgx v5 JSONB codec | native scanner/valuer selection | no registry or connection-global state |
| PostgreSQL 14-18 | `jsonb` column | canonical schedule after server round trip |

Canonical encoding retains nanosecond local-time precision. A decoder rejects
trailing values, duplicates, invalid UTF-8, unknown members, unsupported wire
versions, oversized documents, and invalid or lossy interval states before a
value can reach persistence.
