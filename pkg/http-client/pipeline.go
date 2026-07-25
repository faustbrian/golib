package httpclient

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
)

var (
	// ErrInvalidMiddleware indicates invalid middleware metadata or behavior.
	ErrInvalidMiddleware = errors.New("invalid HTTP middleware")
	// ErrInvalidMiddlewareResult indicates an impossible response/error pair.
	ErrInvalidMiddlewareResult = errors.New("invalid HTTP middleware result")
)

// MiddlewareScope determines whether middleware runs once for a logical
// operation or once for every physical transport attempt.
type MiddlewareScope uint8

const (
	// ScopeOperation runs once around the complete logical operation.
	ScopeOperation MiddlewareScope = iota
	// ScopeAttempt runs for every physical transport attempt.
	ScopeAttempt
	middlewareScopeCount
)

// MiddlewareLayer identifies where middleware was registered. Higher layers
// replace same-named middleware from lower layers at the same stage and scope.
type MiddlewareLayer uint8

const (
	// MiddlewareClient contains client-wide middleware.
	MiddlewareClient MiddlewareLayer = iota
	// MiddlewareEndpoint contains endpoint-specific middleware.
	MiddlewareEndpoint
	// MiddlewareRequest contains logical-request middleware.
	MiddlewareRequest
	// MiddlewareOneShot contains middleware for one invocation.
	MiddlewareOneShot
	middlewareLayerCount
)

// MiddlewareStage identifies one deterministic lifecycle stage.
type MiddlewareStage uint8

const (
	// StageRequest can mutate or reject a request before transport policy.
	StageRequest MiddlewareStage = iota
	// StageTransport surrounds the next inner scope or physical transport.
	StageTransport
	// StageResponse observes and may replace a successful response.
	StageResponse
	// StageError observes and may recover a failed exchange.
	StageError
	// StageCompletion always observes the final scope result.
	StageCompletion
	middlewareStageCount
)

// MiddlewareOptions supplies stable resolution metadata.
type MiddlewareOptions struct {
	Name     string
	Scope    MiddlewareScope
	Layer    MiddlewareLayer
	Priority int
}

// MiddlewareInfo is an immutable inspection record for resolved middleware.
type MiddlewareInfo struct {
	Name     string
	Scope    MiddlewareScope
	Layer    MiddlewareLayer
	Stage    MiddlewareStage
	Priority int
}

// PipelineInspection contains resolved operation and attempt plans. Entries in
// each plan are ordered by stage, priority, layer, and name.
type PipelineInspection struct {
	Operation []MiddlewareInfo
	Attempt   []MiddlewareInfo
}

// Next continues an around-middleware chain with request.
type Next func(request *http.Request) (*http.Response, error)

// AroundMiddlewareFunc handles request or transport stages.
type AroundMiddlewareFunc func(request *http.Request, next Next) (*http.Response, error)

// ResponseMiddlewareFunc observes or replaces a response.
type ResponseMiddlewareFunc func(request *http.Request, response *http.Response) (*http.Response, error)

// ErrorMiddlewareFunc observes an error and may return a recovered response.
type ErrorMiddlewareFunc func(request *http.Request, failure error) (*http.Response, error)

// CompletionMiddlewareFunc observes the final response and error for a scope.
type CompletionMiddlewareFunc func(request *http.Request, response *http.Response, failure error) error

// TransportFunc adapts a function to http.RoundTripper.
type TransportFunc func(request *http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (function TransportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// MiddlewarePanicError reports a contained middleware or transport panic. The
// panic value remains available to the caller but is not rendered.
type MiddlewarePanicError struct {
	Middleware MiddlewareInfo
	Value      any
}

// Error implements error without rendering the panic value.
func (err *MiddlewarePanicError) Error() string {
	return fmt.Sprintf(
		"HTTP %s middleware %q panicked",
		err.Middleware.Stage,
		err.Middleware.Name,
	)
}

// MiddlewareExecutionError reports a failure returned by response, error, or
// completion middleware while preserving its cause.
type MiddlewareExecutionError struct {
	Middleware MiddlewareInfo
	Cause      error
}

// MiddlewareResultError reports an invalid response/error combination without
// rendering an underlying failure or response-close error.
type MiddlewareResultError struct {
	Reason string
	Cause  error
}

// Error implements error.
func (err *MiddlewareResultError) Error() string {
	return fmt.Sprintf("invalid HTTP middleware result: %s", err.Reason)
}

// Unwrap preserves the stable sentinel and the underlying cause.
func (err *MiddlewareResultError) Unwrap() []error {
	errors := []error{ErrInvalidMiddlewareResult}
	if err.Cause != nil {
		errors = append(errors, err.Cause)
	}

	return errors
}

// Error implements error without rendering the cause.
func (err *MiddlewareExecutionError) Error() string {
	return fmt.Sprintf(
		"HTTP %s middleware %q failed",
		err.Middleware.Stage,
		err.Middleware.Name,
	)
}

// Unwrap returns the middleware failure.
func (err *MiddlewareExecutionError) Unwrap() error {
	return err.Cause
}

// Middleware is one immutable pipeline registration. Construct values with a
// stage-specific constructor.
type Middleware struct {
	information MiddlewareInfo
	around      AroundMiddlewareFunc
	response    ResponseMiddlewareFunc
	error       ErrorMiddlewareFunc
	completion  CompletionMiddlewareFunc
}

// NewRequestMiddleware constructs request-stage middleware.
func NewRequestMiddleware(options MiddlewareOptions, handler AroundMiddlewareFunc) (Middleware, error) {
	return newAroundMiddleware(options, StageRequest, handler)
}

// NewTransportMiddleware constructs transport-stage middleware.
func NewTransportMiddleware(options MiddlewareOptions, handler AroundMiddlewareFunc) (Middleware, error) {
	return newAroundMiddleware(options, StageTransport, handler)
}

// NewResponseMiddleware constructs response-stage middleware.
func NewResponseMiddleware(options MiddlewareOptions, handler ResponseMiddlewareFunc) (Middleware, error) {
	if nilLike(handler) {
		return Middleware{}, fmt.Errorf("%w: response handler is nil", ErrInvalidMiddleware)
	}
	middleware, err := newMiddleware(options, StageResponse)
	if err != nil {
		return Middleware{}, err
	}
	middleware.response = handler

	return middleware, nil
}

// NewErrorMiddleware constructs error-stage middleware.
func NewErrorMiddleware(options MiddlewareOptions, handler ErrorMiddlewareFunc) (Middleware, error) {
	if nilLike(handler) {
		return Middleware{}, fmt.Errorf("%w: error handler is nil", ErrInvalidMiddleware)
	}
	middleware, err := newMiddleware(options, StageError)
	if err != nil {
		return Middleware{}, err
	}
	middleware.error = handler

	return middleware, nil
}

// NewCompletionMiddleware constructs completion-stage middleware.
func NewCompletionMiddleware(options MiddlewareOptions, handler CompletionMiddlewareFunc) (Middleware, error) {
	if nilLike(handler) {
		return Middleware{}, fmt.Errorf("%w: completion handler is nil", ErrInvalidMiddleware)
	}
	middleware, err := newMiddleware(options, StageCompletion)
	if err != nil {
		return Middleware{}, err
	}
	middleware.completion = handler

	return middleware, nil
}

func newAroundMiddleware(options MiddlewareOptions, stage MiddlewareStage, handler AroundMiddlewareFunc) (Middleware, error) {
	if nilLike(handler) {
		return Middleware{}, fmt.Errorf("%w: around handler is nil", ErrInvalidMiddleware)
	}
	middleware, err := newMiddleware(options, stage)
	if err != nil {
		return Middleware{}, err
	}
	middleware.around = handler

	return middleware, nil
}

func newMiddleware(options MiddlewareOptions, stage MiddlewareStage) (Middleware, error) {
	if !validMiddlewareName(options.Name) {
		return Middleware{}, fmt.Errorf("%w: name must be a stable lowercase token", ErrInvalidMiddleware)
	}
	if options.Scope >= middlewareScopeCount {
		return Middleware{}, fmt.Errorf("%w: unknown scope %d", ErrInvalidMiddleware, options.Scope)
	}
	if options.Layer >= middlewareLayerCount {
		return Middleware{}, fmt.Errorf("%w: unknown layer %d", ErrInvalidMiddleware, options.Layer)
	}
	if stage >= middlewareStageCount {
		return Middleware{}, fmt.Errorf("%w: unknown stage %d", ErrInvalidMiddleware, stage)
	}

	return Middleware{information: MiddlewareInfo{
		Name:     options.Name,
		Scope:    options.Scope,
		Layer:    options.Layer,
		Stage:    stage,
		Priority: options.Priority,
	}}, nil
}

// NewPipeline resolves middleware into an immutable pipeline.
func NewPipeline(middleware ...Middleware) (Pipeline, error) {
	registrations := append([]Middleware(nil), middleware...)

	return resolvePipeline(registrations)
}

// Pipeline is an immutable resolved middleware plan. Its zero value is an
// empty valid pipeline.
type Pipeline struct {
	registrations []Middleware
	operation     []Middleware
	attempt       []Middleware
}

// With returns a new pipeline containing middleware. The receiver remains
// unchanged.
func (pipeline Pipeline) With(middleware ...Middleware) (Pipeline, error) {
	registrations := make([]Middleware, 0, len(pipeline.registrations)+len(middleware))
	registrations = append(registrations, pipeline.registrations...)
	registrations = append(registrations, middleware...)

	return resolvePipeline(registrations)
}

// Inspect returns independent copies of the resolved operation and attempt
// plans.
func (pipeline Pipeline) Inspect() PipelineInspection {
	return PipelineInspection{
		Operation: middlewareInformation(pipeline.operation),
		Attempt:   middlewareInformation(pipeline.attempt),
	}
}

// Execute runs the logical operation and every physical attempt through the
// resolved pipeline before delegating to transport.
func (pipeline Pipeline) Execute(request *http.Request, transport http.RoundTripper) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidMiddlewareResult)
	}
	if nilLike(transport) {
		return nil, fmt.Errorf("%w: transport is nil", ErrInvalidMiddlewareResult)
	}

	return pipeline.executeOperation(request, func(attemptRequest *http.Request) (*http.Response, error) {
		return pipeline.executeAttempt(attemptRequest, transport.RoundTrip)
	})
}

func (pipeline Pipeline) executeOperation(request *http.Request, terminal Next) (*http.Response, error) {
	operationRequest := request.Clone(request.Context())

	return executeMiddlewareScope(pipeline.operation, ScopeOperation, operationRequest, terminal)
}

func (pipeline Pipeline) executeAttempt(request *http.Request, terminal Next) (*http.Response, error) {
	attemptRequest := request.Clone(request.Context())

	return executeMiddlewareScope(pipeline.attempt, ScopeAttempt, attemptRequest, terminal)
}

func executeMiddlewareScope(
	middleware []Middleware,
	scope MiddlewareScope,
	request *http.Request,
	terminal Next,
) (*http.Response, error) {
	lastRequest := request
	terminalInvoked := false
	around := middlewareAtAroundStages(middleware)
	next := func(current *http.Request) (*http.Response, error) {
		if current == nil {
			return nil, fmt.Errorf("%w: middleware passed a nil request", ErrInvalidMiddlewareResult)
		}
		lastRequest = current
		if err := current.Context().Err(); err != nil {
			return nil, err
		}
		terminalInvoked = true

		return invokeTerminal(scope, current, terminal)
	}
	for index := len(around) - 1; index >= 0; index-- {
		item := around[index]
		inner := next
		next = func(current *http.Request) (*http.Response, error) {
			if current == nil {
				return nil, fmt.Errorf("%w: middleware passed a nil request", ErrInvalidMiddlewareResult)
			}
			lastRequest = current
			if err := current.Context().Err(); err != nil {
				return nil, err
			}

			return invokeAround(item, current, inner)
		}
	}

	response, failure := next(request)
	if !terminalInvoked && lastRequest != nil && lastRequest.Body != nil {
		// A short circuit assumes the RoundTripper's request-body ownership
		// without reaching the terminal that would normally close it.
		_ = lastRequest.Body.Close()
	}
	response, failure = normalizeMiddlewareResult(lastRequest, response, failure)
	snapshot := snapshotRequest(lastRequest)

	if failure == nil && response != nil {
		response, failure = runResponseMiddleware(middleware, snapshot, response)
	}
	if failure != nil {
		response, failure = runErrorMiddleware(middleware, snapshot, failure)
	}
	response, failure = normalizeMiddlewareResult(lastRequest, response, failure)

	completionFailure := runCompletionMiddleware(middleware, snapshot, response, failure)
	if completionFailure != nil {
		if response != nil {
			completionFailure = errors.Join(completionFailure, closeResponse(response))
			response = nil
		}
		failure = errors.Join(failure, completionFailure)
	}

	return response, failure
}

func middlewareAtAroundStages(middleware []Middleware) []Middleware {
	around := make([]Middleware, 0, len(middleware))
	for _, item := range middleware {
		if item.information.Stage == StageRequest || item.information.Stage == StageTransport {
			around = append(around, item)
		}

	}

	return around
}

func invokeAround(middleware Middleware, request *http.Request, next Next) (response *http.Response, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = nil
			failure = &MiddlewarePanicError{Middleware: middleware.information, Value: recovered}
		}
	}()

	return middleware.around(request, next)
}

func invokeTerminal(scope MiddlewareScope, request *http.Request, terminal Next) (response *http.Response, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = nil
			failure = &MiddlewarePanicError{
				Middleware: MiddlewareInfo{
					Name:  "net-http-transport",
					Scope: scope,
					Layer: MiddlewareClient,
					Stage: StageTransport,
				},
				Value: recovered,
			}
		}
	}()

	return terminal(request)
}

func runResponseMiddleware(
	middleware []Middleware,
	request *http.Request,
	response *http.Response,
) (*http.Response, error) {
	current := response
	for _, item := range middleware {
		if item.information.Stage != StageResponse {
			continue
		}
		replacement, err := invokeResponse(item, snapshotRequest(request), current)
		if err != nil {
			return nil, errors.Join(
				&MiddlewareExecutionError{Middleware: item.information, Cause: err},
				closeResponse(current),
			)
		}
		if replacement == nil {
			return nil, &MiddlewareResultError{
				Reason: fmt.Sprintf("response middleware %q returned nil", item.information.Name),
				Cause:  closeResponse(current),
			}
		}
		if replacement != current {
			if err := closeResponse(current); err != nil {
				return nil, errors.Join(
					&MiddlewareExecutionError{Middleware: item.information, Cause: err},
					closeResponse(replacement),
				)
			}
		}
		current = normalizeResponse(request, replacement)
	}

	return current, nil
}

func invokeResponse(
	middleware Middleware,
	request *http.Request,
	response *http.Response,
) (replacement *http.Response, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			replacement = nil
			failure = &MiddlewarePanicError{Middleware: middleware.information, Value: recovered}
		}
	}()

	return middleware.response(request, response)
}

func runErrorMiddleware(
	middleware []Middleware,
	request *http.Request,
	failure error,
) (*http.Response, error) {
	current := failure
	for _, item := range middleware {
		if item.information.Stage != StageError {
			continue
		}
		response, nextFailure := invokeError(item, snapshotRequest(request), current)
		if nextFailure == nil && response != nil {
			return normalizeResponse(request, response), nil
		}
		if nextFailure == nil {
			return nil, &MiddlewareResultError{
				Reason: fmt.Sprintf("error middleware %q swallowed failure without a response", item.information.Name),
			}
		}
		if response != nil {
			nextFailure = errors.Join(nextFailure, closeResponse(response))
		}
		current = &MiddlewareExecutionError{Middleware: item.information, Cause: nextFailure}
	}

	return nil, current
}

func invokeError(
	middleware Middleware,
	request *http.Request,
	failure error,
) (response *http.Response, nextFailure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = nil
			nextFailure = &MiddlewarePanicError{Middleware: middleware.information, Value: recovered}
		}
	}()

	return middleware.error(request, failure)
}

func runCompletionMiddleware(
	middleware []Middleware,
	request *http.Request,
	response *http.Response,
	failure error,
) error {
	var completionFailures []error
	for _, item := range middleware {
		if item.information.Stage != StageCompletion {
			continue
		}
		if err := invokeCompletion(item, snapshotRequest(request), response, failure); err != nil {
			completionFailures = append(completionFailures, &MiddlewareExecutionError{
				Middleware: item.information,
				Cause:      err,
			})
		}
	}

	return errors.Join(completionFailures...)
}

func invokeCompletion(
	middleware Middleware,
	request *http.Request,
	response *http.Response,
	failure error,
) (completionFailure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			completionFailure = &MiddlewarePanicError{Middleware: middleware.information, Value: recovered}
		}
	}()

	return middleware.completion(request, response, failure)
}

func normalizeMiddlewareResult(
	request *http.Request,
	response *http.Response,
	failure error,
) (*http.Response, error) {
	if response == nil && failure == nil {
		return nil, ErrInvalidMiddlewareResult
	}
	if response != nil && failure != nil {
		return nil, &MiddlewareResultError{
			Reason: "response and error returned together",
			Cause:  errors.Join(failure, closeResponse(response)),
		}
	}
	if response != nil {
		return normalizeResponse(request, response), nil
	}

	return nil, failure
}

func normalizeResponse(request *http.Request, response *http.Response) *http.Response {
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	if response.Request == nil {
		response.Request = request
	}

	return response
}

func closeResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}

	return response.Body.Close()
}

func snapshotRequest(request *http.Request) *http.Request {
	if request == nil {
		return nil
	}

	snapshot := request.Clone(request.Context())
	if snapshot.Body != nil {
		snapshot.Body = http.NoBody
	}
	snapshot.GetBody = nil

	return snapshot
}

func resolvePipeline(registrations []Middleware) (Pipeline, error) {
	type identity struct {
		name  string
		scope MiddlewareScope
		stage MiddlewareStage
	}
	type registrationIdentity struct {
		identity
		layer MiddlewareLayer
	}

	seen := make(map[registrationIdentity]struct{}, len(registrations))
	selected := make(map[identity]Middleware, len(registrations))
	for _, middleware := range registrations {
		if err := validateMiddleware(middleware); err != nil {
			return Pipeline{}, err
		}
		key := identity{
			name:  middleware.information.Name,
			scope: middleware.information.Scope,
			stage: middleware.information.Stage,
		}
		registrationKey := registrationIdentity{identity: key, layer: middleware.information.Layer}
		if _, exists := seen[registrationKey]; exists {
			return Pipeline{}, fmt.Errorf(
				"%w: duplicate %s middleware %q at layer %d",
				ErrInvalidMiddleware,
				middleware.information.Stage,
				middleware.information.Name,
				middleware.information.Layer,
			)
		}
		seen[registrationKey] = struct{}{}
		current, exists := selected[key]
		if !exists || middleware.information.Layer > current.information.Layer {
			selected[key] = middleware
		}
	}

	resolved := Pipeline{registrations: append([]Middleware(nil), registrations...)}
	for _, middleware := range selected {
		if middleware.information.Scope == ScopeOperation {
			resolved.operation = append(resolved.operation, middleware)
		} else {
			resolved.attempt = append(resolved.attempt, middleware)
		}
	}
	sortMiddleware(resolved.operation)
	sortMiddleware(resolved.attempt)

	return resolved, nil
}

func validateMiddleware(middleware Middleware) error {
	information := middleware.information
	if _, err := newMiddleware(MiddlewareOptions{
		Name:     information.Name,
		Scope:    information.Scope,
		Layer:    information.Layer,
		Priority: information.Priority,
	}, information.Stage); err != nil {
		return err
	}

	switch information.Stage {
	case StageRequest, StageTransport:
		if nilLike(middleware.around) {
			return fmt.Errorf("%w: around handler is nil", ErrInvalidMiddleware)
		}
	case StageResponse:
		if nilLike(middleware.response) {
			return fmt.Errorf("%w: response handler is nil", ErrInvalidMiddleware)
		}
	case StageError:
		if nilLike(middleware.error) {
			return fmt.Errorf("%w: error handler is nil", ErrInvalidMiddleware)
		}
	case StageCompletion:
		if nilLike(middleware.completion) {
			return fmt.Errorf("%w: completion handler is nil", ErrInvalidMiddleware)
		}
	}

	return nil
}

func validMiddlewareName(name string) bool {
	if len(name) == 0 || len(name) > 64 || !lowerAlphaNumeric(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !lowerAlphaNumeric(character) && character != '.' && character != '_' &&
			character != '/' && character != '-' {
			return false
		}
	}

	return true
}

func lowerAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func sortMiddleware(middleware []Middleware) {
	sort.Slice(middleware, func(left int, right int) bool {
		leftInfo := middleware[left].information
		rightInfo := middleware[right].information
		if leftInfo.Stage != rightInfo.Stage {
			return leftInfo.Stage < rightInfo.Stage
		}
		if leftInfo.Priority != rightInfo.Priority {
			return leftInfo.Priority < rightInfo.Priority
		}
		if leftInfo.Layer != rightInfo.Layer {
			return leftInfo.Layer < rightInfo.Layer
		}

		return leftInfo.Name < rightInfo.Name
	})
}

func middlewareInformation(middleware []Middleware) []MiddlewareInfo {
	information := make([]MiddlewareInfo, len(middleware))
	for index, item := range middleware {
		information[index] = item.information
	}

	return information
}

func (stage MiddlewareStage) String() string {
	switch stage {
	case StageRequest:
		return "request"
	case StageTransport:
		return "transport"
	case StageResponse:
		return "response"
	case StageError:
		return "error"
	case StageCompletion:
		return "completion"
	default:
		return fmt.Sprintf("stage(%d)", stage)
	}
}
