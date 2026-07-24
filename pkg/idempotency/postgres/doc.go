// Package postgres implements durable idempotency storage on PostgreSQL using
// pgx, transaction-scoped advisory locks, row locks, server time, and bounded
// retention cleanup.
package postgres
