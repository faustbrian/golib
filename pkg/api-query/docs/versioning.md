# Schema and cursor versioning

Schema revision identifies the full application query behavior, not merely a
database migration. Change it when clients could observe a capability, default,
projection, filter, sort, page, cost, error, or canonical-plan difference.
Requests may send the expected revision; mismatches fail predictably.

Cursor protocol version identifies the encrypted wire contract and validation
policy. It is authenticated outside and inside the encrypted payload. A cursor
also embeds the schema revision and exact sort list, so compatible key rotation
does not require a protocol change while semantic changes fail closed.

Recommended rollout:

1. Publish the new schema revision and cursor version support.
2. Continue decoding retained old-key cursors only under their original
   version/schema/sort contract.
3. Issue new cursors exclusively under the new contract.
4. Remove old schemas and keys after the announced compatibility window and
   maximum TTL.

Never reuse a revision or cursor version for different semantics. Record changes
in `CHANGELOG.md` and update the API export baseline only for intentional,
reviewed compatible additions or a new major version.
