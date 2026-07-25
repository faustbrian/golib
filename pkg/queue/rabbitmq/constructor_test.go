package rabbitmq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWorkerEReturnsConnectionError(t *testing.T) {
	worker, err := NewWorkerE(
		WithAddr("amqp://audit-user:audit-secret@localhost:%zz"),
		WithReconnectConfig(ReconnectConfig{MaxRetries: 1}),
	)

	require.Nil(t, worker)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "audit-secret")
}
