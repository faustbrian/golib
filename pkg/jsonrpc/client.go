package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// Client errors identify transport, envelope, correlation, batch, and size
// failures and support errors.Is through direct return or wrapping.
var (
	ErrTransport              = errors.New("jsonrpc: transport error")
	ErrInvalidResponse        = errors.New("jsonrpc: invalid response")
	ErrMismatchedID           = errors.New("jsonrpc: mismatched response id")
	ErrUnexpectedResponse     = errors.New("jsonrpc: unexpected response")
	ErrMissingResponse        = errors.New("jsonrpc: missing batch response")
	ErrDuplicateResponse      = errors.New("jsonrpc: duplicate batch response")
	ErrDuplicateRequestID     = errors.New("jsonrpc: duplicate batch request id")
	ErrEmptyBatch             = errors.New("jsonrpc: empty client batch")
	ErrClientResponseTooLarge = errors.New("jsonrpc: client response too large")
)

const defaultMaxClientResponseBytes int64 = 4 << 20

// Transport exchanges one complete JSON-RPC payload with a peer.
type Transport interface {
	RoundTrip(context.Context, []byte) ([]byte, error)
}

// TransportFunc adapts a function to Transport.
type TransportFunc func(context.Context, []byte) ([]byte, error)

// RoundTrip calls function with ctx and payload.
func (function TransportFunc) RoundTrip(ctx context.Context, payload []byte) ([]byte, error) {
	return function(ctx, payload)
}

// IDGenerator supplies request IDs to a Client.
type IDGenerator interface {
	NextID() ID
}

// AtomicIDGenerator generates concurrency-safe, monotonically increasing
// numeric IDs.
type AtomicIDGenerator struct{ value atomic.Int64 }

// NewAtomicIDGenerator creates a generator whose first ID is start+1.
func NewAtomicIDGenerator(start int64) *AtomicIDGenerator {
	generator := &AtomicIDGenerator{}
	generator.value.Store(start)
	return generator
}

// NextID atomically increments the generator and returns the new numeric ID.
func (generator *AtomicIDGenerator) NextID() ID {
	return NumberID(json.Number(strconv.FormatInt(generator.value.Add(1), 10)))
}

// ClientOption configures a Client during construction.
type ClientOption func(*Client)

// WithIDGenerator installs a non-nil request ID generator.
func WithIDGenerator(generator IDGenerator) ClientOption {
	return func(client *Client) {
		if generator != nil {
			client.ids = generator
		}
	}
}

// WithMaxClientResponseBytes changes the client's four-MiB reply parsing limit.
func WithMaxClientResponseBytes(limit int64) ClientOption {
	return func(client *Client) {
		if limit > 0 {
			client.maxResponseBytes = limit
		}
	}
}

// Client validates requests and correlates responses over a Transport. Its
// default AtomicIDGenerator is safe for concurrent calls; custom transports,
// generators, and BatchCall values retain their own concurrency contracts.
type Client struct {
	transport        Transport
	ids              IDGenerator
	maxResponseBytes int64
}

// NewClient constructs a client. A nil transport is reported as ErrTransport
// when an operation is attempted. Nil options are ignored.
func NewClient(transport Transport, options ...ClientOption) *Client {
	client := &Client{
		transport:        transport,
		ids:              NewAtomicIDGenerator(0),
		maxResponseBytes: defaultMaxClientResponseBytes,
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
}

// Call sends one request, validates and correlates its response, and decodes a
// successful result into result when result is non-nil.
func (client *Client) Call(ctx context.Context, method string, params, result any) error {
	request, err := NewRequest(method, params, client.ids.NextID())
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(request)
	reply, err := client.roundTrip(ctx, payload)
	if err != nil {
		return err
	}
	var response Response
	if err := json.Unmarshal(reply, &response); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	if err := response.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	if !request.ID.Equal(response.ID) {
		return ErrMismatchedID
	}
	if response.Error != nil {
		return response.Error
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("%w: result: %w", ErrInvalidResponse, err)
	}
	return nil
}

// Call sends one request and returns its result decoded as T.
func Call[T any](ctx context.Context, client *Client, method string, params any) (T, error) {
	var result T
	err := client.Call(ctx, method, params, &result)
	return result, err
}

// Notify sends a notification and requires the peer to return no payload.
func (client *Client) Notify(ctx context.Context, method string, params any) error {
	request, err := NewNotification(method, params)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(request)
	reply, err := client.roundTrip(ctx, payload)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(reply)) != 0 {
		return ErrUnexpectedResponse
	}
	return nil
}

// BatchCall describes one member of a client batch. Batch writes a decoded
// success into Result and assigns a protocol failure to Error.
type BatchCall struct {
	// Method is the JSON-RPC method name.
	Method string
	// Params must encode as an object or array when non-nil.
	Params any
	// Result receives a successful response when non-nil.
	Result any
	// Notification omits the request ID and expects no response member.
	Notification bool
	// Error receives a valid JSON-RPC failure response.
	Error *Error

	id ID
}

// Batch sends calls together and correlates every non-notification response by
// ID. It rejects empty batches, nil calls, duplicate generated IDs, and
// malformed response membership.
func (client *Client) Batch(ctx context.Context, calls ...*BatchCall) error {
	if len(calls) == 0 {
		return ErrEmptyBatch
	}
	requests := make([]Request, 0, len(calls))
	pending := make(map[string]*BatchCall, len(calls))
	for _, call := range calls {
		if call == nil {
			return errors.New("jsonrpc: nil batch call")
		}
		call.Error = nil
		var request Request
		var err error
		if call.Notification {
			request, err = NewNotification(call.Method, call.Params)
		} else {
			call.id = client.ids.NextID()
			request, err = NewRequest(call.Method, call.Params, call.id)
			key := idKey(call.id)
			if _, duplicate := pending[key]; duplicate {
				return ErrDuplicateRequestID
			}
			pending[key] = call
		}
		if err != nil {
			return err
		}
		requests = append(requests, request)
	}
	payload, _ := json.Marshal(requests)
	reply, err := client.roundTrip(ctx, payload)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		if len(bytes.TrimSpace(reply)) != 0 {
			return ErrUnexpectedResponse
		}
		return nil
	}
	trimmed := bytes.TrimSpace(reply)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return ErrInvalidResponse
	}
	var responses []Response
	if err := json.Unmarshal(trimmed, &responses); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	seen := make(map[string]struct{}, len(responses))
	for _, response := range responses {
		if err := response.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidResponse, err)
		}
		key := idKey(response.ID)
		call, ok := pending[key]
		if !ok {
			return ErrMismatchedID
		}
		if _, duplicate := seen[key]; duplicate {
			return ErrDuplicateResponse
		}
		seen[key] = struct{}{}
		if response.Error != nil {
			call.Error = response.Error
			continue
		}
		if call.Result != nil {
			if err := json.Unmarshal(response.Result, call.Result); err != nil {
				return fmt.Errorf("%w: result: %w", ErrInvalidResponse, err)
			}
		}
	}
	if len(seen) != len(pending) {
		return ErrMissingResponse
	}
	return nil
}

func (client *Client) roundTrip(ctx context.Context, payload []byte) ([]byte, error) {
	if client.transport == nil {
		return nil, fmt.Errorf("%w: nil transport", ErrTransport)
	}
	reply, err := client.transport.RoundTrip(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	if int64(len(reply)) > client.maxResponseBytes {
		return nil, ErrClientResponseTooLarge
	}
	return reply, nil
}

// NewRequest constructs a validated request with an explicit non-missing ID.
func NewRequest(method string, params any, id ID) (Request, error) {
	if err := validateClientMethod(method); err != nil {
		return Request{}, err
	}
	if id.Kind() == IDMissing {
		return Request{}, errors.New("jsonrpc: request id is required")
	}
	if !id.valid() {
		return Request{}, errors.New("jsonrpc: request id is invalid")
	}
	encoded, err := encodeParams(params)
	if err != nil {
		return Request{}, err
	}
	return Request{JSONRPC: Version, Method: method, Params: encoded, ID: id, idSet: true, methodSet: true}, nil
}

// NewNotification constructs a validated request without an ID.
func NewNotification(method string, params any) (Request, error) {
	if err := validateClientMethod(method); err != nil {
		return Request{}, err
	}
	encoded, err := encodeParams(params)
	if err != nil {
		return Request{}, err
	}
	return Request{JSONRPC: Version, Method: method, Params: encoded, methodSet: true}, nil
}

func validateClientMethod(method string) error {
	if strings.HasPrefix(method, "rpc.") {
		return ErrInvalidMethodName
	}
	return nil
}

func encodeParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: encode params: %w", err)
	}
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, errors.New("jsonrpc: params must encode as an object or array")
	}
	return encoded, nil
}

func idKey(id ID) string { return strconv.Itoa(int(id.Kind())) + ":" + id.canonical }
