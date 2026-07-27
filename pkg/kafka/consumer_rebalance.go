package kafka

import (
	"context"
	"sync"
	"time"
)

type consumerRebalanceState struct {
	mu             sync.Mutex
	policy         RebalanceHandlerPolicy
	active         bool
	pending        bool
	handlerID      uint64
	handlerCancels map[uint64]context.CancelCauseFunc
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
	clear(state.handlerCancels)
}

func (state *consumerRebalanceState) blocked() bool {
	state.mu.Lock()
	if !state.active {
		state.mu.Unlock()

		return false
	}
	state.pending = true
	cancels := make([]context.CancelCauseFunc, 0, len(state.handlerCancels))
	if state.policy == RebalanceCancelHandler {
		for _, cancel := range state.handlerCancels {
			cancels = append(cancels, cancel)
		}
	}
	state.mu.Unlock()

	for _, cancel := range cancels {
		cancel(ErrConsumerRebalance)
	}

	return true
}

func (state *consumerRebalanceState) handlerContext(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, func() error, bool) {
	state.mu.Lock()
	if state.active && state.pending {
		state.mu.Unlock()

		return nil, nil, false
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	handlerCtx, handlerCancel := context.WithCancelCause(timeoutCtx)
	handlerID := state.nextHandlerID()
	if state.handlerCancels == nil {
		state.handlerCancels = make(map[uint64]context.CancelCauseFunc)
	}
	state.handlerCancels[handlerID] = handlerCancel
	state.mu.Unlock()

	return handlerCtx, func() error {
		state.mu.Lock()
		if state.active &&
			state.pending &&
			state.policy == RebalanceCancelHandler {
			handlerCancel(ErrConsumerRebalance)
		}
		delete(state.handlerCancels, handlerID)
		cause := context.Cause(handlerCtx)
		state.mu.Unlock()
		handlerCancel(nil)
		timeoutCancel()

		return cause
	}, true
}

func (state *consumerRebalanceState) nextHandlerID() uint64 {
	for {
		state.handlerID++
		if state.handlerID == 0 {
			continue
		}
		if _, active := state.handlerCancels[state.handlerID]; !active {
			return state.handlerID
		}
	}
}
