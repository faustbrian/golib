//go:build interoperability

package kafka_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestValidateTestcontainersRyukImageID(t *testing.T) {
	t.Parallel()

	if err := validateTestcontainersRyukImageID(
		"runtime",
		"sha256:pinned",
		"sha256:pinned",
	); err != nil {
		t.Fatalf("validate matching Ryuk image IDs: %v", err)
	}
}

func TestValidateTestcontainersRyukImageIDRejectsMismatch(t *testing.T) {
	t.Parallel()

	err := validateTestcontainersRyukImageID(
		"runtime",
		"sha256:pinned",
		"sha256:unexpected",
	)
	if err == nil {
		t.Fatal("unexpected Ryuk image ID was accepted")
	}
}

func TestValidateTestcontainersRyukRequest(t *testing.T) {
	t.Parallel()

	if err := validateTestcontainersRyukRequest(testcontainers.ContainerRequest{
		Image: testcontainersRyukMutableImage,
	}); err != nil {
		t.Fatalf("validate reviewed Ryuk request: %v", err)
	}
}

func TestValidateTestcontainersRyukRequestRejectsUpstreamChange(t *testing.T) {
	t.Parallel()

	err := validateTestcontainersRyukRequest(testcontainers.ContainerRequest{
		Image: "testcontainers/ryuk:unexpected",
	})
	if err == nil {
		t.Fatal("unreviewed upstream Ryuk image was accepted")
	}
}

func TestValidateTestcontainersRyukRegistryPrefix(t *testing.T) {
	t.Parallel()

	if err := validateTestcontainersRyukRegistryPrefix(""); err != nil {
		t.Fatalf("validate empty Ryuk registry prefix: %v", err)
	}
}

func TestValidateTestcontainersRyukRegistryPrefixRejectsSubstitution(t *testing.T) {
	t.Parallel()

	if err := validateTestcontainersRyukRegistryPrefix("registry.example"); err == nil {
		t.Fatal("Ryuk registry substitution was accepted")
	}
}

func TestSignalTestcontainersReaperTermination(t *testing.T) {
	t.Parallel()

	termination := make(chan bool, 1)

	if err := signalTestcontainersReaperTermination(termination, time.Second); err != nil {
		t.Fatalf("signal Testcontainers reaper termination: %v", err)
	}
	if signal := <-termination; !signal {
		t.Fatal("Testcontainers reaper received a false termination signal")
	}
}

func TestSignalTestcontainersReaperTerminationTimesOut(t *testing.T) {
	t.Parallel()

	err := signalTestcontainersReaperTermination(make(chan bool), time.Nanosecond)
	if err == nil {
		t.Fatal("blocked Testcontainers reaper termination signal did not time out")
	}
}
