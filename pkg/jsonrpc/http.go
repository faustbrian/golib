package jsonrpc

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const defaultMaxRequestBytes int64 = 4 << 20

// HTTPHandlerOption configures an HTTPHandler during construction.
type HTTPHandlerOption func(*HTTPHandler)

// WithMaxRequestBytes changes the default four-MiB HTTP request-body limit.
func WithMaxRequestBytes(limit int64) HTTPHandlerOption {
	return func(handler *HTTPHandler) {
		if limit > 0 {
			handler.maxRequestBytes = limit
		}
	}
}

// HTTPHandler adapts a Dispatcher to net/http with strict POST, content-type,
// and request-body handling.
type HTTPHandler struct {
	dispatcher      *Dispatcher
	maxRequestBytes int64
}

// NewHTTPHandler constructs an HTTP handler. A nil dispatcher is replaced by
// an empty dispatcher, and nil options are ignored.
func NewHTTPHandler(dispatcher *Dispatcher, options ...HTTPHandlerOption) *HTTPHandler {
	if dispatcher == nil {
		dispatcher = NewDispatcher(nil)
	}
	handler := &HTTPHandler{dispatcher: dispatcher, maxRequestBytes: defaultMaxRequestBytes}
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler
}

// ServeHTTP validates and dispatches one HTTP JSON-RPC request.
func (h *HTTPHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !IsJSONContentType(request.Header.Get("Content-Type")) {
		http.Error(writer, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	defer func() { _ = request.Body.Close() }()
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, h.maxRequestBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(writer, "could not read request body", http.StatusBadRequest)
		return
	}
	response, hasReply := h.dispatcher.Dispatch(request.Context(), body)
	if !hasReply {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(response)
}

// IsJSONContentType reports whether value is application/json,
// application/json-rpc, or an application subtype ending in +json.
func IsJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return mediaType == "application/json" ||
		mediaType == "application/json-rpc" ||
		strings.HasSuffix(mediaType, "+json")
}
