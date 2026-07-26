package nsq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWorkerERequiresAddress(t *testing.T) {
	worker, err := NewWorkerE(WithAddr(""))

	require.Nil(t, worker)
	require.ErrorContains(t, err, "address")
}
