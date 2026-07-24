// Package idempotencyhttp provides buffered net/http middleware backed by an
// idempotency.Service. Applications must supply semantic key and canonical
// fingerprint policies; the package does not infer business identity from
// unstable transport details.
package idempotencyhttp
