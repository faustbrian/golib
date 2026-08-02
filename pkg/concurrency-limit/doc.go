// Package concurrencylimit provides bounded, process-local adaptive in-flight
// admission. A Limiter owns concurrency measurement and learning; callers own
// timeouts, retries, hedges, breakers, quotas, fallbacks, and authorization.
//
// Every admitted Permit requires exactly one terminal Outcome. Execute is the
// preferred helper when one function call owns the permit lifecycle. Queue wait
// is excluded from measured execution latency because a permit's monotonic start
// timestamp is recorded only when admission is granted.
//
// Adaptive state is pod-local. Reset starts a new generation at InitialLimit
// and invalidates old permits; it does not import or infer fleet capacity.
package concurrencylimit
