# Identifier selection

Choose from requirements, not appearance.

Use UUIDv4 when interoperability and non-disclosure of creation time matter
more than index locality. Use UUIDv7 when a native UUID database column and
roughly chronological locality are wanted. Use ULID only when its 26-character
representation or an existing ULID schema is a compatibility requirement.

Use TypeID when a validated lowercase type prefix materially helps operators
and API consumers. Prefixes are metadata, not authorization boundaries. Use
KSUID for Segment-compatible 27-character data or second-resolution ordering.
Use NanoID for compact random URL-safe text when ordering and timestamp
inspection are explicitly unwanted.

Snowflake-style IDs are intentionally absent. A safe implementation requires a
deployment-specific node allocator, durable uniqueness leases, a defined clock
rollback policy, sequence-overflow backpressure, bit allocation, epoch, and
maximum fleet lifetime. A generic constructor cannot truthfully supply those
requirements. Applications with that deployment contract should use a
dedicated package whose configuration is reviewed with the allocator.

Do not use any family as a password, bearer token, authorization decision,
idempotency proof, or automatic correlation context. If unguessability is a
security property, generate and store a separate secret.
