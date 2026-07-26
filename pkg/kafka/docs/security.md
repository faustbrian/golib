# Security

`ClientSecurity` clones TLS configuration, defaults its minimum to TLS 1.2, and
rejects disabled certificate verification, obsolete minimum versions, and
inconsistent maximum versions. Callers may supply a franz-go SASL mechanism.

Credentials remain caller-owned and must come from a runtime secret provider.
They must not be embedded in topic names, client IDs, transactional IDs, logs,
metrics, traces, errors, fixtures, or replay audit records.

Use least-privilege ACLs per service role. Producers need only approved topic
write access, consumers need their topic read and group access, replay roles
must be separately authorized, and inspectors need read-only metadata and group
describe access. This module intentionally exposes no topic mutation, ACL
mutation, or consumer-group offset mutation.
