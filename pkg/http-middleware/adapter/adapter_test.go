package adapter_test

import (
	"errors"
	"net/http"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/adapter"
)

func TestGoServiceOwnershipRejectsDuplicateCoreMiddleware(t *testing.T) {
	t.Parallel()

	descriptor, err := adapter.Named(adapter.RequestID, func(next http.Handler) http.Handler { return next })
	if err != nil {
		t.Fatalf("Named() error = %v", err)
	}
	chain, err := middleware.Described(descriptor)
	if err != nil {
		t.Fatalf("Described() error = %v", err)
	}
	err = adapter.ValidateGoService(chain, adapter.GoServiceDefaults())
	if !errors.Is(err, adapter.ErrDuplicateOwnership) {
		t.Fatalf("ValidateGoService() error = %v", err)
	}
}

func TestOwningPackageConcernsComposeWithoutPolicyDuplication(t *testing.T) {
	t.Parallel()

	descriptors := make([]middleware.Descriptor, 0, 5)
	for _, concern := range []adapter.Concern{adapter.Authentication, adapter.Authorization, adapter.RateLimit, adapter.Idempotency, adapter.Telemetry} {
		descriptor, err := adapter.Named(concern, func(next http.Handler) http.Handler { return next })
		if err != nil {
			t.Fatalf("Named(%q) error = %v", concern, err)
		}
		descriptors = append(descriptors, descriptor)
	}
	chain, err := middleware.Described(descriptors...)
	if err != nil || len(chain.Descriptors()) != 5 {
		t.Fatalf("chain = %#v, error = %v", chain, err)
	}
}
