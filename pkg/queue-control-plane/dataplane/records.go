package dataplane

import (
	"context"
	"errors"
	"strings"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrInvalidRecordConfiguration reports a missing reader resolver.
	ErrInvalidRecordConfiguration = errors.New("dataplane: invalid record configuration")
	// ErrInvalidRecordRequest reports an invalid tenant scope.
	ErrInvalidRecordRequest = errors.New("dataplane: invalid record request")
	// ErrRecordReaderUnavailable reports a resolver without a tenant reader.
	ErrRecordReaderUnavailable = errors.New("dataplane: record reader unavailable")
	// ErrInvalidRecordOutput reports malformed or over-disclosed adapter output.
	ErrInvalidRecordOutput = errors.New("dataplane: invalid record output")
)

// RecordReaderResolver selects a tenant-scoped queue record reader.
type RecordReaderResolver interface {
	ResolveRecordReader(context.Context, string) (queue.RecordReader, error)
}

// RecordSource applies tenant routing and validates all queue record output.
type RecordSource struct {
	resolver RecordReaderResolver
}

// NewRecordSource creates a tenant-scoped failure and dead-letter source.
func NewRecordSource(resolver RecordReaderResolver) (*RecordSource, error) {
	if nilInterface(resolver) {
		return nil, ErrInvalidRecordConfiguration
	}

	return &RecordSource{resolver: resolver}, nil
}

// ListFailures returns one validated page of failure metadata.
func (s *RecordSource) ListFailures(
	ctx context.Context,
	tenant string,
	request queue.PageRequest,
) (queue.RecordPage, error) {
	reader, err := s.reader(ctx, tenant, request.Validate())
	if err != nil {
		return queue.RecordPage{}, err
	}
	page, err := reader.ListFailures(ctx, request)
	if err != nil {
		return queue.RecordPage{}, err
	}
	if !validRecordPage(page, queue.RecordFailure) {
		return queue.RecordPage{}, ErrInvalidRecordOutput
	}

	return page, nil
}

// ListDeadLetters returns one validated page of dead-letter metadata.
func (s *RecordSource) ListDeadLetters(
	ctx context.Context,
	tenant string,
	request queue.PageRequest,
) (queue.RecordPage, error) {
	reader, err := s.reader(ctx, tenant, request.Validate())
	if err != nil {
		return queue.RecordPage{}, err
	}
	page, err := reader.ListDeadLetters(ctx, request)
	if err != nil {
		return queue.RecordPage{}, err
	}
	if !validRecordPage(page, queue.RecordDeadLetter) {
		return queue.RecordPage{}, ErrInvalidRecordOutput
	}

	return page, nil
}

// Inspect returns one validated record without exceeding requested payload
// visibility. Adapters may safely return a more-redacted representation.
func (s *RecordSource) Inspect(
	ctx context.Context,
	tenant string,
	request queue.InspectRequest,
) (queue.JobRecord, error) {
	reader, err := s.reader(ctx, tenant, request.Validate())
	if err != nil {
		return queue.JobRecord{}, err
	}
	record, err := reader.Inspect(ctx, request)
	if err != nil {
		return queue.JobRecord{}, err
	}
	if record.Validate() != nil || record.Kind != request.Kind ||
		record.ID != request.ID || !visibilityPermitted(request.Visibility, record.Payload.Visibility) {
		return queue.JobRecord{}, ErrInvalidRecordOutput
	}

	return record, nil
}

func (s *RecordSource) reader(
	ctx context.Context,
	tenant string,
	requestErr error,
) (queue.RecordReader, error) {
	if requestErr != nil {
		return nil, requestErr
	}
	if strings.TrimSpace(tenant) == "" || len(tenant) > controlplane.MaxIdentityBytes {
		return nil, ErrInvalidRecordRequest
	}
	reader, err := s.resolver.ResolveRecordReader(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if nilInterface(reader) {
		return nil, ErrRecordReaderUnavailable
	}

	return reader, nil
}

func validRecordPage(page queue.RecordPage, kind queue.RecordKind) bool {
	if page.Validate() != nil {
		return false
	}
	for _, record := range page.Items {
		if record.Kind != kind {
			return false
		}
	}

	return true
}

func visibilityPermitted(requested, actual queue.PayloadVisibility) bool {
	switch requested {
	case queue.PayloadHidden:
		return actual == queue.PayloadHidden
	case queue.PayloadRedacted:
		return actual == queue.PayloadHidden || actual == queue.PayloadRedacted
	case queue.PayloadRevealed:
		return actual == queue.PayloadHidden || actual == queue.PayloadRedacted ||
			actual == queue.PayloadRevealed
	default:
		return false
	}
}
