//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

type kafkaContainerCleanupStub struct {
	terminateErr error
	containerID  string
}

func (stub *kafkaContainerCleanupStub) GetContainerID() string {
	return stub.containerID
}

func (stub *kafkaContainerCleanupStub) Terminate(
	context.Context,
	...testcontainers.TerminateOption,
) error {
	return stub.terminateErr
}

func TestTerminateKafkaContainerAcceptsVerifiedRemovalAfterTimeout(t *testing.T) {
	t.Parallel()

	container := &kafkaContainerCleanupStub{
		terminateErr: context.DeadlineExceeded,
		containerID:  "removed-container",
	}
	checks := 0
	if err := terminateKafkaContainerWithVerifier(
		container,
		func(_ context.Context, containerID string) (bool, error) {
			checks++
			if containerID != "removed-container" {
				t.Fatalf("container ID = %q", containerID)
			}

			return true, nil
		},
	); err != nil {
		t.Fatalf("terminate removed Kafka container: %v", err)
	}
	if checks != 1 {
		t.Fatalf("container absence checks = %d, want 1", checks)
	}
}

func TestTerminateKafkaContainerSkipsInspectionAfterSuccessfulRemoval(t *testing.T) {
	t.Parallel()

	container := &kafkaContainerCleanupStub{containerID: "removed-container"}
	if err := terminateKafkaContainerWithVerifier(
		container,
		func(context.Context, string) (bool, error) {
			t.Fatal("successful termination inspected the container")

			return false, nil
		},
	); err != nil {
		t.Fatalf("terminate Kafka container: %v", err)
	}
}

func TestTerminateKafkaContainerRejectsUnremovedContainer(t *testing.T) {
	t.Parallel()

	container := &kafkaContainerCleanupStub{
		terminateErr: context.DeadlineExceeded,
		containerID:  "existing-container",
	}
	err := terminateKafkaContainerWithVerifier(
		container,
		func(context.Context, string) (bool, error) {
			return false, nil
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("termination error = %v, want deadline exceeded", err)
	}
}

func TestTerminateKafkaContainerPreservesInspectionFailure(t *testing.T) {
	t.Parallel()

	inspectErr := errors.New("inspect failed")
	container := &kafkaContainerCleanupStub{
		terminateErr: context.DeadlineExceeded,
		containerID:  "unknown-container",
	}
	err := terminateKafkaContainerWithVerifier(
		container,
		func(context.Context, string) (bool, error) {
			return false, inspectErr
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("termination error = %v, want deadline exceeded", err)
	}
	if !errors.Is(err, inspectErr) {
		t.Fatalf("termination error = %v, want inspection failure", err)
	}
}
