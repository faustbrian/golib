package httpclient

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

var (
	// ErrInvalidCompression indicates invalid compression configuration.
	ErrInvalidCompression = errors.New("invalid HTTP compression policy")
	// ErrUnsupportedContentEncoding indicates an unconfigured response encoding.
	ErrUnsupportedContentEncoding = errors.New("unsupported HTTP content encoding")
	// ErrCompression indicates request compression failure.
	ErrCompression = errors.New("HTTP request compression failed")
	// ErrDecompression indicates malformed compressed data or decoder failure.
	ErrDecompression = errors.New("HTTP response decompression failed")
	// ErrDecompressionLimit indicates excessive output size or expansion.
	ErrDecompressionLimit = errors.New("HTTP response decompression limit reached")
)

const (
	defaultMaximumDecompressedBytes = 64 << 20
	maximumDecompressedBytes        = 1 << 40
	defaultMaximumExpansionRatio    = 100
	maximumExpansionRatio           = 1_000_000
	minimumRatioCheckBytes          = 1 << 10
	compressionMiddlewarePriority   = -100
)

// CompressionOptions configures explicit attempt-scoped response decoding.
type CompressionOptions struct {
	Name                     string
	Layer                    MiddlewareLayer
	Priority                 int
	CompressRequests         bool
	MinimumRequestBytes      int64
	MaximumDecompressedBytes int64
	MaximumExpansionRatio    float64
}

// CompressionError reports content-decoding failure without rendering the
// underlying decoder or body error.
type CompressionError struct {
	Operation string
	Encoding  string
	Cause     error
}

// Error implements error.
func (err *CompressionError) Error() string {
	return "HTTP " + err.Operation + " failed"
}

// Unwrap preserves both the stable category and underlying failure.
func (err *CompressionError) Unwrap() []error {
	category := ErrDecompression
	if errors.Is(err.Cause, ErrUnsupportedContentEncoding) {
		category = ErrUnsupportedContentEncoding
	} else if strings.HasPrefix(err.Operation, "request ") {
		category = ErrCompression
	}
	return []error{category, err.Cause}
}

// DecompressionLimitError reports finite compressed and decoded byte counts.
type DecompressionLimitError struct {
	MaximumBytes      int64
	MaximumRatio      float64
	CompressedBytes   int64
	DecompressedBytes int64
}

// Error implements error.
func (*DecompressionLimitError) Error() string {
	return "HTTP response decompression limit reached"
}

// Unwrap returns the stable decompression limit sentinel.
func (*DecompressionLimitError) Unwrap() error { return ErrDecompressionLimit }

type resolvedCompressionOptions struct {
	compressRequests    bool
	minimumRequestBytes int64
	maximumBytes        int64
	maximumRatio        float64
}

// NewCompressionMiddleware creates explicit attempt-scoped gzip response
// decoding. The default transport disables net/http implicit decompression so
// compressed input remains measurable.
func NewCompressionMiddleware(options CompressionOptions) (Middleware, error) {
	if options.MinimumRequestBytes < 0 {
		return Middleware{}, fmt.Errorf("%w: minimum request bytes are invalid", ErrInvalidCompression)
	}
	maximumBytes := options.MaximumDecompressedBytes
	if maximumBytes == 0 {
		maximumBytes = defaultMaximumDecompressedBytes
	}
	if maximumBytes < 1 || maximumBytes > maximumDecompressedBytes {
		return Middleware{}, fmt.Errorf("%w: maximum decoded bytes are invalid", ErrInvalidCompression)
	}
	maximumRatio := options.MaximumExpansionRatio
	if maximumRatio == 0 {
		maximumRatio = defaultMaximumExpansionRatio
	}
	if maximumRatio < 1 || maximumRatio > maximumExpansionRatio {
		return Middleware{}, fmt.Errorf("%w: maximum expansion ratio is invalid", ErrInvalidCompression)
	}
	policy := resolvedCompressionOptions{
		compressRequests: options.CompressRequests, minimumRequestBytes: options.MinimumRequestBytes,
		maximumBytes: maximumBytes, maximumRatio: maximumRatio,
	}

	return NewTransportMiddleware(MiddlewareOptions{
		Name: options.Name, Scope: ScopeAttempt, Layer: options.Layer,
		Priority: compressionMiddlewarePriority + options.Priority,
	}, policy.execute)
}

func (policy resolvedCompressionOptions) execute(request *http.Request, next Next) (*http.Response, error) {
	request = request.Clone(request.Context())
	if request.Header.Get("Accept-Encoding") == "" {
		request.Header.Set("Accept-Encoding", "gzip")
	}
	if policy.compressRequests && compressibleRequest(request, policy.minimumRequestBytes) {
		originalGetBody := request.GetBody
		request.Body = newGzipRequestBody(request.Body)
		request.ContentLength = -1
		request.Header.Del("Content-Length")
		request.Header.Set("Content-Encoding", "gzip")
		if originalGetBody != nil {
			request.GetBody = func() (io.ReadCloser, error) {
				source, err := originalGetBody()
				if err != nil {
					return nil, &BodyOpenError{Cause: err}
				}
				if source == nil {
					return nil, &BodyOpenError{Cause: ErrInvalidBody}
				}
				return newGzipRequestBody(source), nil
			}
		}
	}
	response, err := next(request)
	if err != nil {
		return nil, err
	}
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	if encoding == "" || encoding == "identity" {
		return response, nil
	}
	if encoding != "gzip" {
		closeErr := response.Body.Close()
		return nil, errors.Join(
			&CompressionError{
				Operation: "response content decoding", Encoding: encoding,
				Cause: ErrUnsupportedContentEncoding,
			},
			wrapCompressionCloseError(closeErr),
		)
	}
	counter := &countingReader{reader: response.Body}
	decoder, decodeErr := gzip.NewReader(counter)
	if decodeErr != nil {
		closeErr := response.Body.Close()
		return nil, errors.Join(
			&CompressionError{Operation: "response decompression", Encoding: encoding, Cause: decodeErr},
			wrapCompressionCloseError(closeErr),
		)
	}
	response.Body = &decompressionBody{
		decoder: decoder, source: response.Body, compressed: counter,
		maximumBytes: policy.maximumBytes, maximumRatio: policy.maximumRatio,
	}
	response.Header.Del("Content-Encoding")
	response.Header.Del("Content-Length")
	response.ContentLength = -1
	response.Uncompressed = true

	return response, nil
}

func compressibleRequest(request *http.Request, minimum int64) bool {
	if request.Body == nil || request.Body == http.NoBody || request.Header.Get("Content-Encoding") != "" {
		return false
	}
	return request.ContentLength < 0 || request.ContentLength >= minimum
}

func wrapCompressionCloseError(err error) error {
	if err == nil {
		return nil
	}
	return &CompressionError{Operation: "response body close", Cause: err}
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	reader.bytes += int64(count)
	return count, err
}

type decompressionBody struct {
	decoder       *gzip.Reader
	source        io.Closer
	compressed    *countingReader
	maximumBytes  int64
	maximumRatio  float64
	decompressed  int64
	terminalError error
	closeOnce     sync.Once
	closeErr      error
}

func (body *decompressionBody) Read(buffer []byte) (int, error) {
	if body.terminalError != nil {
		return 0, body.terminalError
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.decompressed == body.maximumBytes {
		var probe [1]byte
		count, err := body.decoder.Read(probe[:])
		if count > 0 {
			body.terminalError = body.limitError(body.decompressed + int64(count))
			return 0, body.terminalError
		}
		return 0, wrapCompressionReadError(err)
	}
	remaining := body.maximumBytes - body.decompressed
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := body.decoder.Read(buffer)
	body.decompressed += int64(count)
	if body.decompressed >= minimumRatioCheckBytes && body.compressed.bytes > 0 &&
		float64(body.decompressed) > body.maximumRatio*float64(body.compressed.bytes) {
		body.terminalError = body.limitError(body.decompressed)
		return 0, body.terminalError
	}
	return count, wrapCompressionReadError(err)
}

func (body *decompressionBody) limitError(decompressed int64) error {
	return &DecompressionLimitError{
		MaximumBytes: body.maximumBytes, MaximumRatio: body.maximumRatio,
		CompressedBytes: body.compressed.bytes, DecompressedBytes: decompressed,
	}
}

func (body *decompressionBody) Close() error {
	body.closeOnce.Do(func() {
		body.closeErr = errors.Join(
			wrapCompressionCloseError(body.decoder.Close()),
			wrapCompressionCloseError(body.source.Close()),
		)
	})
	return body.closeErr
}

func wrapCompressionReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	return &CompressionError{Operation: "response decompression", Encoding: "gzip", Cause: err}
}

type onceReadCloser struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (body *onceReadCloser) Close() error {
	body.once.Do(func() { body.err = body.ReadCloser.Close() })
	return body.err
}

type gzipRequestBody struct {
	reader    *io.PipeReader
	source    *onceReadCloser
	done      chan struct{}
	workerErr error
	once      sync.Once
	err       error
}

func newGzipRequestBody(source io.ReadCloser) *gzipRequestBody {
	ownedSource := &onceReadCloser{ReadCloser: source}
	reader, writer := io.Pipe()
	body := &gzipRequestBody{reader: reader, source: ownedSource, done: make(chan struct{})}
	go func() {
		defer close(body.done)
		compressor := gzip.NewWriter(writer)
		_, copyErr := io.Copy(compressor, ownedSource)
		compressErr := compressor.Close()
		closeErr := ownedSource.Close()
		var result error
		for _, err := range []error{copyErr, compressErr, closeErr} {
			if err != nil {
				result = errors.Join(result, &CompressionError{
					Operation: "request compression", Encoding: "gzip", Cause: err,
				})
			}
		}
		body.workerErr = result
		_ = writer.CloseWithError(result)
	}()
	return body
}

func (body *gzipRequestBody) Read(buffer []byte) (int, error) {
	return body.reader.Read(buffer)
}

func (body *gzipRequestBody) Close() error {
	body.once.Do(func() {
		readerErr := body.reader.Close()
		sourceErr := body.source.Close()
		<-body.done
		body.err = errors.Join(readerErr, sourceErr, body.workerErr)
	})
	return body.err
}
