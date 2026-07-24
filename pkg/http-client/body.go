package httpclient

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"reflect"
	"sync"
)

var (
	// ErrInvalidBody indicates an invalid request-body policy or implementation.
	ErrInvalidBody = errors.New("invalid request body")
	// ErrBodyConsumed indicates that a one-shot streaming body was already used.
	ErrBodyConsumed = errors.New("request body is already consumed")
)

// RequestBody opens request bodies and describes their replay and metadata
// policy. Implementations must be safe for concurrent calls when Replayable
// returns true.
type RequestBody interface {
	Open() (io.ReadCloser, error)
	Replayable() bool
	ContentLength() int64
	ContentType() string
}

// BodyOpener creates a fresh body reader for one physical request attempt.
type BodyOpener func() (io.ReadCloser, error)

// BodyOpenError reports that a request body could not be opened. Its rendered
// message does not include the underlying error, which may contain payload
// details.
type BodyOpenError struct {
	Cause error
}

// Error implements error.
func (*BodyOpenError) Error() string {
	return "request body open failed"
}

// Unwrap returns the body factory failure.
func (err *BodyOpenError) Unwrap() error {
	return err.Cause
}

// NewBytesBody snapshots content and returns a replayable request body.
func NewBytesBody(contentType string, content []byte) (RequestBody, error) {
	if err := validateBodyMetadata(contentType, int64(len(content))); err != nil {
		return nil, err
	}

	snapshot := append([]byte(nil), content...)

	return &factoryBody{
		contentType:   contentType,
		contentLength: int64(len(snapshot)),
		opener: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(snapshot)), nil
		},
	}, nil
}

// NewFormBody snapshots values as a canonical application/x-www-form-urlencoded
// body. Keys are sorted and repeated value order is preserved by url.Values.
func NewFormBody(values url.Values) (RequestBody, error) {
	return NewBytesBody("application/x-www-form-urlencoded", []byte(values.Encode()))
}

// NewReplayableBody returns a body that calls opener for the initial request
// and every replay. Opener must return an independent reader on every call.
func NewReplayableBody(contentType string, contentLength int64, opener BodyOpener) (RequestBody, error) {
	if opener == nil {
		return nil, fmt.Errorf("%w: replayable body opener is nil", ErrInvalidBody)
	}
	if err := validateBodyMetadata(contentType, contentLength); err != nil {
		return nil, err
	}

	return &factoryBody{
		contentType:   contentType,
		contentLength: contentLength,
		opener:        opener,
	}, nil
}

// NewStreamingBody transfers reader to the first build attempt that opens it.
// The body is not replayable, and subsequent build attempts return
// ErrBodyConsumed. If later request construction fails, the reader is closed.
func NewStreamingBody(contentType string, contentLength int64, reader io.ReadCloser) (RequestBody, error) {
	if nilLike(reader) {
		return nil, fmt.Errorf("%w: streaming reader is nil", ErrInvalidBody)
	}
	if err := validateBodyMetadata(contentType, contentLength); err != nil {
		return nil, err
	}

	return &streamingBody{
		contentType:   contentType,
		contentLength: contentLength,
		reader:        reader,
	}, nil
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type factoryBody struct {
	contentType   string
	contentLength int64
	opener        BodyOpener
}

func (body *factoryBody) Open() (io.ReadCloser, error) {
	return body.opener()
}

func (*factoryBody) Replayable() bool {
	return true
}

func (body *factoryBody) ContentLength() int64 {
	return body.contentLength
}

func (body *factoryBody) ContentType() string {
	return body.contentType
}

type streamingBody struct {
	contentType   string
	contentLength int64
	reader        io.ReadCloser
	mu            sync.Mutex
	consumed      bool
}

func (body *streamingBody) Open() (io.ReadCloser, error) {
	body.mu.Lock()
	defer body.mu.Unlock()

	if body.consumed {
		return nil, ErrBodyConsumed
	}
	body.consumed = true

	return body.reader, nil
}

func (*streamingBody) Replayable() bool {
	return false
}

func (body *streamingBody) ContentLength() int64 {
	return body.contentLength
}

func (body *streamingBody) ContentType() string {
	return body.contentType
}

func validateBodyMetadata(contentType string, contentLength int64) error {
	if contentLength < -1 {
		return fmt.Errorf("%w: content length must be -1 or greater", ErrInvalidBody)
	}
	if contentType == "" {
		return nil
	}
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return fmt.Errorf("%w: content type is malformed", ErrInvalidBody)
	}

	return nil
}
