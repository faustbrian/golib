package kafka

import (
	"context"
	"testing"
	"time"
)

func TestConsumerCriticalGuardsTerminateDeterministically(t *testing.T) {
	runConsumerCriticalGuards(t)
}

func TestConsumerRebalanceCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("initializes the handler cancellation registry", func(t *testing.T) {
		state := newConsumerRebalanceState(RebalanceCancelHandler)
		handlerCtx, finish, admitted := state.handlerContext(
			context.Background(),
			time.Second,
		)
		if !admitted || handlerCtx == nil || finish == nil {
			t.Fatal("handler context was not admitted")
		}
		if len(state.handlerCancels) != 1 || state.handlerCancels[1] == nil {
			t.Fatalf("handler ID/cancels = %d/%#v", state.handlerID, state.handlerCancels)
		}
		if cause := finish(); cause != nil {
			t.Fatalf("handler cleanup cause = %v", cause)
		}
	})

	t.Run("skips zero and active IDs after wraparound", func(t *testing.T) {
		state := newConsumerRebalanceState(RebalanceCancelHandler)
		state.handlerID = ^uint64(0)
		state.handlerCancels = map[uint64]context.CancelCauseFunc{
			1: func(error) {},
		}
		handlerCtx, finish, admitted := state.handlerContext(
			context.Background(),
			time.Second,
		)
		if !admitted || handlerCtx == nil || finish == nil {
			t.Fatal("handler context was not admitted")
		}
		if state.handlerID != 2 ||
			len(state.handlerCancels) != 2 ||
			state.handlerCancels[2] == nil {
			t.Fatalf("handler ID/cancels = %d/%#v", state.handlerID, state.handlerCancels)
		}
		if cause := finish(); cause != nil {
			t.Fatalf("handler cleanup cause = %v", cause)
		}
	})
}
