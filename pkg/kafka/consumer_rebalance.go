package kafka

import (
	"context"
	"sync"
	"time"
)

type consumerRebalanceState struct {
	mu            sync.Mutex
	policy        RebalanceHandlerPolicy
	active        bool
	pending       bool
	handlerID     uint64
	handlerCancel context.CancelCauseFunc
}

func newConsumerRebalanceState(
	policy RebalanceHandlerPolicy,
) *consumerRebalanceState {
	return &consumerRebalanceState{policy: policy}
}

func (state *consumerRebalanceState) beginPoll() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.active = true
	state.pending = false
}

func (state *consumerRebalanceState) endPoll() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.active = false
	state.pending = false
	state.handlerCancel = nil
}

func (state *consumerRebalanceState) blocked() {
	state.mu.Lock()
	defer state.mu.Unlock()

	if !state.active {
		return
	}
	state.pending = true
	if state.policy == RebalanceCancelHandler && state.handlerCancel != nil {
		state.handlerCancel(ErrConsumerRebalance)
	}
}

func (state *consumerRebalanceState) isPending() bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.pending
}

func (state *consumerRebalanceState) handlerContext(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, func()) {
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	handlerCtx, handlerCancel := context.WithCancelCause(timeoutCtx)

	state.mu.Lock()
	state.handlerID++
	handlerID := state.handlerID
	state.handlerCancel = handlerCancel
	if state.active && state.pending && state.policy == RebalanceCancelHandler {
		handlerCancel(ErrConsumerRebalance)
	}
	state.mu.Unlock()

	return handlerCtx, func() {
		state.mu.Lock()
		if state.handlerID == handlerID {
			state.handlerCancel = nil
		}
		state.mu.Unlock()
		handlerCancel(nil)
		timeoutCancel()
	}
}
