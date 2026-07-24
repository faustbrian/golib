package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func BenchmarkParseConfig(b *testing.B) {
	for b.Loop() {
		config, err := ParseConfig(Config{
			DSN:      "postgres://app:secret@localhost/app?sslmode=disable",
			MaxConns: 20,
		})
		if err != nil || config.MaxConns != 20 {
			b.Fatalf("ParseConfig() = %#v, %v", config, err)
		}
	}
}

func BenchmarkClassify(b *testing.B) {
	err := &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}

	for b.Loop() {
		if info := Classify(err); info.Kind != ErrorUniqueViolation {
			b.Fatalf("Classify().Kind = %q", info.Kind)
		}
	}
}

func BenchmarkPoolAcquireOverhead(b *testing.B) {
	pool := newPool(nil, &stubPoolBackend{}, time.Second, time.Second, time.Second)
	ctx := context.Background()

	for b.Loop() {
		if _, err := pool.Acquire(ctx); err != nil {
			b.Fatalf("Acquire() error = %v", err)
		}
	}
}

func BenchmarkPoolCreation(b *testing.B) {
	ctx := context.Background()
	config := Config{
		DSN:           "postgres://localhost/app?sslmode=disable",
		StartupPolicy: StartupLazy,
	}

	for b.Loop() {
		pool, err := New(ctx, config)
		if err != nil {
			b.Fatalf("New() error = %v", err)
		}
		if err := pool.Close(ctx); err != nil {
			b.Fatalf("Close() error = %v", err)
		}
	}
}

func BenchmarkObserverInstrumentation(b *testing.B) {
	ctx := context.Background()
	observer := ObserverFunc(func(context.Context, Observation) {})
	observation := Observation{
		Operation:    OperationAcquire,
		Outcome:      OutcomeSuccess,
		Pool:         Stats{MaxConns: 10},
		HasPoolStats: true,
	}

	for b.Loop() {
		observer.Observe(ctx, observation)
	}
}

func BenchmarkRunTransactionOverhead(b *testing.B) {
	ctx := context.Background()
	tx := &stubTx{}
	beginner := &stubBeginner{tx: tx}
	callback := func(context.Context, pgx.Tx) error { return nil }

	for b.Loop() {
		if err := RunTransaction(ctx, beginner, TransactionOptions{}, callback); err != nil {
			b.Fatalf("RunTransaction() error = %v", err)
		}
	}
}
