# Operations

## Startup

Apply the PostgreSQL migration, construct the repository and compiler, load and
compile the current manifest, then create the engine. Do not serve protected
traffic until a verified snapshot is active. A missing initial manifest is an
operator error, not an empty policy.

Start the repository synchronizer as the correctness loop and the Valkey
watcher as a latency optimization. Use the synchronizer, rather than its raw
engine, as the `Authorizer` for protected operations. `Synchronizer.Decide`
fails closed until the first successful repository verification and after the
configured maximum staleness. Supervise both loops. A returned error must be
logged and retried with bounded backoff or cause the instance to leave service
according to the application's availability policy.

## Health and readiness

Readiness should require:

- an initialized engine with a verified non-zero revision;
- a successful repository read within the application's freshness objective;
- no persistent compile or snapshot-replacement failure; and
- an active revision within the accepted fleet convergence window.

Valkey connectivity alone is not readiness because Valkey is not authoritative.
A temporary Valkey outage can remain ready while repository polling succeeds.

## Metrics and alerts

Record bounded decision count and duration through `authotel`. Alert on
evaluation errors, policy panics, invalid requests above the expected client
baseline, repeated reload failures, revision lag, revision divergence, and
repository optimistic conflicts.

Do not label metrics by subject, tenant, resource, policy ID, reason, or
revision. These have high or attacker-controlled cardinality. Use protected
audit logs for bounded policy IDs and correlate with deployment records.

## Publication runbook

1. Validate, compile, diff, and dry-run the candidate.
2. Confirm every deployed version supports its formats.
3. Persist with the expected revision.
4. Publish the Valkey revision hint.
5. Observe fleet convergence and decision errors.
6. Retain the candidate, diff, approver, timestamp, and resulting revision.

If publication conflicts, reload the current manifest and rebuild the candidate
rather than retrying with a guessed expectation.

## Incident response

For an accidental grant, publish a higher revision containing an explicit deny
or the last known-safe semantics. Do not lower or rewrite the stored revision.
If policy safety cannot be established, disable the protected operation or
remove affected instances from service.

For a compile failure, keep the last verified snapshot, capture the stable
policy ID and error class, and quarantine the candidate. Never log the complete
document or attacker-controlled attribute values by default.

For repository unavailability, configure a documented maximum-staleness policy
with `policy.WithMaxStaleness`. The default is two minutes. A successful
same-revision repository read refreshes verification; failed, invalid, stale,
or rolled-back reads do not.

While the repository is healthy, direct polling detects a publication within
the configured synchronization interval plus repository, compile, and
replacement latency. During repository failure, `Synchronizer.Decide` bounds
the stale-policy window and returns `ErrPolicyStale` with
`ReasonPolicyStale` after that age. Calls made directly to `Engine.Decide`
intentionally bypass repository freshness and are unsafe for operations whose
revocations require that bound. For an emergency revocation, persist the higher
revision and synchronously reload local instances where possible; Valkey
wakeups reduce latency but are never the revocation guarantee.

## Backup and recovery

Back up authoritative manifests and their audit metadata with PostgreSQL. A
Valkey backup is not required for authorization correctness. Recovery restores
the latest verified manifest, then publishes a new higher revision if policy
state must change after recovery.

Periodically exercise startup from backup, repository polling without Valkey,
duplicate and out-of-order invalidations, and a rolling rollback using a new
revision.
