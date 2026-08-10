# Integrity and export verification

`Chain` supports SHA-256 and HMAC-SHA-256 using standard-library primitives.
HMAC keys come from `KeyProvider`; the library does not store or derive keys.
Partition chains by a stable scope whose ordering can be serialized, commonly
tenant plus service. Every link records partition, sequence, previous digest,
algorithm, key ID when keyed, and digest.

Key rotation is selected by the provider using partition and recording time.
Verification supplies the exact persisted key ID in `KeyRequest`, so providers
must return that historical key rather than whichever key is current. Old keys
must remain available for verification. Partitions and key IDs are
valid UTF-8 bounded fields. HMAC keys contain 32 to 1024 bytes and are copied
only for the operation; providers retain lifecycle ownership. Store a
`Checkpoint` outside
the audit database after verifying a stable export. `VerifyFromCheckpoint`
binds a suffix to its prefix and expected final checkpoint, detecting missing
links and truncation. `MerkleRoot` provides an order-sensitive export anchor;
hold its root independently with export query, canonical schema version, count,
first and last cursor, and key metadata.

Repair must never rewrite a historical record in place. Preserve the damaged
range, record the incident separately, establish a new partition or explicit
repair checkpoint, and document the trust boundary. Independent key and
checkpoint custody is required before claiming resistance to a privileged
storage attacker.
