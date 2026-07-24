package idempotency_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestOwnershipContextRoundTrip(t *testing.T) {
	key, err := idempotency.NewKey("context", "tenant", "operation", "caller", "key")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	ownership := idempotency.Ownership{Key: key, OwnerToken: "owner", FencingToken: 42}
	ctx := idempotency.WithOwnership(context.Background(), ownership)
	actual, found := idempotency.OwnershipFromContext(ctx)
	if !found || actual != ownership {
		t.Fatalf("OwnershipFromContext() = %#v, %t", actual, found)
	}
	if _, found := idempotency.OwnershipFromContext(context.Background()); found {
		t.Fatal("OwnershipFromContext() found absent ownership")
	}
}
