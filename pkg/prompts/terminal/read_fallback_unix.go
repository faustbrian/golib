//go:build linux || darwin

package terminal

import (
	"context"
	"errors"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func readWithoutDeadline(
	ctx context.Context,
	input *os.File,
	buffer []byte,
	pollInterval time.Duration,
) (int, error) {
	return readWithoutDeadlineUsing(ctx, input, buffer, pollInterval, input.Stat, unix.Poll)
}

func readWithoutDeadlineUsing(
	ctx context.Context,
	input *os.File,
	buffer []byte,
	pollInterval time.Duration,
	stat func() (os.FileInfo, error),
	poll func([]unix.PollFd, int) (int, error),
) (int, error) {
	descriptor := input.Fd()
	if descriptor == ^uintptr(0) || descriptor > math.MaxInt32 {
		return 0, os.ErrClosed
	}
	info, err := stat()
	if err != nil {
		return 0, err
	}
	if info.Mode()&(os.ModeCharDevice|os.ModeNamedPipe|os.ModeSocket) == 0 {
		return 0, os.ErrNoDeadline
	}
	fds := []unix.PollFd{{Fd: int32(descriptor), Events: unix.POLLIN}}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		wait := pollInterval
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < wait {
				wait = remaining
			}
		}
		milliseconds := max(1, int((wait+time.Millisecond-1)/time.Millisecond))
		fds[0].Revents = 0
		ready, err := poll(fds, milliseconds)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready == 0 {
			return 0, os.ErrDeadlineExceeded
		}
		events := fds[0].Revents
		if events&unix.POLLNVAL != 0 {
			return 0, os.ErrClosed
		}
		if events&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			return input.Read(buffer)
		}
	}
}
