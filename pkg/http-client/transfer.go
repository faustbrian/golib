package httpclient

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"time"
)

var (
	// ErrInvalidTransfer indicates invalid streaming transfer policy or state.
	ErrInvalidTransfer = errors.New("invalid HTTP transfer policy")
	// ErrTransferLimit indicates that a finite transfer bound was exceeded.
	ErrTransferLimit = errors.New("HTTP transfer limit reached")
	// ErrTransferLength indicates transferred length mismatch.
	ErrTransferLength = errors.New("HTTP transfer length mismatch")
	// ErrDigestMismatch indicates transferred content failed validation.
	ErrDigestMismatch = errors.New("HTTP transfer digest mismatch")
	// ErrTransferProgressPanic indicates that a progress observer panicked.
	ErrTransferProgressPanic = errors.New("HTTP transfer progress observer panicked")
)

const (
	defaultMaximumTransferBytes = 1 << 30
	maximumTransferBytes        = 1 << 40
	defaultProgressInterval     = 100 * time.Millisecond
	defaultProgressBytes        = 64 << 10
	transferBufferBytes         = 32 << 10
)

// DigestAlgorithm identifies an explicitly supported transfer digest.
type DigestAlgorithm string

const (
	// DigestSHA256 computes SHA-256.
	DigestSHA256 DigestAlgorithm = "sha-256"
	// DigestSHA512 computes SHA-512.
	DigestSHA512 DigestAlgorithm = "sha-512"
)

// TransferClock supplies deterministic progress and elapsed time.
type TransferClock interface{ Now() time.Time }

// TransferProgress is an immutable progress snapshot.
type TransferProgress struct {
	Bytes    int64
	Total    int64
	Elapsed  time.Duration
	Digest   []byte
	Complete bool
}

// TransferProgressObserver receives bounded-frequency callbacks. It must not
// retain or mutate shared request state; Digest is an independent snapshot.
type TransferProgressObserver func(context.Context, TransferProgress) error

// TransferOptions configures a bounded response-to-writer transfer.
type TransferOptions struct {
	MaximumBytes     int64
	ExpectedBytes    int64
	DigestAlgorithm  DigestAlgorithm
	ExpectedDigest   []byte
	Progress         TransferProgressObserver
	ProgressInterval time.Duration
	ProgressBytes    int64
	Clock            TransferClock
}

// TransferResult describes one completed transfer.
type TransferResult struct {
	Bytes   int64
	Elapsed time.Duration
	Digest  []byte
}

// TransferError reports streaming I/O or observer failure without rendering
// its cause, which may contain destination or response data.
type TransferError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *TransferError) Error() string { return "HTTP transfer " + err.Operation + " failed" }

// Unwrap returns the streaming failure.
func (err *TransferError) Unwrap() error { return err.Cause }

// TransferLimitError reports the finite byte bound.
type TransferLimitError struct {
	MaximumBytes int64
	Bytes        int64
}

// Error implements error.
func (*TransferLimitError) Error() string { return "HTTP transfer limit reached" }

// Unwrap returns the stable transfer limit sentinel.
func (*TransferLimitError) Unwrap() error { return ErrTransferLimit }

// TransferLengthError reports expected and observed byte counts.
type TransferLengthError struct {
	Expected int64
	Actual   int64
}

// Error implements error.
func (*TransferLengthError) Error() string { return "HTTP transfer length mismatch" }

// Unwrap returns the stable transfer length sentinel.
func (*TransferLengthError) Unwrap() error { return ErrTransferLength }

// DigestMismatchError reports the algorithm without rendering digest values.
type DigestMismatchError struct{ Algorithm DigestAlgorithm }

// Error implements error.
func (*DigestMismatchError) Error() string { return "HTTP transfer digest mismatch" }

// Unwrap returns the stable digest sentinel.
func (*DigestMismatchError) Unwrap() error { return ErrDigestMismatch }

type resolvedTransferOptions struct {
	maximum          int64
	expected         int64
	digest           hash.Hash
	expectedDigest   []byte
	progress         TransferProgressObserver
	progressInterval time.Duration
	progressBytes    int64
	clock            TransferClock
}

// CopyResponse streams response body into destination, validates configured
// bounds and digest, and always closes the response body. Destination remains
// caller-owned and is never closed.
func CopyResponse(
	ctx context.Context,
	response *http.Response,
	destination io.Writer,
	options TransferOptions,
) (result TransferResult, resultErr error) {
	if ctx == nil || response == nil || response.Body == nil || nilLike(destination) {
		return result, fmt.Errorf("%w: context, response, body, or destination is invalid", ErrInvalidTransfer)
	}
	body := response.Body
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, &TransferError{Operation: "body close", Cause: closeErr})
		}
	}()
	policy, err := resolveTransferOptions(response, options)
	if err != nil {
		return result, err
	}
	started := policy.clock.Now()
	lastProgressAt := started
	lastProgressBytes := int64(0)
	progress := func(complete bool) error {
		if policy.progress == nil {
			return nil
		}
		now := policy.clock.Now()
		update := TransferProgress{
			Bytes: result.Bytes, Total: policy.expected,
			Elapsed: max(now.Sub(started), 0), Complete: complete,
		}
		if policy.digest != nil {
			update.Digest = append([]byte(nil), policy.digest.Sum(nil)...)
		}
		if err := observeTransferProgress(ctx, policy.progress, update); err != nil {
			return &TransferError{Operation: "progress observation", Cause: err}
		}
		lastProgressAt, lastProgressBytes = now, result.Bytes
		return nil
	}
	if err := progress(false); err != nil {
		return result, err
	}
	buffer := make([]byte, transferBufferBytes)
	for {
		if err := ctx.Err(); err != nil {
			return result, &TransferError{Operation: "cancellation", Cause: err}
		}
		if result.Bytes == policy.maximum {
			var probe [1]byte
			count, readErr := body.Read(probe[:])
			if count > 0 {
				return result, &TransferLimitError{MaximumBytes: policy.maximum, Bytes: result.Bytes + int64(count)}
			}
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return result, &TransferError{Operation: "body read", Cause: readErr}
			}
			break
		}
		readBuffer := buffer
		if remaining := policy.maximum - result.Bytes; int64(len(readBuffer)) > remaining {
			readBuffer = readBuffer[:remaining]
		}
		count, readErr := body.Read(readBuffer)
		if count > 0 {
			written, writeErr := destination.Write(readBuffer[:count])
			if written > 0 {
				if policy.digest != nil {
					_, _ = policy.digest.Write(readBuffer[:written])
				}
				result.Bytes += int64(written)
			}
			if writeErr != nil || written != count {
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				return result, &TransferError{Operation: "destination write", Cause: writeErr}
			}
			now := policy.clock.Now()
			if policy.progress != nil && result.Bytes != policy.expected &&
				result.Bytes-lastProgressBytes >= policy.progressBytes &&
				now.Sub(lastProgressAt) >= policy.progressInterval {
				if err := progress(false); err != nil {
					return result, err
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return result, &TransferError{Operation: "body read", Cause: readErr}
			}
			break
		}
	}
	result.Elapsed = max(policy.clock.Now().Sub(started), 0)
	if policy.digest != nil {
		result.Digest = append([]byte(nil), policy.digest.Sum(nil)...)
	}
	if policy.expected >= 0 && result.Bytes != policy.expected {
		return result, &TransferLengthError{Expected: policy.expected, Actual: result.Bytes}
	}
	if len(policy.expectedDigest) > 0 && !hmac.Equal(result.Digest, policy.expectedDigest) {
		return result, &DigestMismatchError{Algorithm: options.DigestAlgorithm}
	}
	if err := progress(true); err != nil {
		return result, err
	}

	return result, nil
}

func observeTransferProgress(
	ctx context.Context,
	observer TransferProgressObserver,
	update TransferProgress,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrTransferProgressPanic
		}
	}()

	return observer(ctx, update)
}

func resolveTransferOptions(response *http.Response, options TransferOptions) (resolvedTransferOptions, error) {
	maximum := options.MaximumBytes
	if maximum == 0 {
		maximum = defaultMaximumTransferBytes
	}
	if maximum < 1 || maximum > maximumTransferBytes || options.ExpectedBytes < -1 ||
		options.ProgressInterval < 0 || options.ProgressBytes < 0 {
		return resolvedTransferOptions{}, fmt.Errorf("%w: transfer bounds are invalid", ErrInvalidTransfer)
	}
	expected := options.ExpectedBytes
	if expected == 0 && response.ContentLength >= 0 {
		expected = response.ContentLength
	} else if expected == 0 {
		expected = -1
	}
	if expected > maximum {
		return resolvedTransferOptions{}, &TransferLimitError{MaximumBytes: maximum, Bytes: expected}
	}
	var digest hash.Hash
	switch options.DigestAlgorithm {
	case "":
	case DigestSHA256:
		digest = sha256.New()
	case DigestSHA512:
		digest = sha512.New()
	default:
		return resolvedTransferOptions{}, fmt.Errorf("%w: digest algorithm is unsupported", ErrInvalidTransfer)
	}
	expectedDigest := append([]byte(nil), options.ExpectedDigest...)
	if len(expectedDigest) > 0 && (digest == nil || len(expectedDigest) != digest.Size()) {
		return resolvedTransferOptions{}, fmt.Errorf("%w: expected digest is invalid", ErrInvalidTransfer)
	}
	interval := options.ProgressInterval
	if interval == 0 {
		interval = defaultProgressInterval
	}
	progressBytes := options.ProgressBytes
	if progressBytes == 0 {
		progressBytes = defaultProgressBytes
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return resolvedTransferOptions{}, fmt.Errorf("%w: clock is nil", ErrInvalidTransfer)
	}

	return resolvedTransferOptions{
		maximum: maximum, expected: expected, digest: digest,
		expectedDigest: expectedDigest, progress: options.Progress,
		progressInterval: interval, progressBytes: progressBytes, clock: clock,
	}, nil
}
