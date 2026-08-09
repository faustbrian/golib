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
	observeWait    bool
	pollDone       chan time.Time
	waitDone       chan struct{}
	handlerID      uint64
	handlerCancels map[uint64]context.CancelCauseFunc
}

func newConsumerRebalanceState(
	policy RebalanceHandlerPolicy,
) *consumerRebalanceState {
	return &consumerRebalanceState{
		policy:         policy,
		handlerCancels: make(map[uint64]context.CancelCauseFunc),
	}
}

func (state *consumerRebalanceState) beginPoll(observeWait bool) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.active = true
	state.pending = false
	state.observeWait = observeWait
	state.pollDone = nil
	state.waitDone = nil
}

func (state *consumerRebalanceState) endPoll() {
	state.mu.Lock()
	pollDone := state.pollDone
	waitDone := state.waitDone
	waitForObserver := state.pending && waitDone != nil
	state.active = false
	state.pending = false
	state.observeWait = false
	state.pollDone = nil
	state.waitDone = nil
	clear(state.handlerCancels)
	state.mu.Unlock()

	if pollDone != nil {
		pollDone <- time.Now()
		close(pollDone)
	}
	if waitForObserver {
		<-waitDone
	}
}

func (state *consumerRebalanceState) blockedWait() (
	<-chan time.Time,
	chan<- struct{},
	bool,
) {
	state.mu.Lock()
	if !state.active || state.pending {
		state.mu.Unlock()

		return nil, nil, false
	}
	state.pending = true
	if state.observeWait {
		state.pollDone = make(chan time.Time, 1)
		state.waitDone = make(chan struct{})
	}
	pollDone := state.pollDone
	waitDone := state.waitDone
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

	return pollDone, waitDone, true
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
			state.handlerID++
		}
		if _, active := state.handlerCancels[state.handlerID]; !active {
			return state.handlerID
		}
	}
}
