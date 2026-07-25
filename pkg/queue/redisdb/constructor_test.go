package redisdb

import (
	"testing"

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
