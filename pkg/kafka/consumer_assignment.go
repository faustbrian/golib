package kafka

import (
	"sort"
	"sync"
)

type consumerAssignmentToken struct {
	epoch    uint64
	observed bool
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
) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.advance(false)
	if state.err != nil {
		return
	}
	additions, err := state.validated(assigned, true)
	if err != nil {
		state.fail(err)

		return
	}
	for _, partition := range additions {
		state.partitions[partition] = struct{}{}
	}
}

func (state *consumerAssignmentState) revoked(
	revoked map[string][]int32,
) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.advance(false)
	if state.err != nil {
		return
	}
	partitions, err := state.validated(revoked, false)
	if err != nil {
		state.fail(err)

		return
	}
	for _, partition := range partitions {
		delete(state.partitions, partition)
	}
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
	count := 0
	for _, topicPartitions := range partitions {
		if len(topicPartitions) > state.maximum-count {
			return nil, ErrTooManyAssignedPartitions
		}
		count += len(topicPartitions)
	}

	validated := make([]TopicPartition, 0, count)
	seen := make(map[TopicPartition]struct{}, count)
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
			if _, exists := state.partitions[partition]; adding && !exists {
				additional++
			}
		}
	}
	if adding && len(state.partitions)+additional > state.maximum {
		return nil, ErrTooManyAssignedPartitions
	}

	return validated, nil
}

func (state *consumerAssignmentState) fail(err error) {
	state.err = err
	clear(state.partitions)
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
		return !state.observed && state.err == nil
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

	if token.observed != state.observed ||
		state.err != nil ||
		(token.observed && token.epoch != state.epoch) {
		return ErrConsumerOwnershipLost
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
	sort.Slice(partitions, func(left, right int) bool {
		if partitions[left].Topic == partitions[right].Topic {
			return partitions[left].Partition < partitions[right].Partition
		}

		return partitions[left].Topic < partitions[right].Topic
	})

	return ConsumerAssignment{
		Epoch:      state.epoch,
		Partitions: partitions,
		Lost:       state.lostState,
	}, state.err
}
