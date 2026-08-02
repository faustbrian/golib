# Endpoint diversity

The core passes `AttemptInfo` to the caller and does not discover or select
endpoints. Hedging twice to one saturated pod can add load without improving
latency. A factory may choose another known healthy connection, but diversity
must preserve authentication, tenant routing, residency, consistency, and
duplicate suppression. It does not authorize routing to arbitrary replicas or
regions.

Only a bounded, credential-free endpoint identity belongs in observations.
Never use raw URLs, query strings, tokens, request bodies, results, or raw
errors as labels.
