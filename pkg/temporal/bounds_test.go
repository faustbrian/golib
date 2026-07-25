package temporal_test

import (
	"errors"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestBoundsExposeEveryInclusionCombination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		bounds       temporal.Bounds
		includeStart bool
		includeEnd   bool
	}{
		{"half open", temporal.ClosedOpen, true, false},
		{"closed", temporal.Closed, true, true},
		{"open", temporal.Open, false, false},
		{"open closed", temporal.OpenClosed, false, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.bounds.IncludesStart(); got != test.includeStart {
				t.Fatalf("IncludesStart() = %v, want %v", got, test.includeStart)
			}

			if got := test.bounds.IncludesEnd(); got != test.includeEnd {
				t.Fatalf("IncludesEnd() = %v, want %v", got, test.includeEnd)
			}
		})
	}
}

func TestBoundsSideHelpersAreExplicitAndImmutable(t *testing.T) {
	t.Parallel()

	if !temporal.Start.Valid() || !temporal.End.Valid() || temporal.Side(0).Valid() {
		t.Fatal("Side validity mismatch")
	}
	if temporal.Start.String() != "start" || temporal.End.String() != "end" || temporal.Side(0).String() != "" {
		t.Fatal("Side names mismatch")
	}
	bounds, err := temporal.Open.WithSide(temporal.Start, true)
	if err != nil || bounds != temporal.ClosedOpen {
		t.Fatalf("WithSide(start) = %v, %v", bounds, err)
	}
	bounds, err = bounds.WithSide(temporal.End, true)
	if err != nil || bounds != temporal.Closed {
		t.Fatalf("WithSide(end) = %v, %v", bounds, err)
	}
	bounds, err = bounds.WithSide(temporal.Start, false)
	if err != nil || bounds != temporal.OpenClosed {
		t.Fatalf("WithSide(exclude start) = %v, %v", bounds, err)
	}
	bounds, err = bounds.WithSide(temporal.End, false)
	if err != nil || bounds != temporal.Open {
		t.Fatalf("WithSide(exclude end) = %v, %v", bounds, err)
	}
	if included, err := temporal.Closed.Includes(temporal.Start); err != nil || !included {
		t.Fatalf("Includes(start) = %v, %v", included, err)
	}
	if included, err := temporal.Closed.Includes(temporal.End); err != nil || !included {
		t.Fatalf("Includes(end) = %v, %v", included, err)
	}
	if _, err := bounds.WithSide(0, false); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("WithSide(invalid) error = %v", err)
	}
	if _, err := bounds.Includes(0); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("Includes(invalid) error = %v", err)
	}
}

func TestBoundsHelpersReturnReplacements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bounds       temporal.Bounds
		includeStart temporal.Bounds
		includeEnd   temporal.Bounds
		excludeStart temporal.Bounds
		excludeEnd   temporal.Bounds
	}{
		{temporal.ClosedOpen, temporal.ClosedOpen, temporal.Closed, temporal.Open, temporal.ClosedOpen},
		{temporal.Closed, temporal.Closed, temporal.Closed, temporal.OpenClosed, temporal.ClosedOpen},
		{temporal.Open, temporal.ClosedOpen, temporal.OpenClosed, temporal.Open, temporal.Open},
		{temporal.OpenClosed, temporal.Closed, temporal.OpenClosed, temporal.OpenClosed, temporal.Open},
	}

	for _, test := range tests {
		if got := test.bounds.IncludeStart(); got != test.includeStart {
			t.Errorf("%v.IncludeStart() = %v, want %v", test.bounds, got, test.includeStart)
		}
		if got := test.bounds.IncludeEnd(); got != test.includeEnd {
			t.Errorf("%v.IncludeEnd() = %v, want %v", test.bounds, got, test.includeEnd)
		}
		if got := test.bounds.ExcludeStart(); got != test.excludeStart {
			t.Errorf("%v.ExcludeStart() = %v, want %v", test.bounds, got, test.excludeStart)
		}
		if got := test.bounds.ExcludeEnd(); got != test.excludeEnd {
			t.Errorf("%v.ExcludeEnd() = %v, want %v", test.bounds, got, test.excludeEnd)
		}
	}
}

func TestBoundsRejectInvalidValue(t *testing.T) {
	t.Parallel()

	invalid := temporal.Bounds(255)
	if invalid.Valid() {
		t.Fatal("Valid() = true for an unknown bounds value")
	}
	if got := invalid.String(); got != "" {
		t.Fatalf("String() = %q, want empty string", got)
	}

	if _, err := invalid.MarshalText(); err == nil {
		t.Fatal("MarshalText() error = nil for an unknown bounds value")
	}
}

func TestBoundsTextRoundTrip(t *testing.T) {
	t.Parallel()

	for _, bounds := range temporal.AllBounds() {
		encoded, err := bounds.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", bounds, err)
		}

		var decoded temporal.Bounds
		if err := decoded.UnmarshalText(encoded); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", encoded, err)
		}

		if decoded != bounds {
			t.Fatalf("round trip = %v, want %v", decoded, bounds)
		}
	}
}

func TestBoundsTextRejectsUnknownAndOversizedInput(t *testing.T) {
	t.Parallel()

	for _, input := range [][]byte{[]byte("unknown"), make([]byte, temporal.DefaultLimits().ParseBytes+1)} {
		var bounds temporal.Bounds
		if err := bounds.UnmarshalText(input); err == nil {
			t.Fatalf("UnmarshalText(%q) error = nil", input)
		}
	}
}
