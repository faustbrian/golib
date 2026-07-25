package safeerr

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrapRedactsCauseTextAndPreservesIdentity(t *testing.T) {
	cause := errors.New("credential=audit-secret")
	err := Wrap("connection failed", cause)

	require.EqualError(t, err, "connection failed")
	require.NotContains(t, err.Error(), "audit-secret")
	require.ErrorIs(t, err, cause)
}
