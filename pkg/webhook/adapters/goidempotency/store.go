// Package goidempotency adapts idempotency leases to webhook replay checks.
package goidempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	idempotency "github.com/faustbrian/golib/pkg/idempotency"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

var (
	ErrInvalidConfig = errors.New("goidempotency: invalid configuration")
	ErrInvalidExpiry = errors.New("goidempotency: expiry must be in the future")
)

// Config supplies the durable service and full collision scope.
type Config struct {
	Service   *idempotency.Service
	Namespace string
	Tenant    string
	Operation string
	Caller    string
	Clock     func() time.Time
}

// Store implements webhook.ReplayStore with fail-closed durable leases.
type Store struct {
	service   *idempotency.Service
	namespace string
	tenant    string
	operation string
	caller    string
	clock     func() time.Time
}

var _ webhook.ReplayStore = (*Store)(nil)

// New validates the durable service and every key scope component.
func New(config Config) (*Store, error) {
	if config.Service == nil || config.Namespace == "" || config.Tenant == "" ||
		config.Operation == "" || config.Caller == "" {
		return nil, ErrInvalidConfig
	}
	if _, err := idempotency.NewKey(
		config.Namespace,
		config.Tenant,
		config.Operation,
		config.Caller,
		"validation",
	); err != nil {
		return nil, fmt.Errorf("%w: invalid scope: %v", ErrInvalidConfig, err)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}

	return &Store{
		service:   config.Service,
		namespace: config.Namespace,
		tenant:    config.Tenant,
		operation: config.Operation,
		caller:    config.Caller,
		clock:     clock,
	}, nil
}

// CheckAndRecord acquires a lease through idempotency. Acquired and stale
// takeover outcomes are new records; every retained matching outcome is a
// replay. Backend unavailability remains an error through fail-closed mode.
func (s *Store) CheckAndRecord(ctx context.Context, value string, expiresAt time.Time) (bool, error) {
	now := s.clock().UTC()
	lease := expiresAt.Sub(now)
	if lease <= 0 {
		return false, ErrInvalidExpiry
	}
	key, err := idempotency.NewKey(s.namespace, s.tenant, s.operation, s.caller, value)
	if err != nil {
		return false, fmt.Errorf("goidempotency: replay key: %w", err)
	}
	// The version is a non-empty compile-time protocol constant, so this
	// constructor cannot fail after dependency contract validation.
	fingerprint, _ := idempotency.NewFingerprint("go-webhook-replay-v1", []byte(value))
	result, err := s.service.Begin(ctx, idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{
			Key:         key,
			Fingerprint: fingerprint,
			Lease:       lease,
		},
		Availability: idempotency.AvailabilityFailClosed,
	})
	if err != nil {
		return false, fmt.Errorf("goidempotency: acquire replay lease: %w", err)
	}

	return result.Execute, nil
}
