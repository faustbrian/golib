package adapter

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
)

func TestOwnershipErrorAndInvalidConcerns(t *testing.T) {
	t.Parallel()
	ownership := &OwnershipError{Concern: RequestID}
	if !strings.Contains(ownership.Error(), "request-id") || !errors.Is(ownership, ErrDuplicateOwnership) {
		t.Fatalf("error = %v", ownership)
	}
	if _, err := Named(Concern("unknown"), func(next http.Handler) http.Handler { return next }); err == nil {
		t.Fatal("invalid concern accepted")
	}
	chain, _ := middleware.New()
	if err := ValidateGoService(chain, []Concern{"unknown"}); err == nil {
		t.Fatal("invalid service concern accepted")
	}
}

func TestValidationAllowsDisjointOwnership(t *testing.T) {
	t.Parallel()
	descriptor, err := Named(Authentication, func(next http.Handler) http.Handler { return next })
	if err != nil {
		t.Fatal(err)
	}
	chain, err := middleware.Described(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGoService(chain, []Concern{Recovery}); err != nil {
		t.Fatalf("validation error = %v", err)
	}
}
