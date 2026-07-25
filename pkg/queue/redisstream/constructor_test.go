package redisdb

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/require"
)

func TestNewWorkerEReturnsInvalidConnectionString(t *testing.T) {
	worker, err := NewWorkerE(WithConnectionString(
		"redis://audit-user:audit-secret@localhost:%zz",
	))

	require.Nil(t, worker)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "audit-secret")
}

func TestNewWorkerERequiresRedisAddress(t *testing.T) {
	worker, err := NewWorkerE()

	require.Nil(t, worker)
	require.ErrorContains(t, err, "address")
}

func TestNewWorkerEInitializesNativeManagementStatus(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		}),
	)
	require.NoError(t, err)
	status, err := worker.ObserveWorker(t.Context())
	require.NoError(t, err)
	require.Equal(t, "worker-1", status.ID)
	require.NoError(t, worker.Shutdown())
}
