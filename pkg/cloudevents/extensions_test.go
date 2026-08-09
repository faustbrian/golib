package cloudevents

import (
	"errors"
	"testing"
)

func TestSelectedExtensionsEnforceNormativeSemantics(t *testing.T) {
	t.Parallel()

	traceParent, err := NewTraceParentAttribute("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err != nil {
		t.Fatalf("create traceparent: %v", err)
	}
	traceState, err := NewTraceStateAttribute("vendor=value,tenant@system=opaque")
	if err != nil {
		t.Fatalf("create tracestate: %v", err)
	}
	partitionKey, err := NewPartitionKeyAttribute("tenant-a")
	if err != nil {
		t.Fatalf("create partitionkey: %v", err)
	}
	if _, err := NewEvent(Attributes{
		ID:     "1",
		Source: "/source",
		Type:   "example",
		Extensions: map[string]Attribute{
			"traceparent":  traceParent,
			"tracestate":   traceState,
			"partitionkey": partitionKey,
		},
	}, Data{}); err != nil {
		t.Fatalf("create event with selected extensions: %v", err)
	}

	for _, invalid := range []string{
		"",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
	} {
		if _, err := NewTraceParentAttribute(invalid); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("traceparent %q error = %v", invalid, err)
		}
	}
	for _, invalid := range []string{"", "Upper=value", "a=value,b=other,a=duplicate", "a=has,comma"} {
		if _, err := NewTraceStateAttribute(invalid); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("tracestate %q error = %v", invalid, err)
		}
	}
	if _, err := NewPartitionKeyAttribute(""); !errors.Is(err, ErrInvalidAttribute) {
		t.Fatalf("empty partitionkey error = %v", err)
	}

	stateOnly, err := NewTraceStateAttribute("vendor=value")
	if err != nil {
		t.Fatalf("create state-only attribute: %v", err)
	}
	_, err = NewEvent(Attributes{
		ID:         "2",
		Source:     "/source",
		Type:       "example",
		Extensions: map[string]Attribute{"tracestate": stateOnly},
	}, Data{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("tracestate without traceparent error = %v", err)
	}
}
