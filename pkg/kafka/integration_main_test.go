//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	dockerclient "github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

const (
	testcontainersRyukReconnectionTimeout = "TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT"
	testcontainersRyukMutableImage        = "testcontainers/ryuk:0.14.0"
	testcontainersRyukPinnedImage         = "testcontainers/ryuk@" +
		"sha256:7c1a8a9a47c780ed0f983770a662f80deb115d95cce3e2daa3d12115b8cd28f0"
)

type reviewedTestcontainersReaperProvider struct {
	testcontainers.ReaperProvider
}

func (provider reviewedTestcontainersReaperProvider) RunContainer(
	ctx context.Context,
	request testcontainers.ContainerRequest,
) (testcontainers.Container, error) {
	if err := validateTestcontainersRyukRequest(request); err != nil {
		return nil, err
	}

	return provider.ReaperProvider.RunContainer(ctx, request)
}

func validateTestcontainersRyukRequest(
	request testcontainers.ContainerRequest,
) error {
	if request.Image != testcontainersRyukMutableImage {
		return errors.New(
			"unreviewed Testcontainers reaper image differs from the reviewed upstream tag",
		)
	}

	return nil
}

func validateTestcontainersRyukRegistryPrefix(prefix string) error {
	if prefix != "" {
		return errors.New(
			"configured Testcontainers reaper registry substitution bypasses the reviewed digest",
		)
	}

	return nil
}

func validateTestcontainersRyukImageID(
	boundary string,
	pinnedID string,
	observedID string,
) error {
	if observedID != pinnedID {
		return fmt.Errorf(
			"Testcontainers reaper %s image ID %q does not match pinned image ID %q",
			boundary,
			observedID,
			pinnedID,
		)
	}

	return nil
}

func signalTestcontainersReaperTermination(
	termination chan<- bool,
	timeout time.Duration,
) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case termination <- true:
		return nil
	case <-timer.C:
		return errors.New("pinned Testcontainers reaper shutdown timed out")
	}
}

func startPinnedTestcontainersReaper() (func() error, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		apacheKafkaCleanupTimeout,
	)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return nil, fmt.Errorf("create Testcontainers Docker provider: %w", err)
	}

	closeProvider := func(cause error) error {
		return errors.Join(cause, provider.Close())
	}
	if err := validateTestcontainersRyukRegistryPrefix(
		provider.Config().Config.HubImageNamePrefix,
	); err != nil {
		return nil, closeProvider(err)
	}
	pinnedImage, err := provider.Client().ImageInspect(
		ctx,
		testcontainersRyukPinnedImage,
	)
	if errdefs.IsNotFound(err) {
		stream, pullErr := provider.Client().ImagePull(
			ctx,
			testcontainersRyukPinnedImage,
			dockerclient.ImagePullOptions{},
		)
		if pullErr != nil {
			return nil, closeProvider(fmt.Errorf(
				"pull pinned Testcontainers reaper: %w",
				pullErr,
			))
		}
		_, copyErr := io.Copy(io.Discard, stream)
		closeErr := stream.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, closeProvider(fmt.Errorf(
				"read pinned Testcontainers reaper pull: %w",
				err,
			))
		}
		pinnedImage, err = provider.Client().ImageInspect(
			ctx,
			testcontainersRyukPinnedImage,
		)
	}
	if err != nil {
		return nil, closeProvider(fmt.Errorf(
			"inspect pinned Testcontainers reaper image: %w",
			err,
		))
	}
	if _, err := provider.Client().ImageTag(
		ctx,
		dockerclient.ImageTagOptions{
			Source: pinnedImage.ID,
			Target: testcontainersRyukMutableImage,
		},
	); err != nil {
		return nil, closeProvider(fmt.Errorf(
			"tag pinned Testcontainers reaper image: %w",
			err,
		))
	}
	mutableImage, err := provider.Client().ImageInspect(
		ctx,
		testcontainersRyukMutableImage,
	)
	if err != nil {
		return nil, closeProvider(fmt.Errorf(
			"inspect Testcontainers reaper tag: %w",
			err,
		))
	}
	if err := validateTestcontainersRyukImageID(
		"tag",
		pinnedImage.ID,
		mutableImage.ID,
	); err != nil {
		return nil, closeProvider(err)
	}

	sessionID := testcontainers.SessionID()
	reaper, err := testcontainers.NewReaper(
		ctx,
		sessionID,
		reviewedTestcontainersReaperProvider{ReaperProvider: provider},
		"",
	)
	if err != nil {
		return nil, closeProvider(fmt.Errorf(
			"start pinned Testcontainers reaper: %w",
			err,
		))
	}

	termination, err := reaper.Connect()
	if err != nil {
		return nil, closeProvider(fmt.Errorf(
			"retain pinned Testcontainers reaper: %w",
			err,
		))
	}
	stopReaper := func(cause error) error {
		return closeProvider(errors.Join(
			cause,
			signalTestcontainersReaperTermination(
				termination,
				apacheKafkaCleanupTimeout,
			),
		))
	}

	containerState, err := provider.Client().ContainerInspect(
		ctx,
		"reaper_"+sessionID,
		dockerclient.ContainerInspectOptions{},
	)
	if err != nil {
		return nil, stopReaper(fmt.Errorf(
			"inspect pinned Testcontainers reaper: %w",
			err,
		))
	}
	if err := validateTestcontainersRyukImageID(
		"runtime",
		pinnedImage.ID,
		containerState.Container.Image,
	); err != nil {
		return nil, stopReaper(err)
	}

	return func() error {
		return stopReaper(nil)
	}, nil
}

func TestMain(m *testing.M) {
	previous, configured := os.LookupEnv(testcontainersRyukReconnectionTimeout)
	if !configured {
		// Keep Ryuk alive while bounded parallel broker fixtures hand ownership
		// to the next fixture in the same test process.
		if err := os.Setenv(
			testcontainersRyukReconnectionTimeout,
			apacheKafkaCleanupTimeout.String(),
		); err != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"configure Testcontainers reaper reconnection timeout: %v\n",
				err,
			)
			os.Exit(1)
		}
	}

	stopReaper, err := startPinnedTestcontainersReaper()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	status := m.Run()
	if err := stopReaper(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "stop Testcontainers reaper: %v\n", err)
		status = 1
	}
	if configured {
		_ = os.Setenv(testcontainersRyukReconnectionTimeout, previous)
	} else {
		_ = os.Unsetenv(testcontainersRyukReconnectionTimeout)
	}
	os.Exit(status)
}
