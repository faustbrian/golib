package kafka

import (
	"cmp"
	"slices"
	"sync"
)

type consumerAssignmentToken struct {
	epoch    uint64
	observed bool
}

type consumerAssignmentTransition struct {
	partitionCount int
	truncated      bool
	err            error
	category       ErrorCategory
}

type consumerAssignmentState struct {
	mu            sync.RWMutex
	epoch         uint64
	maximum       int
	subscriptions map[string]struct{}
	partitions    map[TopicPartition]struct{}
	observed      bool
	lostState     bool
	err           error
}

func newConsumerAssignmentState(
	maximum int,
	topics []string,
) *consumerAssignmentState {
	subscriptions := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		subscriptions[topic] = struct{}{}
	}

	return &consumerAssignmentState{
		maximum:       maximum,
		subscriptions: subscriptions,
		partitions:    make(map[TopicPartition]struct{}),
	}
}

func (state *consumerAssignmentState) assigned(
	assigned map[string][]int32,
) consumerAssignmentTransition {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.advance(false)
	if state.err != nil {
		return consumerAssignmentTransition{err: state.err}
	}
	additions, err := state.validated(assigned, true)
	if err != nil {
		state.fail(err)

		return state.failedTransition(err)
	}
	for _, partition := range additions {
		state.partitions[partition] = struct{}{}
	}

	return consumerAssignmentTransition{partitionCount: len(additions)}
}

func (state *consumerAssignmentState) revoked(
	revoked map[string][]int32,
) consumerAssignmentTransition {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.advance(false)
	if state.err != nil {
		return consumerAssignmentTransition{err: state.err}
	}
	partitions, err := state.validated(revoked, false)
	if err != nil {
		state.fail(err)

		return state.failedTransition(err)
	}
	for _, partition := range partitions {
		delete(state.partitions, partition)
	}

	return consumerAssignmentTransition{partitionCount: len(partitions)}
}

func (state *consumerAssignmentState) lost() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.advance(true)
	state.err = nil
	clear(state.partitions)
}

func (state *consumerAssignmentState) advance(lost bool) {
	state.epoch++
	state.observed = true
	state.lostState = lost
}

func (state *consumerAssignmentState) validated(
	partitions map[string][]int32,
	adding bool,
) ([]TopicPartition, error) {
	counted := make([]struct{}, 0)
	for _, topicPartitions := range partitions {
		for range topicPartitions {
			if len(counted) == state.maximum {
				return nil, ErrTooManyAssignedPartitions
			}
			counted = append(counted, struct{}{})
		}
	}

	validated := make([]TopicPartition, 0, len(counted))
	seen := make(map[TopicPartition]struct{}, len(counted))
	additional := 0
	for topic, topicPartitions := range partitions {
		if _, subscribed := state.subscriptions[topic]; !subscribed {
			return nil, ErrInvalidAssignment
		}
		for _, partitionID := range topicPartitions {
			partition := TopicPartition{Topic: topic, Partition: partitionID}
			if partitionID < 0 {
				return nil, ErrInvalidAssignment
			}
			if _, duplicate := seen[partition]; duplicate {
				return nil, ErrInvalidAssignment
			}
			seen[partition] = struct{}{}
			validated = append(validated, partition)
			if adding {
				if _, exists := state.partitions[partition]; !exists {
					additional++
				}
			}
		}
	}
	if adding {
		if len(state.partitions)+additional > state.maximum {
			return nil, ErrTooManyAssignedPartitions
		}
	}

	return validated, nil
}

func (state *consumerAssignmentState) fail(err error) {
	state.err = err
	clear(state.partitions)
}

func (state *consumerAssignmentState) failedTransition(
	err error,
) consumerAssignmentTransition {
	if err == ErrTooManyAssignedPartitions {
		return consumerAssignmentTransition{
			partitionCount: state.maximum,
			truncated:      true,
			err:            err,
		}
	}

	return consumerAssignmentTransition{err: err}
}

func (state *consumerAssignmentState) token() (consumerAssignmentToken, error) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	return consumerAssignmentToken{
		epoch:    state.epoch,
		observed: state.observed,
	}, state.err
}

func (state *consumerAssignmentState) owns(
	token consumerAssignmentToken,
	partition TopicPartition,
) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()

	if !token.observed {
		if state.observed {
			return false
		}

		return state.err == nil
	}
	if state.err != nil || state.epoch != token.epoch {
		return false
	}
	_, owned := state.partitions[partition]

	return owned
}

func (state *consumerAssignmentState) validate(
	token consumerAssignmentToken,
) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	if token.observed != state.observed {
		return ErrConsumerOwnershipLost
	}
	if state.err != nil {
		return ErrConsumerOwnershipLost
	}
	if token.observed {
		if token.epoch != state.epoch {
			return ErrConsumerOwnershipLost
		}
	}

	return nil
}

func (state *consumerAssignmentState) snapshot() (ConsumerAssignment, error) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	var partitions []TopicPartition
	if len(state.partitions) > 0 {
		partitions = make([]TopicPartition, 0, len(state.partitions))
	}
	for partition := range state.partitions {
		partitions = append(partitions, partition)
	}
	slices.SortFunc(partitions, func(left, right TopicPartition) int {
		if topicOrder := cmp.Compare(left.Topic, right.Topic); topicOrder != 0 {
			return topicOrder
		}

		return cmp.Compare(left.Partition, right.Partition)
	})

	return ConsumerAssignment{
		Epoch:      state.epoch,
		Partitions: partitions,
		Lost:       state.lostState,
	}, state.err
}
