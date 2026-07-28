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
	t.Parallel()

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
		{name: "cancelled", cause: context.Canceled, category: ErrorAmbiguous},
		{name: "shutdown", cause: kgo.ErrClientClosed, category: ErrorShutdown},
		{name: "aborted", cause: kgo.ErrAborting, category: ErrorShutdown},
		{name: "permanent", cause: secretCause, category: ErrorPermanent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
