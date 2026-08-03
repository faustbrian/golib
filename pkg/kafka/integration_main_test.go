//go:build integration

package kafka_test

import (
	"fmt"
	"os"
	"testing"
)

const testcontainersRyukReconnectionTimeout = "TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT"

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

	status := m.Run()
	if configured {
		_ = os.Setenv(testcontainersRyukReconnectionTimeout, previous)
	} else {
		_ = os.Unsetenv(testcontainersRyukReconnectionTimeout)
	}
	os.Exit(status)
}
