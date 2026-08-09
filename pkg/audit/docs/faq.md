# FAQ

## Is an event store an audit trail?

No. Domain events and event-sourcing history serve business state reconstruction.
An audit trail needs its own actor, access, privacy, delivery, retention,
integrity, and operational controls.

## Does this library make us compliant?

No. It supplies explicit technical contracts. Legal scope, policy, operations,
access governance, custody, and evidence determine compliance.

## Can core discover the current user or tenant?

No. The caller supplies stable identities after its own authentication,
authorization, and tenancy decisions.

## Can I retry an unknown append?

Reconcile by record ID first when possible, then retry only the exact same ID
and canonical record. Identical duplicates are safe; different bytes conflict.

## May I put record IDs in metrics?

No. Observation hooks contain bounded counts, duration, append outcome, and a
fixed kind only. Keep identities out of metric labels and diagnostics.

## How do I erase personal data?

Minimize and pseudonymize before recording. Apply the caller's lawful retention,
erasure-exception, archival, and legal-hold policy through the privileged
two-phase retention boundary; never rewrite a historical record in place.
