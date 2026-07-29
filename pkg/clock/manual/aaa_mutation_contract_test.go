package manual

import (
	"testing"
	"time"
)

func TestMutationContractsForAdvancementPredicates(t *testing.T) {
	request := &advanceRequest{
		target:         2,
		waiter:         newWaiter(&Clock{}),
		baseCallbackID: 3,
	}
	callback := func() {}

	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{
			name: "idle",
			got:  advancementIdle(nil, nil),
			want: true,
		},
		{
			name: "successful activation",
			got:  activationFailed(nil),
			want: false,
		},
		{
			name: "failed activation",
			got:  activationFailed(ErrActiveLimit),
			want: true,
		},
		{
			name: "request active",
			got:  advancementIdle([]*advanceRequest{request}, nil),
			want: false,
		},
		{
			name: "callback active",
			got:  advancementIdle(nil, map[uint64]struct{}{1: {}}),
			want: false,
		},
		{
			name: "empty callbacks",
			got:  hasActiveCallbacks(nil),
			want: false,
		},
		{
			name: "nonempty callbacks",
			got:  hasActiveCallbacks(map[uint64]struct{}{1: {}}),
			want: true,
		},
		{
			name: "nil event",
			got:  eventEligible(nil, 2),
			want: false,
		},
		{
			name: "event before target",
			got:  eventEligible(&scheduledEvent{deadline: 1}, 2),
			want: true,
		},
		{
			name: "event at target",
			got:  eventEligible(&scheduledEvent{deadline: 2}, 2),
			want: true,
		},
		{
			name: "event after target",
			got:  eventEligible(&scheduledEvent{deadline: 3}, 2),
			want: false,
		},
		{
			name: "nil callback",
			got:  hasCallback(nil),
			want: false,
		},
		{
			name: "callback",
			got:  hasCallback(callback),
			want: true,
		},
		{
			name: "target before elapsed",
			got:  requestWithinBounds(0, 1, 2),
			want: false,
		},
		{
			name: "target at elapsed",
			got:  requestWithinBounds(1, 1, 2),
			want: false,
		},
		{
			name: "target within bounds",
			got:  requestWithinBounds(2, 1, 3),
			want: true,
		},
		{
			name: "target at maximum",
			got:  requestWithinBounds(2, 1, 2),
			want: true,
		},
		{
			name: "target after maximum",
			got:  requestWithinBounds(3, 1, 2),
			want: false,
		},
		{
			name: "request before elapsed",
			got: requestBlocked(
				&advanceRequest{target: 1, baseCallbackID: 3},
				2,
				nil,
				nil,
			),
			want: false,
		},
		{
			name: "request after elapsed",
			got: requestBlocked(
				&advanceRequest{target: 3, baseCallbackID: 3},
				2,
				nil,
				nil,
			),
			want: true,
		},
		{
			name: "event blocks request",
			got: requestBlocked(
				&advanceRequest{target: 2, baseCallbackID: 3},
				2,
				&scheduledEvent{deadline: 2},
				nil,
			),
			want: true,
		},
		{
			name: "prior callback does not block",
			got: requestBlocked(
				&advanceRequest{target: 2, baseCallbackID: 3},
				2,
				nil,
				map[uint64]struct{}{2: {}, 3: {}},
			),
			want: false,
		},
		{
			name: "later callback blocks",
			got: requestBlocked(
				&advanceRequest{target: 2, baseCallbackID: 3},
				2,
				nil,
				map[uint64]struct{}{4: {}},
			),
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("predicate = %v, want %v", test.got, test.want)
			}
		})
	}
}

func TestMutationContractForStrictEventHeapOrder(t *testing.T) {
	events := eventHeap{
		{deadline: time.Nanosecond, sequence: 1},
		{deadline: 2 * time.Nanosecond, sequence: 1},
		{deadline: time.Nanosecond, sequence: 2},
		{deadline: time.Nanosecond, sequence: 1},
	}

	tests := []struct {
		left  int
		right int
		want  bool
	}{
		{left: 0, right: 1, want: true},
		{left: 1, right: 0, want: false},
		{left: 0, right: 2, want: true},
		{left: 2, right: 0, want: false},
		{left: 0, right: 3, want: false},
		{left: 3, right: 0, want: false},
	}
	for _, test := range tests {
		if got := events.Less(test.left, test.right); got != test.want {
			t.Fatalf("Less(%d, %d) = %v, want %v", test.left, test.right, got, test.want)
		}
	}
}
