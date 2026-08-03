//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

const (
	kafkaCleanupTimeout      = time.Minute
	kafkaCleanupCheckTimeout = 10 * time.Second
)

func cleanupKafkaContainer(
	t *testing.T,
	container kafkaContainerTerminator,
) {
	t.Helper()
	if container == nil {
		return
	}

	t.Cleanup(func() {
		if err := terminateKafkaContainer(container); err != nil {
			t.Errorf("terminate Kafka container: %v", err)
		}
	})
}

type kafkaContainerTerminator interface {
	GetContainerID() string
	Terminate(context.Context, ...testcontainers.TerminateOption) error
}

type kafkaContainerAbsenceVerifier func(context.Context, string) (bool, error)

func terminateKafkaContainer(container kafkaContainerTerminator) error {
	return terminateKafkaContainerWithVerifier(
		container,
		verifyKafkaContainerAbsent,
	)
}

func terminateKafkaContainerWithVerifier(
	container kafkaContainerTerminator,
	verifyAbsent kafkaContainerAbsenceVerifier,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), kafkaCleanupTimeout)
	defer cancel()
	terminateErr := container.Terminate(ctx, testcontainers.StopTimeout(0))
	if terminateErr == nil {
		return nil
	}

	checkCtx, checkCancel := context.WithTimeout(
		context.Background(),
		kafkaCleanupCheckTimeout,
	)
	defer checkCancel()
	absent, checkErr := verifyAbsent(checkCtx, container.GetContainerID())
	// Docker can finish the forced removal immediately after the request's
	// deadline. Accept that error only when a fresh client proves absence.
	if absent && checkErr == nil {
		return nil
	}
	if checkErr != nil {
		return errors.Join(
			terminateErr,
			fmt.Errorf("verify Kafka container cleanup: %w", checkErr),
		)
	}

	return terminateErr
}

func verifyKafkaContainerAbsent(
	ctx context.Context,
	containerID string,
) (bool, error) {
	dockerClient, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return false, fmt.Errorf("connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	_, err = dockerClient.ContainerInspect(
		ctx,
		containerID,
		client.ContainerInspectOptions{},
	)
	if errdefs.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect container: %w", err)
	}

	return false, nil
}
