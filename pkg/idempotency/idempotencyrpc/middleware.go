// Package idempotencyrpc provides method-aware durable JSON-RPC invocation
// ownership and bounded response or protocol-error replay.
package idempotencyrpc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const (
	// MaxResponseBytes is the largest persisted JSON-RPC response envelope.
	MaxResponseBytes = 900 * 1024
	// MinResponseBytes leaves room for a durable internal-error response.
	MinResponseBytes         = 256
	defaultMaxBytes          = 64 * 1024
	replaySchema             = 1
	defaultTransitionTimeout = 5 * time.Second
)

// Request contains the business fields used for method-aware idempotency.
type Request struct {
	Method string
	Params json.RawMessage
}

// Error is a JSON-RPC protocol error that can be durably replayed.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Response contains exactly one JSON result or protocol error.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Handler executes one elected JSON-RPC request owner.
type Handler func(context.Context, Request) Response

// KeyFunc constructs a caller and method-scoped key.
type KeyFunc func(context.Context, Request) (idempotency.Key, error)

// FingerprintFunc computes canonical business-request identity.
type FingerprintFunc func(Request) (idempotency.Fingerprint, error)

// Options configures method-aware durable JSON-RPC invocation.
type Options struct {
	Service           *idempotency.Service
	Lease             time.Duration
	MaxResponseBytes  int
	TransitionTimeout time.Duration
	Key               KeyFunc
	Fingerprint       FingerprintFunc
}

// CallResult reports the semantic outcome and any handler response.
type CallResult struct {
	Outcome  idempotency.Outcome
	Response Response
	Replayed bool
}

// Middleware durably elects handlers and replays bounded responses.
type Middleware struct {
	service           *idempotency.Service
	lease             time.Duration
	maxResponseBytes  int
	transitionTimeout time.Duration
	key               KeyFunc
	fingerprint       FingerprintFunc
}

// New validates options and constructs JSON-RPC middleware.
func New(options Options) (*Middleware, error) {
	if options.Service == nil {
		return nil, configurationError("service")
	}
	if options.Lease <= 0 || options.Lease > idempotency.MaxLease {
		return nil, configurationError("lease")
	}
	if options.Key == nil {
		return nil, configurationError("key")
	}
	if options.Fingerprint == nil {
		return nil, configurationError("fingerprint")
	}
	if options.MaxResponseBytes == 0 {
		options.MaxResponseBytes = defaultMaxBytes
	}
	if options.MaxResponseBytes < MinResponseBytes || options.MaxResponseBytes > MaxResponseBytes {
		return nil, configurationError("max_response_bytes")
	}
	if options.TransitionTimeout == 0 {
		options.TransitionTimeout = defaultTransitionTimeout
	}
	if options.TransitionTimeout < 0 {
		return nil, configurationError("transition_timeout")
	}
	return &Middleware{
		service: options.Service, lease: options.Lease,
		maxResponseBytes:  options.MaxResponseBytes,
		transitionTimeout: options.TransitionTimeout,
		key:               options.Key, fingerprint: options.Fingerprint,
	}, nil
}

// Call invokes handler only for a newly acquired or taken-over request.
func (m *Middleware) Call(
	ctx context.Context,
	request Request,
	handler Handler,
) (result CallResult, err error) {
	if request.Method == "" {
		return CallResult{}, payloadError("method", nil)
	}
	if handler == nil {
		return CallResult{}, configurationError("handler")
	}
	key, err := m.key(ctx, request)
	if err != nil {
		return CallResult{}, err
	}
	if key.Operation() != request.Method {
		return CallResult{}, payloadError("method_namespace", nil)
	}
	fingerprint, err := m.fingerprint(request)
	if err != nil {
		return CallResult{}, err
	}
	begin, err := m.service.Begin(ctx, idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{Key: key, Fingerprint: fingerprint, Lease: m.lease},
	})
	if err != nil {
		return CallResult{}, err
	}
	if begin.Outcome == idempotency.OutcomeConflict || begin.Outcome == idempotency.OutcomeInProgress {
		return CallResult{Outcome: begin.Outcome}, nil
	}
	if begin.Outcome == idempotency.OutcomeReplayed ||
		begin.Outcome == idempotency.OutcomeTerminalFailure {
		response, err := decodeResponse(begin.Record.Result, m.maxResponseBytes)
		if err != nil {
			return CallResult{}, err
		}
		return CallResult{Outcome: begin.Outcome, Response: response, Replayed: true}, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = m.release(ctx, begin.Record.Ownership())
			panic(recovered)
		}
	}()
	handlerCtx := idempotency.WithOwnership(ctx, begin.Record.Ownership())
	response := handler(handlerCtx, request)
	encoded, encodeErr := encodeResponse(response, m.maxResponseBytes)
	if encodeErr != nil {
		return m.fail(ctx, begin.Record.Ownership())
	}
	if _, err := m.service.Complete(ctx, idempotency.CompleteRequest{
		Ownership: begin.Record.Ownership(), Result: encoded,
	}); err != nil {
		return CallResult{}, err
	}
	return CallResult{Outcome: begin.Outcome, Response: response}, nil
}

func (m *Middleware) release(ctx context.Context, ownership idempotency.Ownership) error {
	transitionCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), m.transitionTimeout,
	)
	defer cancel()
	_, err := m.service.Release(transitionCtx, ownership)
	return err
}

func (m *Middleware) fail(ctx context.Context, ownership idempotency.Ownership) (CallResult, error) {
	response := Response{Error: &Error{Code: -32603, Message: "internal error"}}
	encoded, _ := encodeResponse(response, m.maxResponseBytes)
	if _, err := m.service.Fail(ctx, idempotency.FailRequest{
		Ownership: ownership, Result: encoded,
	}); err != nil {
		return CallResult{}, err
	}
	return CallResult{Outcome: idempotency.OutcomeTerminalFailure, Response: response}, nil
}

type persistedResponse struct {
	Schema   int      `json:"schema"`
	Response Response `json:"response"`
}

func encodeResponse(response Response, limit int) ([]byte, error) {
	if !response.valid() {
		return nil, payloadError("response", nil)
	}
	encoded, _ := json.Marshal(persistedResponse{Schema: replaySchema, Response: response})
	if len(encoded) > limit {
		return nil, &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "response"}
	}
	return encoded, nil
}

func decodeResponse(encoded []byte, limit int) (Response, error) {
	if len(encoded) > limit {
		return Response{}, &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "response"}
	}
	var persisted persistedResponse
	if err := json.Unmarshal(encoded, &persisted); err != nil ||
		persisted.Schema != replaySchema || !persisted.Response.valid() {
		return Response{}, payloadError("persisted_response", err)
	}
	return persisted.Response, nil
}

func (r Response) valid() bool {
	if (len(r.Result) == 0) == (r.Error == nil) {
		return false
	}
	if len(r.Result) > 0 {
		return json.Valid(r.Result)
	}
	return r.Error.Message != "" && (len(r.Error.Data) == 0 || json.Valid(r.Error.Data))
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}

func payloadError(field string, cause error) error {
	if cause == nil {
		cause = errors.New("invalid JSON-RPC payload")
	}
	return &idempotency.Error{Reason: idempotency.ReasonInvalidPayload, Field: field, Cause: cause}
}
