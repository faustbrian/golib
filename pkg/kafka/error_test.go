package kafka

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestDeliveryErrorClassifiesKafkaFailuresWithoutRenderingCause(t *testing.T) {

	secretCause := errors.New("dial user:password@broker.internal")
	tests := []struct {
		name     string
		cause    error
		category ErrorCategory
		retry    bool
	}{
		{name: "retryable", cause: kerr.NotEnoughReplicas, category: ErrorRetryable, retry: true},
		{name: "authorization", cause: kerr.TopicAuthorizationFailed, category: ErrorAuthorization},
		{name: "fenced", cause: kerr.ProducerFenced, category: ErrorFenced},
		{name: "oversized", cause: kerr.MessageTooLarge, category: ErrorOversized},
		{name: "timeout", cause: context.DeadlineExceeded, category: ErrorAmbiguous},
		{name: "record timeout", cause: kgo.ErrRecordTimeout, category: ErrorAmbiguous},
		{name: "retries exhausted", cause: kgo.ErrRecordRetries, category: ErrorAmbiguous},
		{
			name:     "transport",
			cause:    &net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
			category: ErrorRetryable,
			retry:    true,
		},
		{
			name:     "dial transport",
			cause:    &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)},
			category: ErrorRetryable,
			retry:    true,
		},
		{
			name:     "dial timeout",
			cause:    &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ETIMEDOUT)},
			category: ErrorTimeout,
		},
		{
			name:     "dial permission",
			cause:    &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.EACCES)},
			category: ErrorPermanent,
		},
		{name: "fatal sequence", cause: kerr.OutOfOrderSequenceNumber, category: ErrorFatal},
		{name: "fatal producer ID", cause: kerr.UnknownProducerID, category: ErrorFatal},
		{name: "fatal producer mapping", cause: kerr.InvalidProducerIDMapping, category: ErrorFatal},
		{name: "ambiguous result", cause: ErrDeliveryResultMissing, category: ErrorAmbiguous},
		{name: "invalid result", cause: ErrDeliveryResultInvalid, category: ErrorAmbiguous},
		{name: "cancelled", cause: context.Canceled, category: ErrorAmbiguous},
		{name: "shutdown", cause: kgo.ErrClientClosed, category: ErrorShutdown},
		{name: "aborted", cause: kgo.ErrAborting, category: ErrorShutdown},
		{name: "permanent", cause: secretCause, category: ErrorPermanent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			err := newDeliveryError(test.cause)
			if err == nil {
				t.Fatal("newDeliveryError() = nil")
			}
			if err.Category() != test.category {
				t.Fatalf("Category() = %v, want %v", err.Category(), test.category)
			}
			if err.Retryable() != test.retry {
				t.Fatalf("Retryable() = %t, want %t", err.Retryable(), test.retry)
			}
			if !errors.Is(err, test.cause) {
				t.Fatalf("errors.Is(%v) = false", test.cause)
			}
			if strings.Contains(err.Error(), test.cause.Error()) ||
				strings.Contains(err.Error(), "password") ||
				strings.Contains(err.Error(), "broker.internal") {
				t.Fatalf("Error() disclosed cause: %q", err.Error())
			}
		})
	}
}

func TestPublishRecordReturnsClassifiedDeliveryFailure(t *testing.T) {

	backend := &recordingProducerBackend{deliveryErr: kerr.TopicAuthorizationFailed}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	result := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
	})

	var deliveryErr *DeliveryError
	if !errors.As(result.Err, &deliveryErr) {
		t.Fatalf("PublishRecord() error = %T, want *DeliveryError", result.Err)
	}
	if deliveryErr.Category() != ErrorAuthorization {
		t.Fatalf("PublishRecord() category = %v", deliveryErr.Category())
	}
}

func TestDeliveryErrorSupportsAllStableCategoriesAndNilReceiver(t *testing.T) {

	if got := ErrorAmbiguous.String(); got != "ambiguous" {
		t.Fatalf("ErrorAmbiguous.String() = %q", got)
	}
	if got := ErrorFatal.String(); got != "fatal" {
		t.Fatalf("ErrorFatal.String() = %q", got)
	}
	if got := ErrorTimeout.String(); got != "timeout" {
		t.Fatalf("ErrorTimeout.String() = %q", got)
	}
	if got := ErrorCanceled.String(); got != "canceled" {
		t.Fatalf("ErrorCanceled.String() = %q", got)
	}
	if got := ErrorCategory(255).String(); got != "unknown" {
		t.Fatalf("unknown ErrorCategory.String() = %q", got)
	}
	if err := newDeliveryError(nil); err != nil {
		t.Fatalf("newDeliveryError(nil) = %v", err)
	}
	if category := classifyError(newDeliveryError(context.Canceled)); category != ErrorAmbiguous {
		t.Fatalf("classifyError(DeliveryError) = %v, want %v", category, ErrorAmbiguous)
	}

	var err *DeliveryError
	if err.Error() != "kafka: delivery failed" || err.Unwrap() != nil ||
		err.Category() != 0 || err.Retryable() {
		t.Fatalf("nil DeliveryError methods returned an unsafe result")
	}
}

func TestConsumerErrorClassifiesAndRedactsGroupFailures(t *testing.T) {

	cause := errors.Join(
		kerr.NotCoordinator,
		errors.New("group user:password@broker.internal"),
	)
	err := newConsumerError(ConsumerOperationPoll, cause)
	var consumerErr *ConsumerError
	if !errors.As(err, &consumerErr) ||
		!errors.Is(err, kerr.NotCoordinator) ||
		consumerErr.Error() != "kafka: consumer poll retryable failure" ||
		consumerErr.Operation() != ConsumerOperationPoll ||
		consumerErr.Category() != ErrorRetryable ||
		!consumerErr.Retryable() ||
		classifyError(err) != ErrorRetryable ||
		strings.Contains(consumerErr.Error(), "password") ||
		strings.Contains(consumerErr.Error(), "broker.internal") {
		t.Fatalf("consumer error = %#v / %q", consumerErr, err)
	}
	if got := newConsumerError(ConsumerOperationPoll, err); got != err {
		t.Fatalf("same-operation newConsumerError() = %#v, want original", got)
	}
	joined := errors.Join(err, errors.New("joined password secret"))
	rewrapped := newConsumerError(ConsumerOperationPoll, joined)
	if rewrapped == joined ||
		!errors.Is(rewrapped, joined) ||
		strings.Contains(rewrapped.Error(), "password") {
		t.Fatalf("joined same-operation consumer error = %v", rewrapped)
	}

	wrapped := newConsumerError(ConsumerOperationCommit, err)
	if !errors.As(wrapped, &consumerErr) ||
		consumerErr.Operation() != ConsumerOperationCommit ||
		consumerErr.Category() != ErrorRetryable ||
		!errors.Is(wrapped, cause) {
		t.Fatalf("cross-operation consumer error = %#v / %v", consumerErr, wrapped)
	}

	permanent := newConsumerError(
		ConsumerOperationLeave,
		errors.New("leave failed"),
	)
	if !errors.As(permanent, &consumerErr) ||
		consumerErr.Category() != ErrorPermanent ||
		consumerErr.Retryable() {
		t.Fatalf("permanent consumer error = %#v / %v", consumerErr, permanent)
	}
}

func TestConsumerErrorSupportsAllOperationsAndNilReceiver(t *testing.T) {

	if ConsumerOperationPoll.String() != "poll" ||
		ConsumerOperationCommit.String() != "commit" ||
		ConsumerOperationLeave.String() != "leave" ||
		ConsumerOperation(255).String() != "unknown" {
		t.Fatal("consumer operation strings are unstable")
	}
	if err := newConsumerError(ConsumerOperationPoll, nil); err != nil {
		t.Fatalf("newConsumerError(nil) = %v", err)
	}

	var err *ConsumerError
	if err.Error() != "kafka: consumer failed" ||
		err.Unwrap() != nil ||
		err.Operation() != 0 ||
		err.Category() != 0 ||
		err.Retryable() {
		t.Fatal("nil ConsumerError methods returned an unsafe result")
	}
}
