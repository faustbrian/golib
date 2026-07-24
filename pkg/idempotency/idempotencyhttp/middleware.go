package idempotencyhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const (
	// HeaderKey carries the caller's idempotency key.
	HeaderKey = "Idempotency-Key"
	// HeaderOutcome reports the semantic result of the request.
	HeaderOutcome = "Idempotency-Outcome"
	// HeaderReplayed is true when the response came from a durable record.
	HeaderReplayed = "Idempotency-Replayed"
	// MaxReplayResponseBytes is the largest handler body accepted for replay.
	MaxReplayResponseBytes   = 700 * 1024
	replaySchema             = 1
	defaultMaxBytes          = 64 * 1024
	defaultTransitionTimeout = 5 * time.Second
)

// ErrResponseTooLarge is returned to a handler whose body crosses its limit.
var ErrResponseTooLarge = errors.New("idempotencyhttp: response exceeds replay limit")

// KeyFunc maps a request and header value to an application-scoped key.
type KeyFunc func(*http.Request, string) (idempotency.Key, error)

// FingerprintFunc computes the application's canonical request fingerprint.
type FingerprintFunc func(*http.Request) (idempotency.Fingerprint, error)

// Options configures durable HTTP handler ownership and replay.
type Options struct {
	// Service owns the durable state machine.
	Service *idempotency.Service
	// Lease bounds one handler owner's authority.
	Lease time.Duration
	// MaxResponseBytes bounds the buffered handler body. Zero defaults to 64 KiB.
	MaxResponseBytes int
	// ReplayHeaders names response headers to persist and replay.
	ReplayHeaders []string
	// TransitionTimeout bounds detached panic cleanup. Zero defaults to five seconds.
	TransitionTimeout time.Duration
	// Key constructs the fully scoped semantic key.
	Key KeyFunc
	// Fingerprint supplies canonical business-request identity.
	Fingerprint FingerprintFunc
}

// Middleware durably elects a handler and replays its bounded response.
type Middleware struct {
	service           *idempotency.Service
	lease             time.Duration
	maxResponseBytes  int
	transitionTimeout time.Duration
	replayHeaders     []string
	key               KeyFunc
	fingerprint       FingerprintFunc
}

// New validates options and constructs middleware.
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
	if options.MaxResponseBytes < 0 || options.MaxResponseBytes > MaxReplayResponseBytes {
		return nil, configurationError("max_response_bytes")
	}
	if options.TransitionTimeout == 0 {
		options.TransitionTimeout = defaultTransitionTimeout
	}
	if options.TransitionTimeout < 0 {
		return nil, configurationError("transition_timeout")
	}
	headers := make([]string, 0, len(options.ReplayHeaders))
	seen := make(map[string]struct{}, len(options.ReplayHeaders))
	for _, header := range options.ReplayHeaders {
		header = http.CanonicalHeaderKey(strings.TrimSpace(header))
		if header == "" {
			return nil, configurationError("replay_headers")
		}
		if _, exists := seen[header]; exists {
			continue
		}
		seen[header] = struct{}{}
		headers = append(headers, header)
	}
	return &Middleware{
		service: options.Service, lease: options.Lease,
		maxResponseBytes:  options.MaxResponseBytes,
		transitionTimeout: options.TransitionTimeout,
		replayHeaders:     headers, key: options.Key, fingerprint: options.Fingerprint,
	}, nil
}

// Handler wraps next with durable acquisition and response replay.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		m.serveHTTP(response, request, next)
	})
}

func (m *Middleware) serveHTTP(
	response http.ResponseWriter,
	request *http.Request,
	next http.Handler,
) {
	value := strings.TrimSpace(request.Header.Get(HeaderKey))
	if value == "" {
		writeOutcome(response, http.StatusBadRequest, idempotency.ReasonInvalidKey)
		return
	}
	key, err := m.key(request, value)
	if err != nil {
		writeOutcome(response, http.StatusBadRequest, reason(err))
		return
	}
	fingerprint, err := m.fingerprint(request)
	if err != nil {
		writeOutcome(response, http.StatusBadRequest, reason(err))
		return
	}
	begin, err := m.service.Begin(request.Context(), idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{Key: key, Fingerprint: fingerprint, Lease: m.lease},
	})
	if err != nil {
		writeOutcome(response, http.StatusServiceUnavailable, idempotency.OutcomeUnavailable)
		return
	}
	switch begin.Outcome {
	case idempotency.OutcomeReplayed, idempotency.OutcomeTerminalFailure:
		m.replay(response, begin)
	case idempotency.OutcomeInProgress:
		writeOutcome(response, http.StatusConflict, begin.Outcome)
	case idempotency.OutcomeConflict:
		writeOutcome(response, http.StatusConflict, begin.Outcome)
	case idempotency.OutcomeAcquired, idempotency.OutcomeStaleOwnerTakeover:
		m.execute(response, request, next, begin)
	}
}

func (m *Middleware) execute(
	response http.ResponseWriter,
	request *http.Request,
	next http.Handler,
	begin idempotency.BeginResult,
) {
	capture := &responseCapture{header: make(http.Header), limit: m.maxResponseBytes}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = m.release(request.Context(), begin.Record.Ownership())
			panic(recovered)
		}
	}()
	ownedRequest := request.WithContext(idempotency.WithOwnership(
		request.Context(), begin.Record.Ownership(),
	))
	next.ServeHTTP(capture, ownedRequest)

	if capture.oversized {
		m.failOversized(response, request, begin)
		return
	}
	snapshot := capture.snapshot(m.replayHeaders)
	encoded, _ := json.Marshal(snapshot)
	if len(encoded) > idempotency.MaxResultBytes {
		m.failOversized(response, request, begin)
		return
	}
	if _, err := m.service.Complete(request.Context(), idempotency.CompleteRequest{
		Ownership: begin.Record.Ownership(), Result: encoded,
	}); err != nil {
		writeOutcome(response, http.StatusServiceUnavailable, idempotency.OutcomeUnavailable)
		return
	}
	writeSnapshot(response, snapshot, begin.Outcome, false)
}

func (m *Middleware) release(ctx context.Context, ownership idempotency.Ownership) error {
	transitionCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), m.transitionTimeout,
	)
	defer cancel()
	_, err := m.service.Release(transitionCtx, ownership)
	return err
}

func (m *Middleware) failOversized(
	response http.ResponseWriter,
	request *http.Request,
	begin idempotency.BeginResult,
) {
	snapshot := responseSnapshot{
		Schema: replaySchema,
		Status: http.StatusInternalServerError,
		Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:   []byte("response exceeds idempotency replay limit\n"),
	}
	encoded, _ := json.Marshal(snapshot)
	if _, err := m.service.Fail(request.Context(), idempotency.FailRequest{
		Ownership: begin.Record.Ownership(), Result: encoded,
	}); err != nil {
		writeOutcome(response, http.StatusServiceUnavailable, idempotency.OutcomeUnavailable)
		return
	}
	writeSnapshot(response, snapshot, idempotency.OutcomeTerminalFailure, false)
}

func (m *Middleware) replay(response http.ResponseWriter, begin idempotency.BeginResult) {
	var snapshot responseSnapshot
	if err := json.Unmarshal(begin.Record.Result, &snapshot); err != nil || !snapshot.valid() {
		writeOutcome(response, http.StatusServiceUnavailable, idempotency.OutcomeUnavailable)
		return
	}
	writeSnapshot(response, snapshot, begin.Outcome, true)
}

type responseCapture struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	limit     int
	oversized bool
}

func (c *responseCapture) Header() http.Header { return c.header }

func (c *responseCapture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *responseCapture) Write(value []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len()+len(value) > c.limit {
		c.oversized = true
		return 0, ErrResponseTooLarge
	}
	return c.body.Write(value)
}

func (c *responseCapture) snapshot(headers []string) responseSnapshot {
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	snapshot := responseSnapshot{
		Schema: replaySchema, Status: status, Header: make(http.Header), Body: c.body.Bytes(),
	}
	for _, header := range headers {
		for _, value := range c.header.Values(header) {
			snapshot.Header.Add(header, value)
		}
	}
	return snapshot
}

type responseSnapshot struct {
	Schema int         `json:"schema"`
	Status int         `json:"status"`
	Header http.Header `json:"header,omitempty"`
	Body   []byte      `json:"body,omitempty"`
}

func (s responseSnapshot) valid() bool {
	return s.Schema == replaySchema && s.Status >= 100 && s.Status <= 999 &&
		len(s.Body) <= MaxReplayResponseBytes
}

func writeSnapshot(
	response http.ResponseWriter,
	snapshot responseSnapshot,
	outcome idempotency.Outcome,
	replayed bool,
) {
	for header, values := range snapshot.Header {
		for _, value := range values {
			response.Header().Add(header, value)
		}
	}
	response.Header().Set(HeaderOutcome, string(outcome))
	if replayed {
		response.Header().Set(HeaderReplayed, "true")
	}
	response.WriteHeader(snapshot.Status)
	_, _ = response.Write(snapshot.Body)
}

func writeOutcome[T ~string](response http.ResponseWriter, status int, outcome T) {
	response.Header().Set(HeaderOutcome, string(outcome))
	response.WriteHeader(status)
}

func reason(err error) idempotency.Reason {
	var semantic *idempotency.Error
	if errors.As(err, &semantic) {
		return semantic.Reason
	}
	return idempotency.ReasonInvalidPayload
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}
