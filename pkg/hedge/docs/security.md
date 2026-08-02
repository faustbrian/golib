# Security

Treat replay authorization as a security decision. Duplicate writes can cause
financial loss, repeated acknowledgements, inconsistent transactions, or
cross-tenant routing. Require independently owned bodies and destinations,
bounded contexts, trusted delay input, finite counts, and a shared budget.

Observers contain no raw values or errors. Resource and endpoint identities
must be credential-free and bounded. Error strings from the execution wrapper
do not concatenate downstream errors, though callers can deliberately inspect
the selected cause with `errors.Is` or `errors.As`.
