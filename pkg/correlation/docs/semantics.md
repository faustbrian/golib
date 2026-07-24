# Semantics

Correlation answers “which logical workflow?”, request answers “which exact
hop or attempt?”, and causation answers “which immediate parent produced this
hop?”. `Factory.Next` preserves only correlation, generates a request ID, and
copies the parent's request ID into causation. `Factory.Accept` does the same
for a receiving boundary only when the inbound policy explicitly trusts the
relevant fields.

An external identifier also records its kind, source, and trust. It is never
promoted to correlation implicitly. Correlation metadata must not control
identity, access, tenancy, billing, replay detection, or deduplication.
