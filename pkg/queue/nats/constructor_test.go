package nats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWorkerEReturnsConnectionError(t *testing.T) {
	worker, err := NewWorkerE(WithAddr(
		"nats://audit-user:audit-secret@localhost:%zz",
	))

	require.Nil(t, worker)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "audit-secret")
}
