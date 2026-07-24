package httpclient

import (
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
)

const (
	defaultMultipartMaximumBytes = 64 << 20
	maximumMultipartBytes        = 1 << 40
	maximumMultipartParts        = 1000
	maximumMultipartTextBytes    = 8 << 10
)

var (
	// ErrInvalidMultipart indicates malformed multipart policy or metadata.
	ErrInvalidMultipart = errors.New("invalid multipart body")
	// ErrMultipartLimit indicates that a multipart body exceeded its limit.
	ErrMultipartLimit = errors.New("multipart body limit exceeded")
	// ErrMultipartPartLength indicates that a part did not match its declared length.
	ErrMultipartPartLength = errors.New("multipart part length mismatch")
)

// MultipartError reports a multipart operation failure without rendering its
// underlying error, which may contain request payload details.
type MultipartError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *MultipartError) Error() string {
	if err.Operation == "" {
		return "multipart body failed"
	}
	return "multipart body " + err.Operation + " failed"
}

// Unwrap returns the underlying multipart failure.
func (err *MultipartError) Unwrap() error {
	return err.Cause
}

// MultipartOptions configures a deterministic streaming multipart body.
type MultipartOptions struct {
	Boundary     string
	MaximumBytes int64
	Parts        []MultipartPart
}

// MultipartPart describes one form-data part and its owned request body.
type MultipartPart struct {
	Name     string
	FileName string
	Header   http.Header
	Body     RequestBody
}

type multipartPart struct {
	header textproto.MIMEHeader
	body   RequestBody
	length int64
}

type multipartRequestBody struct {
	boundary      string
	contentType   string
	contentLength int64
	maximumBytes  int64
	parts         []multipartPart
	replayable    bool
	mu            sync.Mutex
	consumed      bool
}

// NewMultipartBody validates and snapshots a multipart/form-data request body.
// A boundary is required so retries produce byte-identical wire content.
func NewMultipartBody(options MultipartOptions) (RequestBody, error) {
	maximum, err := validateMultipartOptions(options)
	if err != nil {
		return nil, err
	}

	parts := make([]multipartPart, 0, len(options.Parts))
	replayable := true
	for _, part := range options.Parts {
		snapshot, snapshotErr := snapshotMultipartPart(part)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		parts = append(parts, snapshot)
		replayable = replayable && snapshot.body.Replayable()
	}

	contentLength, err := multipartContentLength(options.Boundary, parts)
	if err != nil {
		return nil, err
	}
	if contentLength > maximum {
		return nil, fmt.Errorf("%w: declared body is too large", ErrMultipartLimit)
	}

	return &multipartRequestBody{
		boundary: options.Boundary, contentType: mime.FormatMediaType(
			"multipart/form-data", map[string]string{"boundary": options.Boundary},
		),
		contentLength: contentLength, maximumBytes: maximum,
		parts: parts, replayable: replayable,
	}, nil
}

func validateMultipartOptions(options MultipartOptions) (int64, error) {
	if options.Boundary == "" {
		return 0, fmt.Errorf("%w: boundary is required", ErrInvalidMultipart)
	}
	writer := multipart.NewWriter(io.Discard)
	if err := writer.SetBoundary(options.Boundary); err != nil {
		return 0, fmt.Errorf("%w: boundary is malformed", ErrInvalidMultipart)
	}
	if len(options.Parts) == 0 || len(options.Parts) > maximumMultipartParts {
		return 0, fmt.Errorf("%w: part count is outside the allowed range", ErrInvalidMultipart)
	}
	if options.MaximumBytes < 0 || options.MaximumBytes > maximumMultipartBytes {
		return 0, fmt.Errorf("%w: maximum bytes is outside the allowed range", ErrInvalidMultipart)
	}
	if options.MaximumBytes == 0 {
		return defaultMultipartMaximumBytes, nil
	}
	return options.MaximumBytes, nil
}

func snapshotMultipartPart(part MultipartPart) (multipartPart, error) {
	if !validMultipartText(part.Name, true) || !validMultipartText(part.FileName, false) {
		return multipartPart{}, fmt.Errorf("%w: part name or filename is malformed", ErrInvalidMultipart)
	}
	if nilLike(part.Body) {
		return multipartPart{}, fmt.Errorf("%w: part body is nil", ErrInvalidMultipart)
	}
	if err := validateBodyMetadata(part.Body.ContentType(), part.Body.ContentLength()); err != nil {
		return multipartPart{}, fmt.Errorf("%w: part body metadata is malformed", ErrInvalidMultipart)
	}

	header := make(textproto.MIMEHeader, len(part.Header)+2)
	disposition := map[string]string{"name": part.Name}
	if part.FileName != "" {
		disposition["filename"] = part.FileName
	}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", disposition))
	if part.Body.ContentType() != "" {
		header.Set("Content-Type", part.Body.ContentType())
	}
	if err := copyMultipartHeaders(header, part.Header); err != nil {
		return multipartPart{}, err
	}

	return multipartPart{header: header, body: part.Body, length: part.Body.ContentLength()}, nil
}

func validMultipartText(value string, required bool) bool {
	if required && value == "" || len(value) > maximumMultipartTextBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func copyMultipartHeaders(destination textproto.MIMEHeader, source http.Header) error {
	bytes := 0
	for key, values := range source {
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		switch canonical {
		case "Content-Disposition", "Content-Type", "Content-Length", "Transfer-Encoding":
			return fmt.Errorf("%w: part header is managed by the multipart body", ErrInvalidMultipart)
		}
		if canonical == "" || strings.TrimSpace(key) != key || len(values) == 0 {
			return fmt.Errorf("%w: part header is malformed", ErrInvalidMultipart)
		}
		bytes += len(canonical)
		for _, value := range values {
			bytes += len(value)
			if bytes > maximumMultipartTextBytes || !validMultipartText(value, false) {
				return fmt.Errorf("%w: part headers are malformed", ErrInvalidMultipart)
			}
			destination.Add(canonical, value)
		}
	}
	return nil
}

func multipartContentLength(boundary string, parts []multipartPart) (int64, error) {
	counter := &multipartCountingWriter{}
	return measureMultipartContentLength(boundary, parts, counter)
}

func measureMultipartContentLength(
	boundary string,
	parts []multipartPart,
	counter *multipartCountingWriter,
) (int64, error) {
	writer := multipart.NewWriter(counter)
	if err := writer.SetBoundary(boundary); err != nil {
		return 0, fmt.Errorf("%w: boundary is malformed", ErrInvalidMultipart)
	}
	length := int64(0)
	known := true
	for _, part := range parts {
		if _, err := writer.CreatePart(part.header); err != nil {
			return 0, &MultipartError{Operation: "metadata encoding", Cause: err}
		}
		if part.length < 0 {
			known = false
		} else if length > math.MaxInt64-part.length {
			return 0, fmt.Errorf("%w: declared body is too large", ErrMultipartLimit)
		} else {
			length += part.length
		}
	}
	if err := writer.Close(); err != nil {
		return 0, &MultipartError{Operation: "metadata encoding", Cause: err}
	}
	if !known {
		return -1, nil
	}
	if length > math.MaxInt64-counter.count {
		return 0, fmt.Errorf("%w: declared body is too large", ErrMultipartLimit)
	}
	return length + counter.count, nil
}

type multipartCountingWriter struct {
	count       int64
	destination io.Writer
}

func (writer *multipartCountingWriter) Write(buffer []byte) (int, error) {
	if writer.destination == nil {
		writer.count += int64(len(buffer))
		return len(buffer), nil
	}
	count, err := writer.destination.Write(buffer)
	writer.count += int64(count)
	return count, err
}

func (body *multipartRequestBody) Open() (io.ReadCloser, error) {
	body.mu.Lock()
	defer body.mu.Unlock()
	if !body.replayable && body.consumed {
		return nil, ErrBodyConsumed
	}
	if !body.replayable {
		body.consumed = true
	}

	sources := make([]*onceReadCloser, 0, len(body.parts))
	for _, part := range body.parts {
		source, err := part.body.Open()
		if err != nil {
			return nil, &MultipartError{
				Operation: "part open",
				Cause:     errors.Join(err, closeMultipartSources(sources)),
			}
		}
		if nilLike(source) {
			return nil, &MultipartError{
				Operation: "part open",
				Cause:     errors.Join(ErrInvalidBody, closeMultipartSources(sources)),
			}
		}
		sources = append(sources, &onceReadCloser{ReadCloser: source})
	}

	return newMultipartBodyReader(body.boundary, body.maximumBytes, body.parts, sources), nil
}

func (body *multipartRequestBody) Replayable() bool     { return body.replayable }
func (body *multipartRequestBody) ContentLength() int64 { return body.contentLength }
func (body *multipartRequestBody) ContentType() string  { return body.contentType }

type multipartBodyReader struct {
	reader    *io.PipeReader
	sources   []*onceReadCloser
	done      chan struct{}
	workerErr error
	once      sync.Once
	err       error
}

func newMultipartBodyReader(
	boundary string,
	maximum int64,
	parts []multipartPart,
	sources []*onceReadCloser,
) *multipartBodyReader {
	reader, pipeWriter := io.Pipe()
	body := &multipartBodyReader{reader: reader, sources: sources, done: make(chan struct{})}
	go func() {
		defer close(body.done)
		body.workerErr = writeMultipartBody(pipeWriter, boundary, maximum, parts, sources)
		_ = pipeWriter.CloseWithError(body.workerErr)
	}()
	return body
}

func writeMultipartBody(
	destination *io.PipeWriter,
	boundary string,
	maximum int64,
	parts []multipartPart,
	sources []*onceReadCloser,
) error {
	limited := &multipartLimitWriter{destination: destination, remaining: maximum}
	writer := multipart.NewWriter(limited)
	if err := writer.SetBoundary(boundary); err != nil {
		return &MultipartError{
			Operation: "encoding",
			Cause:     errors.Join(ErrInvalidMultipart, err),
		}
	}

	var result error
	for index, part := range parts {
		partWriter, err := writer.CreatePart(part.header)
		if err == nil {
			err = copyMultipartPart(partWriter, sources[index], part.length)
		}
		if closeErr := sources[index].Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if err != nil {
			result = &MultipartError{Operation: "part streaming", Cause: err}
			break
		}
	}
	if result == nil {
		if err := writer.Close(); err != nil {
			result = &MultipartError{Operation: "encoding", Cause: err}
		}
	}
	for _, source := range sources {
		if err := source.Close(); err != nil {
			result = errors.Join(result, &MultipartError{Operation: "part close", Cause: err})
		}
	}
	return result
}

func copyMultipartPart(destination io.Writer, source io.Reader, length int64) error {
	if length < 0 {
		_, err := io.Copy(destination, source)
		return err
	}
	written, err := io.CopyN(destination, source, length)
	if err != nil || written != length {
		return errors.Join(ErrMultipartPartLength, err)
	}
	var probe [1]byte
	count, err := source.Read(probe[:])
	if count != 0 || err == nil {
		return ErrMultipartPartLength
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type multipartLimitWriter struct {
	destination io.Writer
	remaining   int64
}

func (writer *multipartLimitWriter) Write(buffer []byte) (int, error) {
	if int64(len(buffer)) > writer.remaining {
		buffer = buffer[:writer.remaining]
		count, err := writer.destination.Write(buffer)
		writer.remaining -= int64(count)
		return count, errors.Join(ErrMultipartLimit, err)
	}
	count, err := writer.destination.Write(buffer)
	writer.remaining -= int64(count)
	return count, err
}

func (body *multipartBodyReader) Read(buffer []byte) (int, error) {
	return body.reader.Read(buffer)
}

func (body *multipartBodyReader) Close() error {
	body.once.Do(func() {
		readerErr := wrapMultipartCloseError("reader close", body.reader.Close())
		sourceErr := closeMultipartSources(body.sources)
		<-body.done
		body.err = errors.Join(readerErr, sourceErr, body.workerErr)
	})
	return body.err
}

func closeMultipartSources(sources []*onceReadCloser) error {
	var result error
	for _, source := range sources {
		result = errors.Join(result, wrapMultipartCloseError("part close", source.Close()))
	}
	return result
}

func wrapMultipartCloseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &MultipartError{Operation: operation, Cause: err}
}

var _ RequestBody = (*multipartRequestBody)(nil)
var _ io.ReadCloser = (*multipartBodyReader)(nil)
