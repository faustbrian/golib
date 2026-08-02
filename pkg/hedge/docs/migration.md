# Migration

This is a new module. To migrate from a retry-only call, keep retry and hedge as
separate layers, inventory every side effect, add independent request-state
construction, choose a shared amplification budget, define classification and
cleanup, and introduce a conservative measured delay. Roll out per resource
while watching amplification, budget denials, cleanup, and tail latency.

Failsafe-Go users must explicitly supply the dependencies and ownership that a
builder may default: total deadline, clock, replay declaration, shared budget,
factory failure mode, classifier, and disposer.
