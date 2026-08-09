# Threat model and compliance boundary

The library treats every caller, record field, sink result, database response,
and export consumer as untrusted input. Validation and defensive ownership
limit field injection and alias mutation. Explicit tenant scopes reduce tenant
confusion but do not authenticate a tenant. Actor kinds make anonymous, unknown,
system, human, service, and delegated identities explicit, but a compromised
writer can still forge actors, tenants, occurrence times, and business facts.

Redaction and default-deny field policies address secret leakage before
persistence and diagnostics. Bounded records, batches, queries, exports, and
buffer capacities provide backpressure for storage outage and disk-exhaustion
conditions; they cannot manufacture durable capacity. Fail-closed,
fail-open-with-alert, and durable-buffer modes make omission visible. Stable
record IDs and canonical bytes make identical retries idempotent and conflicting
duplicates rejectable. Commit errors remain unknown until reconciled.

Canonical encoding plus optional chains, external checkpoints, and Merkle roots
detect alteration, duplication, reordering, missing links, truncation, and
backdated records only relative to independently retained ordering evidence.
They do not prevent a compromised writer from omitting a record or a privileged
operator from replacing both the database and its co-located checkpoints.
Readers and export consumers can exfiltrate everything they are authorized to
read; malicious exports must therefore be tenant-bounded, access-controlled by
the caller, streamed to restricted storage, and verified before use.

It does not defend against a fully privileged database administrator, stolen
application and integrity keys, compromised writers or readers, compromised
caller policy, a caller that omits an action, false actor or tenant inputs,
malicious archive operators, or destruction of both records and independently
held checkpoints. Hashing alone does not prove who created a record and does
not provide non-repudiation.

The module does not decide authentication, authorization, read privileges,
business policy, action vocabularies, transport middleware, tenancy, legal
holds, erasure exceptions, or regulatory applicability. Deployment-specific
controls and evidence remain necessary for any compliance claim.
