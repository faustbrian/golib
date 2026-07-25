# Service accounts

Give each workload a stable subject distinct from its credential ID. A key ID
or JWT `kid` identifies verification material; it is not the service identity.
Use method, issuer, audience, and authentication time to describe how the
identity was established.

Prefer asymmetric JWTs or OIDC workload identities when a trusted issuer is
available. For smaller controlled deployments, use an opaque bearer token,
API key, or TLS-protected Basic credential. Scope and tenant hints are asserted
authentication data only; authorization must validate them against policy.

Issue separate credentials per workload, bound their lifetime, monitor failure
outcomes, and rehearse revocation and rotation.
