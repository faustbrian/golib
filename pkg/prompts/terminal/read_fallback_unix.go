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
	if !validPollDescriptor(descriptor) {
		return 0, os.ErrClosed
	}
	info, err := stat()
	if err != nil {
		return 0, err
	}
	if info.Mode() & (os.ModeCharDevice | os.ModeNamedPipe | os.ModeSocket) == 0 {
		return 0, os.ErrNoDeadline
	}
	// #nosec G115 -- validPollDescriptor bounds descriptor to MaxInt32 above.
	fds := []unix.PollFd{{Fd: int32(descriptor), Events: unix.POLLIN}}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		wait := boundedPollWait(ctx, pollInterval, time.Now())
		milliseconds := pollMilliseconds(wait)
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
		if invalidPollEvents(events) {
			return 0, os.ErrClosed
		}
		if readablePollEvents(events) {
			return input.Read(buffer)
		}
	}
}

func validPollDescriptor(descriptor uintptr) bool {
	return descriptor != ^uintptr(0) && descriptor <= math.MaxInt32
}

func pollMilliseconds(wait time.Duration) int {
	return max(1, int((wait + time.Millisecond - 1) / time.Millisecond))
}

func boundedPollWait(ctx context.Context, pollInterval time.Duration, now time.Time) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return min(pollInterval, deadline.Sub(now))
	}

	return pollInterval
}

func invalidPollEvents(events int16) bool {
	return events & unix.POLLNVAL != 0
}

func readablePollEvents(events int16) bool {
	return events & (unix.POLLIN | unix.POLLHUP | unix.POLLERR) != 0
}
