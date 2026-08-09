package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

type sinkFunc struct {
	append      func(context.Context, audit.Record) (audit.AppendResult, error)
	appendBatch func(context.Context, []audit.Record) (audit.BatchResult, error)
}

func (sink sinkFunc) Append(ctx context.Context, record audit.Record) (audit.AppendResult, error) {
	return sink.append(ctx, record)
}
func (sink sinkFunc) AppendBatch(ctx context.Context, records []audit.Record) (audit.BatchResult, error) {
	return sink.appendBatch(ctx, records)
}

type boundedBuffer struct {
	sinkFunc
	limits audit.BufferLimits
}

func (buffer boundedBuffer) BufferLimits() audit.BufferLimits { return buffer.limits }

func testBuffer(sink sinkFunc) boundedBuffer {
	return boundedBuffer{sinkFunc: sink, limits: audit.BufferLimits{
		MaxRecords: 100, MaxBytes: 1 << 20, MaxBatchRecords: 100,
	}}
}

func failingSink(cause error) sinkFunc {
	return sinkFunc{
		append: func(context.Context, audit.Record) (audit.AppendResult, error) {
			return audit.AppendResult{}, cause
		},
		appendBatch: func(context.Context, []audit.Record) (audit.BatchResult, error) {
			return audit.BatchResult{}, cause
		},
	}
}

func acceptingSink(status audit.AppendStatus) sinkFunc {
	return sinkFunc{
		append: func(_ context.Context, record audit.Record) (audit.AppendResult, error) {
			return audit.AppendResult{RecordID: record.ID(), Status: status}, nil
		},
		appendBatch: func(_ context.Context, records []audit.Record) (audit.BatchResult, error) {
			results := make([]audit.AppendResult, len(records))
			for index, record := range records {
				results[index] = audit.AppendResult{RecordID: record.ID(), Status: status}
			}
			return audit.BatchResult{Results: results}, nil
		},
	}
}

func passthroughRedactor() audit.Redactor {
	return audit.RedactorFunc(func(_ context.Context, record audit.Record) (audit.Record, error) { return record, nil })
}

func TestRecorderRequiresExplicitFailurePolicy(t *testing.T) {
	t.Parallel()

	sink := acceptingSink(audit.AppendAccepted)
	for _, config := range []audit.RecorderConfig{
		{},
		{Sink: sink, Redactor: passthroughRedactor()},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed, DelayThreshold: -time.Second},
	} {
		if _, err := audit.NewRecorder(config); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewRecorder(%#v) error = %v", config, err)
		}
	}
	for _, config := range []audit.RecorderConfig{
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert, Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { return nil })},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: boundedBuffer{sinkFunc: sink, limits: audit.BufferLimits{MaxRecords: 1, MaxBytes: 1, MaxBatchRecords: 1}}},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: testBuffer(sink)},
		{Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: boundedBuffer{sinkFunc: sink, limits: audit.BufferLimits{MaxRecords: audit.MaxAppendBatchRecords, MaxBytes: 1, MaxBatchRecords: audit.MaxAppendBatchRecords}}},
	} {
		if _, err := audit.NewRecorder(config); err != nil {
			t.Fatalf("NewRecorder(valid) error = %v", err)
		}
	}
}

func TestRecorderRejectsUnboundedDurableBuffer(t *testing.T) {
	t.Parallel()

	sink := acceptingSink(audit.AppendAccepted)
	for _, limits := range []audit.BufferLimits{
		{},
		{MaxBytes: 1, MaxBatchRecords: 1},
		{MaxRecords: 1, MaxBatchRecords: 1},
		{MaxRecords: 1, MaxBytes: 1},
		{MaxRecords: 1, MaxBytes: 1, MaxBatchRecords: 2},
		{MaxRecords: audit.MaxAppendBatchRecords + 1, MaxBytes: 1, MaxBatchRecords: audit.MaxAppendBatchRecords + 1},
	} {
		_, err := audit.NewRecorder(audit.RecorderConfig{
			Sink: sink, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer,
			Buffer: boundedBuffer{sinkFunc: sink, limits: limits},
		})
		if !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewRecorder(buffer limits %#v) error = %v", limits, err)
		}
	}
}

func TestRecorderExecutesEachFailureModeExplicitly(t *testing.T) {
	t.Parallel()

	record := mustSecurityRecord(t)
	primaryFailure := audit.NewAppendError(audit.AppendUnknown, errors.New("connection lost"))
	primary := failingSink(primaryFailure)

	failClosed, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed})
	if _, err := failClosed.Submit(context.Background(), record); !errors.Is(err, primaryFailure) {
		t.Fatalf("fail-closed Submit() error = %v", err)
	}

	alerted := 0
	failOpen, _ := audit.NewRecorder(audit.RecorderConfig{
		Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
		Alerter: audit.AlertFunc(func(_ context.Context, alert audit.DeliveryAlert) error {
			alerted++
			if alert.Outcome != audit.AppendUnknown {
				t.Fatalf("alert outcome = %v", alert.Outcome)
			}
			return nil
		}),
	})
	result, err := failOpen.Submit(context.Background(), record)
	if err != nil || result.Disposition != audit.DeliveryProceededAfterAlert || alerted != 1 {
		t.Fatalf("fail-open Submit() = %#v, %v, alerts=%d", result, err, alerted)
	}

	alertFailure := errors.New("pager unavailable")
	brokenAlert, _ := audit.NewRecorder(audit.RecorderConfig{
		Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
		Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { return alertFailure }),
	})
	if _, err := brokenAlert.Submit(context.Background(), record); !errors.Is(err, primaryFailure) || !errors.Is(err, alertFailure) {
		t.Fatalf("failed alert Submit() error = %v", err)
	}

	buffer := acceptingSink(audit.AppendAccepted)
	buffered, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: testBuffer(buffer)})
	result, err = buffered.Submit(context.Background(), record)
	if err != nil || result.Disposition != audit.DeliveryBuffered || result.Append.Status != audit.AppendAccepted {
		t.Fatalf("buffered Submit() = %#v, %v", result, err)
	}

	bufferFailure := errors.New("buffer full")
	brokenBuffer, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: testBuffer(failingSink(bufferFailure))})
	if _, err := brokenBuffer.Submit(context.Background(), record); !errors.Is(err, primaryFailure) || !errors.Is(err, bufferFailure) {
		t.Fatalf("failed buffer Submit() error = %v", err)
	}
}

func TestRecorderBatchFailureModesRemainAtomic(t *testing.T) {
	t.Parallel()

	records := []audit.Record{mustSecurityRecord(t), deliveryRecord(t, "delivery-second")}
	primaryFailure := audit.NewAppendError(audit.AppendRejected, audit.ErrBackpressure)
	primary := failingSink(primaryFailure)

	failOpen, _ := audit.NewRecorder(audit.RecorderConfig{
		Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
		Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { return nil }),
	})
	result, err := failOpen.SubmitBatch(context.Background(), records)
	if err != nil || result.Disposition != audit.DeliveryProceededAfterAlert {
		t.Fatalf("fail-open SubmitBatch() = %#v, %v", result, err)
	}

	buffer := acceptingSink(audit.AppendDuplicate)
	buffered, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: testBuffer(buffer)})
	result, err = buffered.SubmitBatch(context.Background(), records)
	if err != nil || result.Disposition != audit.DeliveryBuffered || len(result.Append.Results) != 2 {
		t.Fatalf("buffered SubmitBatch() = %#v, %v", result, err)
	}

	alertFailure := errors.New("alert failed")
	brokenAlert, _ := audit.NewRecorder(audit.RecorderConfig{
		Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
		Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { return alertFailure }),
	})
	if _, err := brokenAlert.SubmitBatch(context.Background(), records); !errors.Is(err, alertFailure) || !errors.Is(err, primaryFailure) {
		t.Fatalf("failed alert SubmitBatch() error = %v", err)
	}

	bufferFailure := errors.New("buffer failed")
	brokenBuffer, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer, Buffer: testBuffer(failingSink(bufferFailure))})
	if _, err := brokenBuffer.SubmitBatch(context.Background(), records); !errors.Is(err, bufferFailure) || !errors.Is(err, primaryFailure) {
		t.Fatalf("failed buffer SubmitBatch() error = %v", err)
	}

	failClosed, _ := audit.NewRecorder(audit.RecorderConfig{Sink: primary, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed})
	if _, err := failClosed.SubmitBatch(context.Background(), records); !errors.Is(err, primaryFailure) {
		t.Fatalf("fail-closed SubmitBatch() error = %v", err)
	}
}

func TestRecorderPersistsSuccessfulBatchAndObservesDuplicates(t *testing.T) {
	t.Parallel()

	observed := make([]audit.ObservationKind, 0, 2)
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink:     acceptingSink(audit.AppendDuplicate),
		Redactor: passthroughRedactor(),
		Mode:     audit.DeliveryFailClosed,
		Observer: audit.ObserverFunc(func(_ context.Context, value audit.Observation) {
			observed = append(observed, value.Kind)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := recorder.SubmitBatch(context.Background(), []audit.Record{
		mustSecurityRecord(t), deliveryRecord(t, "batch-second"),
	})
	if err != nil || result.Disposition != audit.DeliveryPersisted || len(result.Append.Results) != 2 {
		t.Fatalf("SubmitBatch() = %#v, %v", result, err)
	}
	if len(observed) != 2 || observed[0] != audit.ObservationDuplicated || observed[1] != audit.ObservationDuplicated {
		t.Fatalf("duplicate observations = %#v", observed)
	}
}

func TestRecorderRejectsRedactionIdentityChangesAndInvalidCalls(t *testing.T) {
	t.Parallel()

	record := mustSecurityRecord(t)
	identityRedactor := audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) {
		return deliveryRecord(t, "changed-id"), nil
	})
	recorder, _ := audit.NewRecorder(audit.RecorderConfig{Sink: acceptingSink(audit.AppendAccepted), Redactor: identityRedactor, Mode: audit.DeliveryFailClosed})
	if _, err := recorder.Submit(context.Background(), record); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("identity-changing Submit() error = %v", err)
	}
	if _, err := recorder.SubmitBatch(context.Background(), []audit.Record{record}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("identity-changing SubmitBatch() error = %v", err)
	}

	redactionFailure := errors.New("redaction rejected")
	recorder, _ = audit.NewRecorder(audit.RecorderConfig{
		Sink: acceptingSink(audit.AppendAccepted), Mode: audit.DeliveryFailClosed,
		Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) { return audit.Record{}, redactionFailure }),
	})
	if _, err := recorder.SubmitBatch(context.Background(), []audit.Record{record}); err == nil || errors.Is(err, redactionFailure) {
		t.Fatalf("redaction-failed SubmitBatch() error = %v", err)
	}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		recorder, err := audit.NewRecorder(audit.RecorderConfig{
			Sink: acceptingSink(audit.AppendAccepted), Mode: audit.DeliveryFailClosed,
			Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) {
				return audit.Record{}, cancellation
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recorder.Submit(context.Background(), record); !errors.Is(err, cancellation) {
			t.Fatalf("redaction cancellation %v returned %v", cancellation, err)
		}
	}

	var nilRecorder *audit.Recorder
	if _, err := nilRecorder.Submit(context.Background(), record); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil Submit() error = %v", err)
	}
	if _, err := nilRecorder.SubmitBatch(context.Background(), []audit.Record{record}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil SubmitBatch() error = %v", err)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := recorder.Submit(nil, record); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Submit() error = %v", err)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := recorder.SubmitBatch(nil, []audit.Record{record}); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context SubmitBatch() error = %v", err)
	}
	if _, err := recorder.SubmitBatch(context.Background(), nil); audit.AppendOutcomeOf(err) != audit.AppendRejected || !errors.Is(err, audit.ErrBatchTooLarge) {
		t.Fatalf("empty SubmitBatch() error = %v", err)
	}
	if _, err := recorder.SubmitBatch(context.Background(), make([]audit.Record, audit.MaxAppendBatchRecords+1)); audit.AppendOutcomeOf(err) != audit.AppendRejected || !errors.Is(err, audit.ErrBatchTooLarge) {
		t.Fatalf("oversized SubmitBatch() error = %v", err)
	}
}
