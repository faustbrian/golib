package correlation_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

type foreignKey struct{}

func TestIDsRemainSemanticallyDistinctAndValidated(t *testing.T) {
	policy := correlation.Policy{MaxLength: 16}

	correlationID, err := correlation.ParseCorrelationID("flow_123", policy)
	if err != nil {
		t.Fatalf("ParseCorrelationID() error = %v", err)
	}
	requestID, err := correlation.ParseRequestID("request-456", policy)
	if err != nil {
		t.Fatalf("ParseRequestID() error = %v", err)
	}
	causationID, err := correlation.ParseCausationID("parent-789", policy)
	if err != nil {
		t.Fatalf("ParseCausationID() error = %v", err)
	}

	values := correlation.Values{
		CorrelationID: correlationID,
		RequestID:     requestID,
		CausationID:   causationID,
	}
	if values.CorrelationID.String() != "flow_123" ||
		values.RequestID.String() != "request-456" ||
		values.CausationID.String() != "parent-789" {
		t.Fatalf("Values = %#v", values)
	}

	for _, invalid := range []string{"", " leading", "trailing ", "contains space", "control\n", "nonascii-ä", "too-long-for-limit"} {
		if _, err := correlation.ParseCorrelationID(invalid, policy); !errors.Is(err, correlation.ErrInvalidID) {
			t.Errorf("ParseCorrelationID(%q) error = %v, want ErrInvalidID", invalid, err)
		}
	}
}

func TestContextValuesAreImmutableAndCollisionSafe(t *testing.T) {
	original := context.WithValue(context.Background(), foreignKey{}, "foreign")
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request", correlation.Policy{}),
	}

	derived := correlation.WithValues(original, values)
	if _, ok := correlation.FromContext(original); ok {
		t.Fatal("original context was mutated")
	}
	got, ok := correlation.FromContext(derived)
	if !ok || got != values {
		t.Fatalf("FromContext() = %#v, %v", got, ok)
	}
	if reflect.TypeOf(got.CorrelationID) == reflect.TypeOf(got.RequestID) {
		t.Fatal("correlation and request identifiers share a type")
	}
}
