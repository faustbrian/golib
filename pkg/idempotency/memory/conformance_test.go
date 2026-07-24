package memory_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestStoreConformance(t *testing.T) {
	idempotencytest.RunStoreConformance(t, func(t testing.TB) idempotencytest.StoreFixture {
		t.Helper()

		clock := idempotencytest.NewClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
		tokens := idempotencytest.NewTokenSource("conformance-owner")
		store, err := memory.New(memory.Options{
			Clock:       clock,
			OwnerTokens: tokens.Next,
		})
		if err != nil {
			t.Fatalf("memory.New() error = %v", err)
		}
		key, err := idempotency.NewKey("conformance", "tenant", "operation", "caller", t.Name())
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		fingerprint, err := idempotency.NewFingerprint("v1", []byte("canonical request"))
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}

		return idempotencytest.StoreFixture{
			Store:       store,
			Key:         key,
			Fingerprint: fingerprint,
			Advance:     clock.Advance,
		}
	})
}
