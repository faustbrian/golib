//go:build !linux && !darwin

package terminal

import (
	"context"
	"os"
	"time"
)

func readWithoutDeadline(context.Context, *os.File, []byte, time.Duration) (int, error) {
	return 0, os.ErrNoDeadline
}
