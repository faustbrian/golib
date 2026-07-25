package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

var (
	// ErrInvalidCircuitBreaker indicates malformed breaker integration policy.
	ErrInvalidCircuitBreaker = errors.New("invalid HTTP circuit breaker policy")
	// ErrCircuitRejected indicates fail-fast rejection before network execution.
	ErrCircuitRejected = errors.New("HTTP circuit breaker rejected operation")
)

const circuitBreakerMiddlewarePriority = -600

// CircuitBreaker executes one complete logical HTTP operation. Implementations
// own admission, half-open probes, state, and outcome accounting.
type CircuitBreaker interface {
	Execute(
		context.Context,
		func(context.Context) (*http.Response, error),
	) (*http.Response, error)
}

// CircuitBreakerFunc adapts a function to CircuitBreaker.
type CircuitBreakerFunc func(
	context.Context,
	func(context.Context) (*http.Response, error),
) (*http.Response, error)

// Execute implements CircuitBreaker.
func (function CircuitBreakerFunc) Execute(
	ctx context.Context,
	operation func(context.Context) (*http.Response, error),
) (*http.Response, error) {
	return function(ctx, operation)
}

// CircuitBreakerOptions configures logical-operation breaker middleware.
type CircuitBreakerOptions struct {
	Name     string
	Layer    MiddlewareLayer
	Priority int
	Breaker  CircuitBreaker
}

// CircuitBreakerError reports fail-fast rejection without rendering breaker
// names, state details, retry timestamps, or underlying causes.
type CircuitBreakerError struct {
	Cause error
}

// Error implements error without rendering potentially sensitive causes.
func (*CircuitBreakerError) Error() string {
	return "HTTP circuit breaker rejected operation"
}

// Unwrap preserves the stable rejection sentinel and provider cause.
func (err *CircuitBreakerError) Unwrap() []error {
	causes := []error{ErrCircuitRejected}
	if err.Cause != nil {
		causes = append(causes, err.Cause)
	}

	return causes
}

// NewCircuitBreakerMiddleware creates operation transport middleware outside
// retry so one logical completion is recorded across all physical attempts.
func NewCircuitBreakerMiddleware(options CircuitBreakerOptions) (Middleware, error) {
	if nilLike(options.Breaker) {
		return Middleware{}, fmt.Errorf("%w: breaker is nil", ErrInvalidCircuitBreaker)
	}

	return NewTransportMiddleware(MiddlewareOptions{
		Name: options.Name, Scope: ScopeOperation, Layer: options.Layer,
		Priority: circuitBreakerMiddlewarePriority + options.Priority,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		response, err := options.Breaker.Execute(request.Context(), func(ctx context.Context) (*http.Response, error) {
			if ctx == nil {
				return nil, ErrInvalidCircuitBreaker
			}

			return next(request.WithContext(ctx))
		})
		if err != nil && errors.Is(err, ErrCircuitRejected) {
			closeErr := closeResponse(response)
			var rejection *CircuitBreakerError
			if errors.As(err, &rejection) && closeErr == nil {
				return nil, rejection
			}

			return nil, &CircuitBreakerError{Cause: errors.Join(err, closeErr)}
		}

		return response, err
	})
}

// CircuitOutcome is the HTTP classification recorded by breaker state.
type CircuitOutcome uint8

const (
	// CircuitOutcomeSuccess records a dependency success.
	CircuitOutcomeSuccess CircuitOutcome = iota
	// CircuitOutcomeFailure records a dependency failure.
	CircuitOutcomeFailure
	// CircuitOutcomeIgnored excludes a local or caller-controlled outcome.
	CircuitOutcomeIgnored
)

// CircuitOutcomeClassifier classifies one complete logical HTTP operation. It
// must not retain, mutate, consume, or close the response.
type CircuitOutcomeClassifier interface {
	Classify(*http.Response, error) CircuitOutcome
}

// ContextCircuitOutcomeClassifier can distinguish caller cancellation from a
// dependency that independently returns a cancellation-shaped error.
type ContextCircuitOutcomeClassifier interface {
	ClassifyContext(context.Context, *http.Response, error) CircuitOutcome
}

// CircuitOutcomeClassifierFunc adapts a function to a classifier.
type CircuitOutcomeClassifierFunc func(*http.Response, error) CircuitOutcome

// Classify implements CircuitOutcomeClassifier.
func (function CircuitOutcomeClassifierFunc) Classify(
	response *http.Response,
	failure error,
) CircuitOutcome {
	return function(response, failure)
}

type defaultCircuitOutcomeClassifier struct{}

// DefaultCircuitOutcomeClassifier classifies 5xx responses, deadlines, and
// other operation failures while ignoring caller cancellation and local rate
// rejection.
func DefaultCircuitOutcomeClassifier() CircuitOutcomeClassifier {
	return defaultCircuitOutcomeClassifier{}
}

func (defaultCircuitOutcomeClassifier) Classify(
	response *http.Response,
	failure error,
) CircuitOutcome {
	return defaultCircuitOutcomeClassifier{}.ClassifyContext(
		context.Background(), response, failure,
	)
}

// ClassifyContext applies the default policy with caller-context evidence.
func (defaultCircuitOutcomeClassifier) ClassifyContext(
	ctx context.Context,
	response *http.Response,
	failure error,
) CircuitOutcome {
	if failure != nil {
		if errors.Is(failure, context.Canceled) && ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
			return CircuitOutcomeIgnored
		}
		if errors.Is(failure, ErrRateLimitCapacity) ||
			errors.Is(failure, ErrRateLimitWaitExceeded) {
			return CircuitOutcomeIgnored
		}

		return CircuitOutcomeFailure
	}
	if response == nil {
		return CircuitOutcomeIgnored
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return CircuitOutcomeIgnored
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return CircuitOutcomeFailure
	}

	return CircuitOutcomeSuccess
}

type goCircuitBreakerAdapter struct{ breaker *breaker.Breaker }

// NewGoCircuitBreakerAdapter adapts the first-party circuit-breaker. The
// breaker remains caller-owned and must be shut down by its owner.
func NewGoCircuitBreakerAdapter(value *breaker.Breaker) (CircuitBreaker, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: Go breaker is nil", ErrInvalidCircuitBreaker)
	}

	return goCircuitBreakerAdapter{breaker: value}, nil
}

func (adapter goCircuitBreakerAdapter) Execute(
	ctx context.Context,
	operation func(context.Context) (*http.Response, error),
) (*http.Response, error) {
	response, err := breaker.Execute(ctx, adapter.breaker, operation)
	if err == nil {
		return response, nil
	}
	var rejection *breaker.RejectionError
	if errors.As(err, &rejection) {
		return nil, &CircuitBreakerError{Cause: err}
	}

	return response, err
}

// NewGoCircuitBreakerClassifier maps the HTTP classifier into the first-party
// breaker's outcome contract. Nil selects DefaultCircuitOutcomeClassifier.
func NewGoCircuitBreakerClassifier(
	classifier CircuitOutcomeClassifier,
) (breaker.Classifier, error) {
	if classifier == nil {
		classifier = DefaultCircuitOutcomeClassifier()
	} else if nilLike(classifier) {
		return nil, fmt.Errorf("%w: classifier is nil", ErrInvalidCircuitBreaker)
	}

	return func(completion breaker.Completion) breaker.Outcome {
		response, _ := completion.Result.(*http.Response)
		var outcome CircuitOutcome
		if contextual, ok := classifier.(ContextCircuitOutcomeClassifier); ok {
			outcome = contextual.ClassifyContext(
				completion.Context, response, completion.Err,
			)
		} else {
			outcome = classifier.Classify(response, completion.Err)
		}
		switch outcome {
		case CircuitOutcomeSuccess:
			return breaker.OutcomeSuccess
		case CircuitOutcomeFailure:
			return breaker.OutcomeFailure
		case CircuitOutcomeIgnored:
			return breaker.OutcomeIgnored
		default:
			return breaker.Outcome(^uint8(0))
		}
	}, nil
}
