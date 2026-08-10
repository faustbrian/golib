# Migrations

When adopting from embedded registry clients:

1. Inventory each stored wire framing version, provider scope, subject strategy,
   schema format/dialect, references, and compatibility policy.
2. Compile schemas and record portable fingerprints without replacing existing
   provider IDs.
3. Introduce explicit parse-resolve-decode calls and bounded caches.
4. Produce and verify an offline bundle for currently readable schemas.
5. Shadow compatibility checks and registration outcomes before enforcing them.
6. Migrate producers, then consumers, and retain old schema versions until all
   stored messages have aged out.

Changing Confluent subject strategy, Glue registry/name, Protobuf message index,
or wire framing is a protocol migration even when schema content is unchanged.

## Provider switch and rollback exercise

Treat provider migration as an application-owned protocol rollout:

1. Export every still-readable definition, provider ID, subject/version,
   lifecycle, reference edge, wire version, and portable fingerprint. Verify an
   immutable offline bundle before changing traffic.
2. Register the same compiled schema explicitly with the target. Preserve the
   source and target provider IDs separately; an equal portable fingerprint is
   not permission to substitute one provider's ID in the other's frame.
3. Resolve the target-issued ID and compare the returned portable fingerprint
   before producing target-framed messages. An unknown registration outcome
   blocks cutover until this reconciliation succeeds.
4. Dual-register new versions and shadow-read target resolutions while
   continuing to produce source framing. Do not let a target outage implicitly
   redirect a provider ID to the source.
5. Cut over producers first. Consumers remain dual-read by parsing the explicit
   wire version/provider framing, selecting the matching resolver, and applying
   its chosen cache/outage policy.
6. Roll back by returning production to source-issued framing while retaining
   the target versions. Stored target-framed messages remain readable through
   the target resolver or a verified offline bundle.

`TestExplicitDualRegistrationCutoverFailoverAndRollback` exercises this
sequence with distinct provider scopes, an ambiguous target registration, a
verified cutover, explicit outage failover, cross-provider ID rejection, and
rollback. Disaster recovery uses the same rule: restore the registry export or
bundle first, verify fingerprints, then restore provider-specific routing.
