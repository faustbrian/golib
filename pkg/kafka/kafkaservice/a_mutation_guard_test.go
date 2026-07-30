package kafkaservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMutationGuardStartupErrorMessages(t *testing.T) {
	if got := (&StartupError{}).Error(); got != "kafka service startup validation failed" {
		t.Fatalf("Error() = %q", got)
	}
	if got := (&StartupError{Cleanup: context.Canceled}).Error(); got != "kafka service startup validation and cleanup failed" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestMutationGuardValidNameAcceptsExactLimit(t *testing.T) {
	if !validName(strings.Repeat("n", MaxNameBytes)) {
		t.Fatal("validName rejected an identifier at MaxNameBytes")
	}
}

func TestMutationGuardProducerUseState(t *testing.T) {
	producer := &Producer[struct{}]{active: true}
	mutationGuardRequire(producer.beginUse() && producer.inflight == 1)

	drained := make(chan struct{})
	producer.stopping = true
	producer.drained = drained
	producer.finishUse()
	mutationGuardRequire(producer.inflight == 0 && producer.drained == nil)
	select {
	case <-drained:
	default:
		panic("finishUse did not close the drain signal")
	}
}

func TestMutationGuardProducerFinishUseConditions(t *testing.T) {
	tests := []struct {
		name     string
		producer *Producer[struct{}]
		wantOpen bool
	}{
		{
			name:     "not stopping",
			producer: &Producer[struct{}]{inflight: 1, drained: make(chan struct{})},
			wantOpen: true,
		},
		{
			name:     "more work remains",
			producer: &Producer[struct{}]{stopping: true, inflight: 2, drained: make(chan struct{})},
			wantOpen: true,
		},
		{
			name:     "no waiter",
			producer: &Producer[struct{}]{stopping: true, inflight: 1},
			wantOpen: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.producer.inflight
			drained := test.producer.drained
			test.producer.finishUse()
			mutationGuardRequire(test.producer.inflight == initial-1)
			if test.wantOpen {
				select {
				case <-drained:
					panic("drain signal closed early")
				default:
				}
			}
		})
	}
}

func TestMutationGuardConsumerUseState(t *testing.T) {
	consumer := &Consumer[struct{}]{active: true}
	mutationGuardRequire(consumer.beginUse() && consumer.inflight == 1)

	drained := make(chan struct{})
	consumer.stopping = true
	consumer.drained = drained
	consumer.finishUse()
	mutationGuardRequire(consumer.inflight == 0 && consumer.drained == nil)
	select {
	case <-drained:
	default:
		panic("finishUse did not close the drain signal")
	}
}

func TestMutationGuardConsumerFinishUseConditions(t *testing.T) {
	tests := []struct {
		name     string
		consumer *Consumer[struct{}]
		wantOpen bool
	}{
		{
			name:     "not stopping",
			consumer: &Consumer[struct{}]{inflight: 1, drained: make(chan struct{})},
			wantOpen: true,
		},
		{
			name:     "more work remains",
			consumer: &Consumer[struct{}]{stopping: true, inflight: 2, drained: make(chan struct{})},
			wantOpen: true,
		},
		{
			name:     "no waiter",
			consumer: &Consumer[struct{}]{stopping: true, inflight: 1},
			wantOpen: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.consumer.inflight
			drained := test.consumer.drained
			test.consumer.finishUse()
			mutationGuardRequire(test.consumer.inflight == initial-1)
			if test.wantOpen {
				select {
				case <-drained:
					panic("drain signal closed early")
				default:
				}
			}
		})
	}
}

func TestMutationGuardStopWithoutInflightWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := (&Producer[struct{}]{}).stop(ctx); err != nil {
		panic("producer stop failed")
	}
	consumer := &Consumer[struct{}]{
		shutdown: func(context.Context, struct{}) error { return nil },
	}
	if err := consumer.stop(ctx); err != nil {
		panic("consumer stop failed")
	}
}

// mutationGuardRequire terminates a mutant's test process before an invalid
// lifecycle counter can strand later concurrency tests.
func mutationGuardRequire(condition bool) {
	if !condition {
		panic("mutation guard failed")
	}
}
