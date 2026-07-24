// Package breaker provides a protocol-neutral, bounded circuit breaker.
//
// Breakers own dependency-health admission, rolling outcome state, half-open
// probes, administrative modes, immutable snapshots, and transition events.
// Callers retain ownership of operation timeouts, retries, fallbacks, protocol
// classification, request and response resources, and protected results and
// errors.
package breaker
