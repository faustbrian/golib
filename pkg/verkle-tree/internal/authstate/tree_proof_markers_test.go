package authstate

import (
	"context"
	"testing"
)

type markerBudgetContext struct {
	context.Context
	successfulChecks int
}

func (ctx *markerBudgetContext) Err() error {
	if ctx.successfulChecks == 0 {
		return context.Canceled
	}
	ctx.successfulChecks--

	return nil
}

func TestDerivePathMarkersCoalescesSuffixHalves(t *testing.T) {
	t.Parallel()

	var lowMember Key
	lowAbsent := lowMember
	lowAbsent[31] = 1
	highAbsent := lowMember
	highAbsent[31] = 128
	markers, err := derivePathMarkers(
		&markerBudgetContext{
			Context:          context.Background(),
			successfulChecks: 100,
		},
		[]Claim{
			Membership(lowMember, Value{1}),
			Absence(lowAbsent),
			Absence(highAbsent),
		},
		[]StemPath{PresentStemPath(Stem(lowMember[:31]), 1)},
		10,
	)
	if err != nil {
		t.Fatalf("derive markers: %v", err)
	}
	if len(markers) != 3 {
		t.Fatalf("marker count = %d, want 3", len(markers))
	}
	if markers[1].kind != pathMarkerSuffix ||
		markers[1].path[1] != 2 ||
		markers[1].identityAllowed {
		t.Fatalf("low-half marker = %#v", markers[1])
	}
	if markers[2].kind != pathMarkerSuffix ||
		markers[2].path[1] != 3 ||
		!markers[2].identityAllowed {
		t.Fatalf("high-half marker = %#v", markers[2])
	}
}
