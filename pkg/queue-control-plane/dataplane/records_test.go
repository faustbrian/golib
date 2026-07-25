package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestRecordSourceListsTenantScopedFailuresAndDeadLetters(t *testing.T) {
	t.Parallel()

	request := queue.PageRequest{
		Limit: 20, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	for name, kind := range map[string]queue.RecordKind{
		"failures":     queue.RecordFailure,
		"dead letters": queue.RecordDeadLetter,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reader := &recordReaderStub{page: queue.RecordPage{
				Items:      []queue.JobRecord{validJobRecord(kind)},
				NextCursor: "next",
			}}
			resolver := &recordResolverStub{reader: reader}
			source, err := NewRecordSource(resolver)
			if err != nil {
				t.Fatalf("NewRecordSource() error = %v", err)
			}
			var page queue.RecordPage
			if kind == queue.RecordFailure {
				page, err = source.ListFailures(context.Background(), "tenant-1", request)
			} else {
				page, err = source.ListDeadLetters(context.Background(), "tenant-1", request)
			}
			if err != nil || len(page.Items) != 1 || page.Items[0].Kind != kind ||
				page.NextCursor != "next" {
				t.Fatalf("list = (%+v, %v)", page, err)
			}
			if resolver.tenant != "tenant-1" || reader.request != request {
				t.Fatalf("resolution = tenant %q request %+v", resolver.tenant, reader.request)
			}
		})
	}
}

func TestRecordSourceInspectsOnlyAuthorizedVisibility(t *testing.T) {
	t.Parallel()

	record := validJobRecord(queue.RecordFailure)
	record.Payload = queue.Payload{
		Visibility:  queue.PayloadRevealed,
		ContentType: "application/json",
		Size:        2,
		Data:        []byte("{}"),
	}
	reader := &recordReaderStub{record: record}
	source, err := NewRecordSource(&recordResolverStub{reader: reader})
	if err != nil {
		t.Fatalf("NewRecordSource() error = %v", err)
	}
	request := queue.InspectRequest{
		Kind: queue.RecordFailure, ID: record.ID, Visibility: queue.PayloadRevealed,
	}
	got, err := source.Inspect(context.Background(), "tenant-1", request)
	if err != nil || got.ID != record.ID || string(got.Payload.Data) != "{}" || reader.inspect != request {
		t.Fatalf("Inspect() = (%+v, %v)", got, err)
	}

	reader.record.Payload = queue.Payload{Visibility: queue.PayloadRevealed, Size: 2, Data: []byte("{}")}
	request.Visibility = queue.PayloadHidden
	if _, err := source.Inspect(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidRecordOutput) {
		t.Fatalf("hidden Inspect() error = %v", err)
	}
	reader.record.Payload = queue.Payload{Visibility: queue.PayloadHidden, Size: 2}
	request.Visibility = queue.PayloadRedacted
	if _, err := source.Inspect(context.Background(), "tenant-1", request); err != nil {
		t.Fatalf("degraded Inspect() error = %v", err)
	}
}

func TestRecordSourceFailsClosedAtEveryBoundary(t *testing.T) {
	t.Parallel()

	var typedNilResolver *recordResolverStub
	for _, resolver := range []RecordReaderResolver{nil, typedNilResolver} {
		source, err := NewRecordSource(resolver)
		if source != nil || !errors.Is(err, ErrInvalidRecordConfiguration) {
			t.Fatalf("NewRecordSource() = (%v, %v)", source, err)
		}
	}

	request := queue.PageRequest{Limit: 1, Sort: queue.SortOccurredAt, Direction: queue.SortDescending}
	resolverErr := errors.New("resolver unavailable")
	source := mustRecordSource(t, &recordResolverStub{err: resolverErr})
	if _, err := source.ListFailures(context.Background(), "tenant-1", request); !errors.Is(err, resolverErr) {
		t.Fatalf("resolver ListFailures() error = %v", err)
	}
	var typedNilReader *recordReaderStub
	source = mustRecordSource(t, &recordResolverStub{reader: typedNilReader})
	if _, err := source.ListFailures(context.Background(), "tenant-1", request); !errors.Is(err, ErrRecordReaderUnavailable) {
		t.Fatalf("nil reader ListFailures() error = %v", err)
	}

	readerErr := errors.New("reader unavailable")
	reader := &recordReaderStub{err: readerErr}
	source = mustRecordSource(t, &recordResolverStub{reader: reader})
	if _, err := source.ListFailures(context.Background(), "tenant-1", request); !errors.Is(err, readerErr) {
		t.Fatalf("reader ListFailures() error = %v", err)
	}
	if _, err := source.ListDeadLetters(context.Background(), "tenant-1", request); !errors.Is(err, readerErr) {
		t.Fatalf("reader ListDeadLetters() error = %v", err)
	}
	if _, err := source.Inspect(context.Background(), "tenant-1", queue.InspectRequest{
		Kind: queue.RecordFailure, ID: "failure-1",
	}); !errors.Is(err, readerErr) {
		t.Fatalf("reader Inspect() error = %v", err)
	}

	reader.err = nil
	reader.page = queue.RecordPage{Items: []queue.JobRecord{validJobRecord(queue.RecordDeadLetter)}}
	if _, err := source.ListFailures(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidRecordOutput) {
		t.Fatalf("wrong-kind ListFailures() error = %v", err)
	}
	reader.page = queue.RecordPage{Items: []queue.JobRecord{validJobRecord(queue.RecordFailure)}}
	if _, err := source.ListDeadLetters(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidRecordOutput) {
		t.Fatalf("wrong-kind ListDeadLetters() error = %v", err)
	}
	reader.page = queue.RecordPage{Items: []queue.JobRecord{{Kind: queue.RecordFailure}}}
	if _, err := source.ListFailures(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidRecordOutput) {
		t.Fatalf("invalid-page ListFailures() error = %v", err)
	}
	reader.record = validJobRecord(queue.RecordDeadLetter)
	if _, err := source.Inspect(context.Background(), "tenant-1", queue.InspectRequest{
		Kind: queue.RecordFailure, ID: "failure-1",
	}); !errors.Is(err, ErrInvalidRecordOutput) {
		t.Fatalf("wrong-kind Inspect() error = %v", err)
	}

	invalidRequest := request
	invalidRequest.Limit = 0
	resolver := &recordResolverStub{reader: reader}
	source = mustRecordSource(t, resolver)
	if _, err := source.ListFailures(context.Background(), "tenant-1", invalidRequest); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid request = error %v resolver calls %d", err, resolver.calls)
	}
	if _, err := source.ListDeadLetters(context.Background(), "tenant-1", invalidRequest); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid dead-letter request = error %v resolver calls %d", err, resolver.calls)
	}
	if _, err := source.ListFailures(context.Background(), "", request); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid tenant = error %v resolver calls %d", err, resolver.calls)
	}
	if _, err := source.Inspect(context.Background(), "tenant-1", queue.InspectRequest{}); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid inspect = error %v resolver calls %d", err, resolver.calls)
	}
	if visibilityPermitted(queue.PayloadVisibility("invalid"), queue.PayloadHidden) {
		t.Fatal("invalid requested visibility was permitted")
	}
}

func mustRecordSource(t *testing.T, resolver RecordReaderResolver) *RecordSource {
	t.Helper()

	source, err := NewRecordSource(resolver)
	if err != nil {
		t.Fatalf("NewRecordSource() error = %v", err)
	}

	return source
}

func validJobRecord(kind queue.RecordKind) queue.JobRecord {
	return queue.JobRecord{
		Kind: kind, ID: "failure-1", Backend: "valkey-streams", Queue: "critical",
		OccurredAt: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Attempts:   3, FailureCode: "handler_failed",
		Payload: queue.Payload{Visibility: queue.PayloadHidden, Size: 128},
	}
}

type recordResolverStub struct {
	reader queue.RecordReader
	err    error
	tenant string
	calls  int
}

func (s *recordResolverStub) ResolveRecordReader(
	_ context.Context,
	tenant string,
) (queue.RecordReader, error) {
	s.calls++
	s.tenant = tenant

	return s.reader, s.err
}

type recordReaderStub struct {
	page    queue.RecordPage
	record  queue.JobRecord
	err     error
	request queue.PageRequest
	inspect queue.InspectRequest
}

func (s *recordReaderStub) ListFailures(
	_ context.Context,
	request queue.PageRequest,
) (queue.RecordPage, error) {
	s.request = request

	return s.page, s.err
}

func (s *recordReaderStub) ListDeadLetters(
	_ context.Context,
	request queue.PageRequest,
) (queue.RecordPage, error) {
	s.request = request

	return s.page, s.err
}

func (s *recordReaderStub) Inspect(
	_ context.Context,
	request queue.InspectRequest,
) (queue.JobRecord, error) {
	s.inspect = request

	return s.record, s.err
}
