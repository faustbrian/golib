package kafka

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ErrorCategory is a stable operational classification independent of
// franz-go and Kafka protocol error types.
type ErrorCategory uint8

const (
	// ErrorPermanent identifies a definite failure that policy must not retry
	// without changing input or configuration.
	ErrorPermanent ErrorCategory = iota + 1
	// ErrorRetryable identifies a transient broker or transport failure.
	ErrorRetryable
	// ErrorAuthorization identifies authentication or authorization denial.
	ErrorAuthorization
	// ErrorFenced identifies loss of producer, transaction, or group ownership.
	ErrorFenced
	// ErrorOversized identifies a broker-side record or batch size rejection.
	ErrorOversized
	// ErrorTimeout identifies expiry of a bounded operation.
	ErrorTimeout
	// ErrorCanceled identifies caller cancellation.
	ErrorCanceled
	// ErrorShutdown identifies an operation rejected or failed by client close.
	ErrorShutdown
	// ErrorAmbiguous identifies an operation whose durable outcome is unknown.
	ErrorAmbiguous
	// ErrorFatal identifies producer state that cannot safely continue.
	ErrorFatal
)

// String returns a stable low-cardinality category name.
func (category ErrorCategory) String() string {
	switch category {
	case ErrorPermanent:
		return "permanent"
	case ErrorRetryable:
		return "retryable"
	case ErrorAuthorization:
		return "authorization"
	case ErrorFenced:
		return "fenced"
	case ErrorOversized:
		return "oversized"
	case ErrorTimeout:
		return "timeout"
	case ErrorCanceled:
		return "canceled"
	case ErrorShutdown:
		return "shutdown"
	case ErrorAmbiguous:
		return "ambiguous"
	case ErrorFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// DeliveryError classifies one producer delivery failure. Error deliberately
// omits the underlying diagnostic so endpoints, credentials, record bytes, and
// headers cannot be rendered accidentally. Unwrap preserves programmatic error
// identity for callers that intentionally inspect the cause.
type DeliveryError struct {
	category ErrorCategory
	cause    error
}

// Error implements error with a stable redacted diagnostic.
func (err *DeliveryError) Error() string {
	if err == nil {
		return "kafka: delivery failed"
	}

	return "kafka: delivery " + err.category.String() + " failure"
}

// Unwrap returns the original failure for errors.Is and errors.As.
func (err *DeliveryError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.cause
}

// Category returns the stable operational category.
func (err *DeliveryError) Category() ErrorCategory {
	if err == nil {
		return 0
	}

	return err.category
}

// Retryable reports whether the package classified the failure as transient.
func (err *DeliveryError) Retryable() bool {
	return err != nil && err.category == ErrorRetryable
}

func newDeliveryError(cause error) *DeliveryError {
	if cause == nil {
		return nil
	}

	return &DeliveryError{category: classifyError(cause), cause: cause}
}

func classifyError(err error) ErrorCategory {
	switch {
	case errors.Is(err, ErrDeliveryResultMissing):
		return ErrorAmbiguous
	case errors.Is(err, kgo.ErrClientClosed), errors.Is(err, kgo.ErrAborting):
		return ErrorShutdown
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, kerr.RequestTimedOut),
		errors.Is(err, kgo.ErrRecordTimeout):
		return ErrorTimeout
	case errors.Is(err, context.Canceled):
		return ErrorCanceled
	case isAuthorizationError(err):
		return ErrorAuthorization
	case isFencingError(err):
		return ErrorFenced
	case isFatalProducerError(err):
		return ErrorFatal
	case isOversizedError(err):
		return ErrorOversized
	case errors.Is(err, kgo.ErrRecordRetries),
		kerr.IsRetriable(err),
		kgo.IsRetryableBrokerErr(err):
		return ErrorRetryable
	default:
		return ErrorPermanent
	}
}

func isFatalProducerError(err error) bool {
	return errors.Is(err, kerr.OutOfOrderSequenceNumber) ||
		errors.Is(err, kerr.UnknownProducerID)
}

func isAuthorizationError(err error) bool {
	return errors.Is(err, kerr.TopicAuthorizationFailed) ||
		errors.Is(err, kerr.GroupAuthorizationFailed) ||
		errors.Is(err, kerr.ClusterAuthorizationFailed) ||
		errors.Is(err, kerr.TransactionalIDAuthorizationFailed) ||
		errors.Is(err, kerr.DelegationTokenAuthorizationFailed) ||
		errors.Is(err, kerr.SaslAuthenticationFailed)
}

func isFencingError(err error) bool {
	return errors.Is(err, kerr.ProducerFenced) ||
		errors.Is(err, kerr.InvalidProducerEpoch) ||
		errors.Is(err, kerr.TransactionCoordinatorFenced) ||
		errors.Is(err, kerr.FencedInstanceID) ||
		errors.Is(err, kerr.FencedMemberEpoch)
}

func isOversizedError(err error) bool {
	return errors.Is(err, kerr.MessageTooLarge) ||
		errors.Is(err, kerr.RecordListTooLarge) ||
		errors.Is(err, kerr.TelemetryTooLarge)
}
