package httpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopyResponseStreamsWithDigestProgressAndClosure(t *testing.T) {
	t.Parallel()

	content := []byte("streaming response")
	expected := sha256.Sum256(content)
	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body:          &responseTestBody{Reader: bytes.NewReader(content), closed: &closed},
		ContentLength: int64(len(content)),
	}
	var destination bytes.Buffer
	var progress []TransferProgress
	result, err := CopyResponse(context.Background(), response, &destination, TransferOptions{
		MaximumBytes:    64,
		DigestAlgorithm: DigestSHA256,
		ExpectedDigest:  expected[:],
		Progress: func(_ context.Context, update TransferProgress) error {
			progress = append(progress, update)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("copy response: %v", err)
	}
	if destination.String() != string(content) || result.Bytes != int64(len(content)) ||
		!bytes.Equal(result.Digest, expected[:]) || closed.Load() != 1 {
		t.Fatalf("result = %#v, destination %q, closes %d", result, destination.String(), closed.Load())
	}
	if len(progress) != 2 || progress[0].Bytes != 0 ||
		progress[len(progress)-1].Bytes != int64(len(content)) || !progress[len(progress)-1].Complete {
		t.Fatalf("progress = %#v", progress)
	}
	progress[len(progress)-1].Digest[0] ^= 0xff
	if bytes.Equal(progress[len(progress)-1].Digest, result.Digest) {
		t.Fatal("progress digest aliases result")
	}
}

func TestCopyResponseEnforcesFiniteLengthAndDigest(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		content string
		options TransferOptions
		want    error
	}{
		{
			name: "maximum", content: "too large",
			options: TransferOptions{MaximumBytes: 4}, want: ErrTransferLimit,
		},
		{
			name: "expected length", content: "short",
			options: TransferOptions{MaximumBytes: 64, ExpectedBytes: 10}, want: ErrTransferLength,
		},
		{
			name: "digest", content: "content",
			options: TransferOptions{
				MaximumBytes: 64, DigestAlgorithm: DigestSHA256,
				ExpectedDigest: make([]byte, sha256.Size),
			},
			want: ErrDigestMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(test.content)), ContentLength: -1,
			}
			_, err := CopyResponse(context.Background(), response, io.Discard, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("copy error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCopyResponseCancellationAndSecretSafeFailures(t *testing.T) {
	t.Parallel()

	secret := errors.New("transfer-secret")
	for _, test := range []struct {
		name     string
		ctx      context.Context
		body     io.ReadCloser
		writer   io.Writer
		progress TransferProgressObserver
		want     error
	}{
		{
			name: "reader", ctx: context.Background(),
			body:   &compressionErrorBody{Reader: &responseErrorReader{err: secret}},
			writer: io.Discard, want: secret,
		},
		{
			name: "writer", ctx: context.Background(), body: io.NopCloser(strings.NewReader("body")),
			writer: transferWriterFunc(func([]byte) (int, error) { return 0, secret }), want: secret,
		},
		{
			name: "observer", ctx: context.Background(), body: io.NopCloser(strings.NewReader("body")),
			writer:   io.Discard,
			progress: func(context.Context, TransferProgress) error { return secret }, want: secret,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header), Body: test.body,
				ContentLength: -1,
			}
			_, err := CopyResponse(test.ctx, response, test.writer, TransferOptions{
				MaximumBytes: 64, Progress: test.progress,
			})
			var transferError *TransferError
			if !errors.As(err, &transferError) || !errors.Is(err, test.want) ||
				strings.Contains(err.Error(), secret.Error()) {
				t.Fatalf("transfer error = %#v", err)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("body")), ContentLength: -1,
	}
	if _, err := CopyResponse(ctx, response, io.Discard, TransferOptions{MaximumBytes: 64}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled copy error = %v", err)
	}
}

func TestCopyResponseContainsProgressPanicAndCloses(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          &responseTestBody{Reader: strings.NewReader("body"), closed: &closed},
		ContentLength: -1,
	}
	_, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 64,
		Progress: func(context.Context, TransferProgress) error {
			panic("progress-secret")
		},
	})
	var transferError *TransferError
	if !errors.As(err, &transferError) || !errors.Is(err, ErrTransferProgressPanic) {
		t.Fatalf("progress panic error = %#v", err)
	}
	if strings.Contains(err.Error(), "secret") || closed.Load() != 1 {
		t.Fatalf("progress panic rendered secret or leaked body: %q, closes = %d", err, closed.Load())
	}
}

func TestCopyResponseThrottlesIntermediateProgress(t *testing.T) {
	t.Parallel()

	clock := &transferTestClock{now: time.Unix(1_700_000_000, 0)}
	body := &advancingTransferReader{
		content: []byte("123456789"), clock: clock, advance: 10 * time.Millisecond,
	}
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(body), ContentLength: 9,
	}
	var updates []TransferProgress
	_, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 64, ProgressInterval: 25 * time.Millisecond,
		ProgressBytes: 3, Clock: clock,
		Progress: func(_ context.Context, update TransferProgress) error {
			updates = append(updates, update)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("copy response: %v", err)
	}
	if len(updates) != 4 || updates[0].Bytes != 0 || updates[1].Bytes != 3 ||
		updates[2].Bytes != 6 || !updates[3].Complete || updates[3].Bytes != 9 {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestCopyResponsePolicyAndLifecycleBoundaries(t *testing.T) {
	t.Parallel()

	if (&TransferLimitError{}).Error() == "" || (&TransferLengthError{}).Error() == "" ||
		(&DigestMismatchError{}).Error() == "" {
		t.Fatal("transfer errors rendered empty text")
	}
	var nilContext context.Context
	if _, err := CopyResponse(nilContext, nil, nil, TransferOptions{}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("invalid transfer error = %v", err)
	}

	closeFailure := errors.New("close")
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body:          &compressionErrorBody{Reader: strings.NewReader("body"), closeErr: closeFailure},
		ContentLength: 4,
	}
	result, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{})
	if result.Bytes != 4 || !errors.Is(err, closeFailure) {
		t.Fatalf("close failure result = %#v, %v", result, err)
	}

	for _, options := range []TransferOptions{
		{MaximumBytes: -1},
		{MaximumBytes: 64, ExpectedBytes: -2},
		{MaximumBytes: 64, ProgressInterval: -1},
		{MaximumBytes: 64, ProgressBytes: -1},
		{MaximumBytes: 4, ExpectedBytes: 5},
		{MaximumBytes: 64, DigestAlgorithm: "md5"},
		{MaximumBytes: 64, ExpectedDigest: []byte("missing algorithm")},
		{MaximumBytes: 64, DigestAlgorithm: DigestSHA256, ExpectedDigest: []byte("short")},
	} {
		response := &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: http.NoBody, ContentLength: -1,
		}
		if _, err := CopyResponse(context.Background(), response, io.Discard, options); err == nil {
			t.Fatalf("invalid options succeeded: %#v", options)
		}
	}
	var nilClock *transferTestClock
	response = &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody,
		ContentLength: -1,
	}
	if _, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 64, Clock: nilClock,
	}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("typed nil clock error = %v", err)
	}

	response = &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("four")), ContentLength: -1,
	}
	if _, err := CopyResponse(context.Background(), response, transferWriterFunc(func(buffer []byte) (int, error) {
		return 1, nil
	}), TransferOptions{MaximumBytes: 64}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v", err)
	}

	probeFailure := errors.New("probe")
	response = &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(&transferSequenceReader{
			content: []byte("four"), terminal: probeFailure,
		}),
		ContentLength: -1,
	}
	if _, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 4,
	}); !errors.Is(err, probeFailure) {
		t.Fatalf("probe read error = %v", err)
	}
	response = &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("four")), ContentLength: -1,
	}
	if result, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 4,
	}); err != nil || result.Bytes != 4 {
		t.Fatalf("exact maximum result = %#v, %v", result, err)
	}

	content := []byte("sha512")
	digest := sha512.Sum512(content)
	response = &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(content)), ContentLength: int64(len(content)),
	}
	result, err = CopyResponse(context.Background(), response, io.Discard, TransferOptions{
		MaximumBytes: 64, DigestAlgorithm: DigestSHA512, ExpectedDigest: digest[:],
	})
	if err != nil || !bytes.Equal(result.Digest, digest[:]) {
		t.Fatalf("SHA-512 result = %#v, %v", result, err)
	}

	for _, progressCase := range []struct {
		name          string
		contentLength int64
		progressBytes int64
	}{
		{name: "intermediate", contentLength: -1, progressBytes: 1},
		{name: "final", contentLength: 3, progressBytes: 4},
	} {
		calls := 0
		clock := &transferTestClock{now: time.Unix(1_700_000_000, 0)}
		reader := &advancingTransferReader{
			content: []byte("abc"), clock: clock, advance: time.Second,
		}
		response = &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(reader), ContentLength: progressCase.contentLength,
		}
		_, err = CopyResponse(context.Background(), response, io.Discard, TransferOptions{
			MaximumBytes: 64, ProgressBytes: progressCase.progressBytes,
			ProgressInterval: time.Millisecond,
			Clock:            clock,
			Progress: func(context.Context, TransferProgress) error {
				calls++
				if calls == 2 {
					return probeFailure
				}
				return nil
			},
		})
		if !errors.Is(err, probeFailure) {
			t.Fatalf("%s progress error = %v", progressCase.name, err)
		}
	}
}

type transferWriterFunc func([]byte) (int, error)

func (write transferWriterFunc) Write(buffer []byte) (int, error) { return write(buffer) }

type transferTestClock struct {
	now time.Time
}

func (clock *transferTestClock) Now() time.Time                      { return clock.now }
func (*transferTestClock) Wait(context.Context, time.Duration) error { return nil }

type advancingTransferReader struct {
	content []byte
	clock   *transferTestClock
	advance time.Duration
}

type transferSequenceReader struct {
	content  []byte
	terminal error
}

func (reader *transferSequenceReader) Read(buffer []byte) (int, error) {
	if len(reader.content) == 0 {
		return 0, reader.terminal
	}
	count := copy(buffer, reader.content)
	reader.content = reader.content[count:]
	return count, nil
}

func (reader *advancingTransferReader) Read(buffer []byte) (int, error) {
	if len(reader.content) == 0 {
		return 0, io.EOF
	}
	buffer[0] = reader.content[0]
	reader.content = reader.content[1:]
	reader.clock.now = reader.clock.now.Add(reader.advance)
	return 1, nil
}
