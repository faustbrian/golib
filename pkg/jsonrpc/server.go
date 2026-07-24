package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"unicode/utf8"
)

// Registration errors identify reserved method names, duplicate methods, and
// nil handlers.
var (
	ErrInvalidMethodName       = errors.New("jsonrpc: invalid method name")
	ErrMethodAlreadyRegistered = errors.New("jsonrpc: method already registered")
	ErrNilHandler              = errors.New("jsonrpc: nil handler")
	parameterNames             sync.Map
)

const (
	defaultMaxDispatchBytes int64 = 4 << 20
	defaultMaxBatchItems          = 1024
)

// Handler implements one JSON-RPC method.
type Handler func(context.Context, json.RawMessage) (any, error)

// Middleware wraps a Handler with cross-cutting behavior.
type Middleware func(Handler) Handler

// ErrorMapper converts an application error into safe public JSON-RPC data.
type ErrorMapper func(error) *Error

// Hooks observe the complete dispatcher lifecycle, including protocol errors
// that occur before a Handler or Middleware can run. A nil Request represents
// an unparseable or invalid request. Notifications provide an internal outcome
// to OnResponse even though that response is never placed on the wire.
type Hooks struct {
	// OnRequest observes a copied validated request or nil for an invalid
	// envelope and may return a derived context for subsequent processing.
	OnRequest func(context.Context, *Request) context.Context
	// OnResponse observes a copied internal outcome, including notifications.
	OnResponse func(context.Context, *Request, *Response)
}

type requestContextKey struct{}

// RequestFromContext returns the validated request made available to the
// active middleware or handler.
func RequestFromContext(ctx context.Context) (Request, bool) {
	request, ok := ctx.Value(requestContextKey{}).(Request)
	return request, ok
}

// Registry is a concurrency-safe method registry. Its zero value is ready for
// use. A Registry must not be copied after first use.
type Registry struct {
	mu      sync.RWMutex
	methods map[string]Handler
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry { return &Registry{methods: make(map[string]Handler)} }

// Register adds handler under name. Names beginning with rpc. are reserved,
// and an existing name cannot be replaced.
func (r *Registry) Register(name string, handler Handler) error {
	if strings.HasPrefix(name, "rpc.") {
		return ErrInvalidMethodName
	}
	return r.register(name, handler)
}

// RegisterSystem adds an explicitly reserved rpc.* handler. This method is
// intended for protocol extensions such as OpenRPC's rpc.discover and rejects
// application method names.
func (r *Registry) RegisterSystem(name string, handler Handler) error {
	if !strings.HasPrefix(name, "rpc.") {
		return ErrInvalidMethodName
	}
	return r.register(name, handler)
}

func (r *Registry) register(name string, handler Handler) error {
	if handler == nil {
		return ErrNilHandler
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.methods == nil {
		r.methods = make(map[string]Handler)
	}
	if _, exists := r.methods[name]; exists {
		return fmt.Errorf("%w: %s", ErrMethodAlreadyRegistered, name)
	}
	r.methods[name] = handler
	return nil
}

// Lookup returns the handler registered under name.
func (r *Registry) Lookup(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.methods[name]
	return handler, ok
}

// DispatcherOption configures a Dispatcher during construction.
type DispatcherOption func(*Dispatcher)

// WithMiddleware appends middleware in outermost-to-innermost order.
func WithMiddleware(middleware ...Middleware) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		dispatcher.middleware = append(dispatcher.middleware, middleware...)
	}
}

// WithErrorMapper replaces the default internal-error mapper. Returning nil or
// an invalid error is contained as an internal error.
func WithErrorMapper(mapper ErrorMapper) DispatcherOption {
	return func(dispatcher *Dispatcher) { dispatcher.errorMapper = mapper }
}

// WithHooks installs lifecycle observers. Hook panics are contained and hook
// mutations cannot change protocol output.
func WithHooks(hooks Hooks) DispatcherOption {
	return func(dispatcher *Dispatcher) { dispatcher.hooks = hooks }
}

// WithMaxDispatchBytes changes the dispatcher's four-MiB payload limit.
func WithMaxDispatchBytes(limit int64) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		if limit > 0 {
			dispatcher.maxDispatchBytes = limit
		}
	}
}

// WithMaxBatchItems changes the dispatcher's default limit of 1,024 members.
func WithMaxBatchItems(limit int) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		if limit > 0 {
			dispatcher.maxBatchItems = limit
		}
	}
}

// Dispatcher validates and executes JSON-RPC requests and batches. Its
// configuration is immutable after construction; its Registry may be updated
// concurrently.
type Dispatcher struct {
	registry         *Registry
	middleware       []Middleware
	errorMapper      ErrorMapper
	hooks            Hooks
	maxDispatchBytes int64
	maxBatchItems    int
}

// NewDispatcher constructs a bounded dispatcher. A nil registry is replaced
// by an empty registry, and nil options are ignored.
func NewDispatcher(registry *Registry, options ...DispatcherOption) *Dispatcher {
	if registry == nil {
		registry = NewRegistry()
	}
	dispatcher := &Dispatcher{
		registry:         registry,
		maxDispatchBytes: defaultMaxDispatchBytes,
		maxBatchItems:    defaultMaxBatchItems,
		errorMapper: func(err error) *Error {
			return InternalError().WithCause(err)
		},
	}
	for _, option := range options {
		if option != nil {
			option(dispatcher)
		}
	}
	return dispatcher
}

// Dispatch processes one JSON-RPC message. The boolean reports whether the
// caller must send the returned response; notifications intentionally return
// no response.
//
// Notification and batch behavior follow:
//   - https://www.jsonrpc.org/specification#notification
//   - https://www.jsonrpc.org/specification#batch
func (d *Dispatcher) Dispatch(ctx context.Context, payload []byte) ([]byte, bool) {
	if int64(len(payload)) > d.maxDispatchBytes {
		return d.failure(ctx, RequestLimitExceeded())
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || !utf8.Valid(trimmed) || !json.Valid(trimmed) {
		return d.failure(ctx, ParseError())
	}
	if trimmed[0] == '[' {
		if batchExceedsLimit(trimmed, d.maxBatchItems) {
			return d.failure(ctx, RequestLimitExceeded())
		}
		return d.dispatchBatch(ctx, trimmed)
	}
	if trimmed[0] != '{' {
		return d.failure(ctx, InvalidRequest())
	}
	response, ok := d.dispatchItem(ctx, trimmed)
	if !ok {
		return nil, false
	}
	return marshalResponse(response), true
}

func batchExceedsLimit(payload []byte, limit int) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	_, _ = decoder.Token()
	count := 0
	var item json.RawMessage
	for decoder.More() {
		count++
		if count > limit {
			return true
		}
		_ = decoder.Decode(&item)
	}
	return false
}

func (d *Dispatcher) dispatchBatch(ctx context.Context, payload []byte) ([]byte, bool) {
	var items []json.RawMessage
	_ = json.Unmarshal(payload, &items)
	if len(items) == 0 {
		return d.failure(ctx, InvalidRequest())
	}
	responses := make([]Response, 0, len(items))
	for _, item := range items {
		response, ok := d.dispatchItem(ctx, item)
		if ok {
			responses = append(responses, response)
		}
	}
	if len(responses) == 0 {
		return nil, false
	}
	encoded, _ := json.Marshal(responses)
	return encoded, true
}

func (d *Dispatcher) dispatchItem(ctx context.Context, payload []byte) (response Response, reply bool) {
	if len(payload) == 0 || payload[0] != '{' {
		response = errorResponse(NullID(), InvalidRequest())
		ctx = d.begin(ctx, nil)
		d.finish(ctx, nil, &response)
		return response, true
	}
	var request Request
	if err := json.Unmarshal(payload, &request); err != nil {
		response = errorResponse(NullID(), InvalidRequest().WithCause(err))
		ctx = d.begin(ctx, nil)
		d.finish(ctx, nil, &response)
		return response, true
	}
	if rpcErr := request.Validate(); rpcErr != nil {
		response = errorResponse(NullID(), rpcErr)
		ctx = d.begin(ctx, nil)
		d.finish(ctx, nil, &response)
		return response, true
	}
	ctx = d.begin(ctx, &request)
	if request.IsNotification() {
		outcome := d.execute(ctx, request)
		d.finish(ctx, &request, &outcome)
		return Response{}, false
	}
	response = d.execute(ctx, request)
	d.finish(ctx, &request, &response)
	return response, true
}

func (d *Dispatcher) execute(ctx context.Context, request Request) (response Response) {
	ctx = context.WithValue(ctx, requestContextKey{}, request)
	response = Response{JSONRPC: Version, ID: request.ID, idSet: true}
	defer func() {
		if recovered := recover(); recovered != nil {
			response = errorResponse(request.ID, InternalError().WithCause(fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())))
		}
	}()

	handler, ok := d.registry.Lookup(request.Method)
	if !ok {
		return errorResponse(request.ID, MethodNotFound())
	}
	for index := len(d.middleware) - 1; index >= 0; index-- {
		if d.middleware[index] != nil {
			handler = d.middleware[index](handler)
		}
	}
	result, err := handler(ctx, request.Params)
	if err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			return errorResponse(request.ID, rpcErr)
		}
		mapped := d.errorMapper(err)
		if mapped == nil {
			mapped = InternalError().WithCause(err)
		}
		return errorResponse(request.ID, mapped)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return errorResponse(request.ID, InternalError().WithCause(err))
	}
	response.Result = encoded
	response.resultSet = true
	return response
}

func (d *Dispatcher) failure(ctx context.Context, rpcErr *Error) ([]byte, bool) {
	response := errorResponse(NullID(), rpcErr)
	ctx = d.begin(ctx, nil)
	d.finish(ctx, nil, &response)
	return marshalResponse(response), true
}

func (d *Dispatcher) begin(ctx context.Context, request *Request) (observed context.Context) {
	observed = ctx
	if d.hooks.OnRequest == nil {
		return observed
	}
	defer func() {
		if recover() != nil || observed == nil {
			observed = ctx
		}
	}()
	if request == nil {
		return d.hooks.OnRequest(ctx, nil)
	}
	requestCopy := *request
	requestCopy.Params = append(json.RawMessage(nil), request.Params...)
	requestCopy.ID.raw = append(json.RawMessage(nil), request.ID.raw...)
	return d.hooks.OnRequest(ctx, &requestCopy)
}

func (d *Dispatcher) finish(ctx context.Context, request *Request, response *Response) {
	if d.hooks.OnResponse == nil {
		return
	}
	defer func() { _ = recover() }()
	observed := *response
	observed.Result = append(json.RawMessage(nil), response.Result...)
	observed.ID.raw = append(json.RawMessage(nil), response.ID.raw...)
	if response.Error != nil {
		rpcErr := *response.Error
		rpcErr.Data = append(json.RawMessage(nil), response.Error.Data...)
		observed.Error = &rpcErr
	}
	d.hooks.OnResponse(ctx, request, &observed)
}

func errorResponse(id ID, rpcErr *Error) Response {
	if rpcErr == nil || !rpcErr.valid() || (len(rpcErr.Data) > 0 && !json.Valid(rpcErr.Data)) {
		rpcErr = InternalError().WithCause(rpcErr)
	}
	return Response{
		JSONRPC:  Version,
		Error:    rpcErr,
		ID:       id,
		errorSet: true,
		idSet:    true,
	}
}

func marshalResponse(response Response) []byte {
	encoded, err := json.Marshal(response)
	if err == nil {
		return encoded
	}
	return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`)
}

// DecodeParams strictly decodes params as T. Duplicate or unknown named
// members, malformed JSON, and trailing data return InvalidParams.
func DecodeParams[T any](params json.RawMessage) (T, *Error) {
	var value T
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 {
		return value, InvalidParams()
	}
	if err := rejectDuplicateMembers(trimmed); err != nil {
		return value, InvalidParams().WithCause(err)
	}
	if !namedParameterNamesMatch[T](params) {
		return value, InvalidParams()
	}
	decoder := json.NewDecoder(bytes.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, InvalidParams().WithCause(err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, InvalidParams()
	}
	return value, nil
}

func namedParameterNamesMatch[T any](params json.RawMessage) bool {
	trimmed := bytes.TrimSpace(params)
	if trimmed[0] != '{' {
		return true
	}
	names, structured := parameterNameSet(reflect.TypeFor[T]())
	if !structured {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(trimmed, &object) != nil {
		return true
	}
	for name := range object {
		if _, ok := names[name]; !ok {
			return false
		}
	}
	return true
}

func parameterNameSet(parameterType reflect.Type) (map[string]struct{}, bool) {
	for parameterType.Kind() == reflect.Pointer {
		parameterType = parameterType.Elem()
	}
	if parameterType.Kind() != reflect.Struct {
		return nil, false
	}
	if cached, ok := parameterNames.Load(parameterType); ok {
		return cached.(map[string]struct{}), true
	}
	names := make(map[string]struct{})
	for _, field := range reflect.VisibleFields(parameterType) {
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if name == "" && field.Anonymous && fieldType.Kind() == reflect.Struct {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names[name] = struct{}{}
	}
	parameterNames.Store(parameterType, names)
	return names, true
}
