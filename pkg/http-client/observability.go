package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
)

// ErrInvalidTelemetry indicates malformed telemetry adapters or header policy.
var ErrInvalidTelemetry = errors.New("invalid HTTP telemetry policy")

// TelemetryPhase identifies lifecycle start or completion.
type TelemetryPhase string

const (
	// TelemetryStart begins one operation or attempt.
	TelemetryStart TelemetryPhase = "start"
	// TelemetryFinish completes one operation or attempt.
	TelemetryFinish TelemetryPhase = "finish"
)

// TelemetryScope distinguishes logical operations from physical attempts.
type TelemetryScope string

const (
	// TelemetryOperation covers one complete logical Client.Do call.
	TelemetryOperation TelemetryScope = "operation"
	// TelemetryAttempt covers one physical RoundTrip.
	TelemetryAttempt TelemetryScope = "attempt"
)

// TelemetryOutcome is a stable closed completion category.
type TelemetryOutcome string

const (
	// TelemetryOutcomeSuccess indicates a response below status 400.
	TelemetryOutcomeSuccess TelemetryOutcome = "success"
	// TelemetryOutcomeHTTPError indicates a response status of 400 or greater.
	TelemetryOutcomeHTTPError TelemetryOutcome = "http_error"
	// TelemetryOutcomeTransport indicates transport failure before a response.
	TelemetryOutcomeTransport TelemetryOutcome = "transport_error"
	// TelemetryOutcomeCanceled indicates cancellation or deadline expiry.
	TelemetryOutcomeCanceled TelemetryOutcome = "canceled"
	// TelemetryOutcomeRateLimited indicates local admission rejection.
	TelemetryOutcomeRateLimited TelemetryOutcome = "rate_limited"
	// TelemetryOutcomeCircuitOpen indicates circuit admission rejection.
	TelemetryOutcomeCircuitOpen TelemetryOutcome = "circuit_open"
	// TelemetryOutcomeRetryFailure indicates exhausted retry policy.
	TelemetryOutcomeRetryFailure TelemetryOutcome = "retry_exhausted"
	// TelemetryOutcomeFailure indicates another bounded failure category.
	TelemetryOutcomeFailure TelemetryOutcome = "failure"
)

// TelemetryCacheOutcome is a stable closed cache category.
type TelemetryCacheOutcome string

const (
	// TelemetryCacheNone indicates no cache metadata.
	TelemetryCacheNone TelemetryCacheOutcome = "none"
	// TelemetryCacheMiss indicates transport policy supplied the response.
	TelemetryCacheMiss TelemetryCacheOutcome = "miss"
	// TelemetryCacheHit indicates a fresh stored response.
	TelemetryCacheHit TelemetryCacheOutcome = "hit"
	// TelemetryCacheRevalidated indicates a stored response freshened by 304.
	TelemetryCacheRevalidated TelemetryCacheOutcome = "revalidated"
	// TelemetryCacheStale indicates explicitly permitted stale reuse.
	TelemetryCacheStale TelemetryCacheOutcome = "stale"
)

// TelemetryEvent contains fixed bounded fields. It never contains URLs,
// headers, bodies, credentials, tenant identifiers, cursors, or error text.
type TelemetryEvent struct {
	Phase       TelemetryPhase
	Scope       TelemetryScope
	Attempt     int
	OperationID string
	Method      string
	Profile     PolicyProfileID
	Outcome     TelemetryOutcome
	StatusClass string
	Cache       TelemetryCacheOutcome
}

// TelemetryMetricLabels is a closed low-cardinality projection. It excludes
// operation identity and any raw request, response, or error data.
type TelemetryMetricLabels struct {
	Scope       TelemetryScope
	Method      string
	Profile     PolicyProfileID
	Outcome     TelemetryOutcome
	StatusClass string
	Cache       TelemetryCacheOutcome
}

// MetricLabels returns fields safe for use as bounded metric labels.
func (event TelemetryEvent) MetricLabels() TelemetryMetricLabels {
	return TelemetryMetricLabels{
		Scope: event.Scope, Method: telemetryMethod(event.Method),
		Profile: event.Profile, Outcome: event.Outcome,
		StatusClass: event.StatusClass, Cache: event.Cache,
	}
}

// TelemetryObserver creates derived trace contexts and receives completions.
// Implementations must be safe for concurrent use.
type TelemetryObserver interface {
	Start(context.Context, TelemetryEvent) context.Context
	Finish(context.Context, TelemetryEvent)
}

// TelemetryPropagator injects trace context into a cloned physical-attempt
// header. Implementations must be safe for concurrent use.
type TelemetryPropagator interface {
	Inject(context.Context, http.Header)
}

// TelemetryOptions configures optional observation, propagation, and strict
// trust-boundary header handling.
type TelemetryOptions struct {
	Observer          TelemetryObserver
	Propagator        TelemetryPropagator
	CorrelationHeader string
	BaggageAllowlist  []string
	SensitiveHeaders  []string
}

type telemetryPolicy struct {
	observer          TelemetryObserver
	propagator        TelemetryPropagator
	correlationHeader string
	baggage           map[string]struct{}
	sensitiveHeaders  []string
}

type telemetryOperationState struct {
	origin   string
	attempts atomic.Int64
}

func newTelemetryMiddleware(options *TelemetryOptions) ([]Middleware, error) {
	if options == nil {
		return nil, nil
	}
	if options.Observer == nil && options.Propagator == nil {
		return nil, ErrInvalidTelemetry
	}
	if options.Observer != nil && nilLike(options.Observer) ||
		options.Propagator != nil && nilLike(options.Propagator) {
		return nil, ErrInvalidTelemetry
	}
	correlation := options.CorrelationHeader
	if correlation == "" {
		correlation = "X-Request-ID"
	}
	canonicalCorrelation, err := validateHeaderName(correlation)
	if err != nil {
		return nil, ErrInvalidTelemetry
	}
	policy := telemetryPolicy{
		observer: options.Observer, propagator: options.Propagator,
		correlationHeader: canonicalCorrelation,
		baggage:           make(map[string]struct{}, len(options.BaggageAllowlist)),
		sensitiveHeaders:  []string{"Authorization", "Cookie", "Proxy-Authorization"},
	}
	for _, name := range options.BaggageAllowlist {
		name = strings.ToLower(strings.TrimSpace(name))
		if !validBaggageName(name) {
			return nil, ErrInvalidTelemetry
		}
		policy.baggage[name] = struct{}{}
	}
	for _, name := range options.SensitiveHeaders {
		canonical, nameErr := validateHeaderName(name)
		if nameErr != nil {
			return nil, ErrInvalidTelemetry
		}
		policy.sensitiveHeaders = append(policy.sensitiveHeaders, canonical)
	}
	operation, _ := NewRequestMiddleware(MiddlewareOptions{
		Name: "httpclient.telemetry.operation", Scope: ScopeOperation,
		Layer: MiddlewareClient, Priority: -1900,
	}, policy.observeOperation)
	attempt, _ := NewRequestMiddleware(MiddlewareOptions{
		Name: "httpclient.telemetry.attempt", Scope: ScopeAttempt,
		Layer: MiddlewareClient, Priority: -1900,
	}, policy.observeAttempt)
	return []Middleware{operation, attempt}, nil
}

func validBaggageName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for index := range len(name) {
		if !headerTokenByte(name[index]) {
			return false
		}
	}
	return true
}

func (policy telemetryPolicy) observeOperation(request *http.Request, next Next) (*http.Response, error) {
	identity, _ := OperationIdentityFromContext(request.Context())
	profile, _ := ResolvedPolicyFromContext(request.Context())
	start := TelemetryEvent{
		Phase: TelemetryStart, Scope: TelemetryOperation, OperationID: identity.ID,
		Method: telemetryMethod(request.Method), Profile: profile.Profile(), Cache: TelemetryCacheNone,
	}
	ctx := policy.start(request.Context(), start)
	origin, _ := canonicalOrigin(request.URL)
	ctx = context.WithValue(ctx, telemetryOperationContextKey{}, &telemetryOperationState{origin: origin})
	response, failure := next(request.WithContext(ctx))
	policy.finish(ctx, finishTelemetryEvent(start, response, failure))
	return response, failure
}

func (policy telemetryPolicy) observeAttempt(request *http.Request, next Next) (*http.Response, error) {
	state, _ := request.Context().Value(telemetryOperationContextKey{}).(*telemetryOperationState)
	attempt := 1
	if state != nil {
		attempt = int(state.attempts.Add(1))
	}
	identity, _ := OperationIdentityFromContext(request.Context())
	profile, _ := ResolvedPolicyFromContext(request.Context())
	start := TelemetryEvent{
		Phase: TelemetryStart, Scope: TelemetryAttempt, Attempt: attempt,
		OperationID: identity.ID, Method: telemetryMethod(request.Method),
		Profile: profile.Profile(), Cache: TelemetryCacheNone,
	}
	ctx := policy.start(request.Context(), start)
	clone := request.Clone(ctx)
	policy.prepareAttemptHeaders(clone, state)
	response, failure := next(clone)
	policy.finish(ctx, finishTelemetryEvent(start, response, failure))
	return response, failure
}

func (policy telemetryPolicy) prepareAttemptHeaders(request *http.Request, state *telemetryOperationState) {
	identity, _ := OperationIdentityFromContext(request.Context())
	request.Header.Set(policy.correlationHeader, identity.ID)
	crossedBoundary := false
	if state != nil {
		origin, _ := canonicalOrigin(request.URL)
		if origin != state.origin {
			crossedBoundary = true
			for _, name := range policy.sensitiveHeaders {
				request.Header.Del(name)
			}
			request.Header.Del("Traceparent")
			request.Header.Del("Tracestate")
			request.Header.Del("Baggage")
		}
	}
	filterBaggage(request.Header, policy.baggage)
	if !crossedBoundary {
		policy.inject(request.Context(), request.Header)
		filterBaggage(request.Header, policy.baggage)
	}
}

func (policy telemetryPolicy) start(ctx context.Context, event TelemetryEvent) (derived context.Context) {
	derived = ctx
	if policy.observer == nil {
		return derived
	}
	defer func() { _ = recover() }()
	if observed := policy.observer.Start(ctx, event); observed != nil {
		derived = observed
	}
	return derived
}

func (policy telemetryPolicy) finish(ctx context.Context, event TelemetryEvent) {
	if policy.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	policy.observer.Finish(ctx, event)
}

func (policy telemetryPolicy) inject(ctx context.Context, header http.Header) {
	if policy.propagator == nil {
		return
	}
	defer func() { _ = recover() }()
	policy.propagator.Inject(ctx, header)
}

func finishTelemetryEvent(start TelemetryEvent, response *http.Response, failure error) TelemetryEvent {
	event := start
	event.Phase = TelemetryFinish
	event.Outcome = telemetryOutcome(response, failure)
	if response != nil {
		event.StatusClass = statusClass(response.StatusCode)
		if metadata, ok := CacheMetadataFromResponse(response); ok {
			event.Cache = telemetryCacheOutcome(metadata.Provenance)
		}
	}
	return event
}

func telemetryOutcome(response *http.Response, failure error) TelemetryOutcome {
	switch {
	case errors.Is(failure, context.Canceled), errors.Is(failure, context.DeadlineExceeded):
		return TelemetryOutcomeCanceled
	case errors.Is(failure, ErrRateLimitCapacity), errors.Is(failure, ErrRateLimitWaitExceeded):
		return TelemetryOutcomeRateLimited
	case errors.Is(failure, ErrCircuitRejected):
		return TelemetryOutcomeCircuitOpen
	case errors.Is(failure, ErrRetryExhausted):
		return TelemetryOutcomeRetryFailure
	case failure != nil:
		var transport *TransportError
		if errors.As(failure, &transport) {
			return TelemetryOutcomeTransport
		}
		return TelemetryOutcomeFailure
	case response != nil && response.StatusCode >= http.StatusBadRequest:
		return TelemetryOutcomeHTTPError
	default:
		return TelemetryOutcomeSuccess
	}
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "invalid"
	}
	return string(rune('0'+status/100)) + "xx"
}

func telemetryMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

func telemetryCacheOutcome(provenance CacheProvenance) TelemetryCacheOutcome {
	switch provenance {
	case CacheMiss:
		return TelemetryCacheMiss
	case CacheHit:
		return TelemetryCacheHit
	case CacheRevalidated:
		return TelemetryCacheRevalidated
	case CacheStale:
		return TelemetryCacheStale
	default:
		return TelemetryCacheNone
	}
}

func filterBaggage(header http.Header, allowlist map[string]struct{}) {
	var allowed []string
	for _, value := range header.Values("Baggage") {
		for _, member := range strings.Split(value, ",") {
			member = strings.TrimSpace(member)
			name, _, found := strings.Cut(member, "=")
			if !found {
				continue
			}
			if _, ok := allowlist[strings.ToLower(strings.TrimSpace(name))]; ok {
				allowed = append(allowed, member)
			}
		}
	}
	header.Del("Baggage")
	if len(allowed) > 0 {
		header.Set("Baggage", strings.Join(allowed, ", "))
	}
}

type telemetryOperationContextKey struct{}
