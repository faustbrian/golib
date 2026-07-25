package dataplane

import (
	"context"
	"errors"
	"strings"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrInvalidStatusConfiguration reports a missing status resolver.
	ErrInvalidStatusConfiguration = errors.New("dataplane: invalid status configuration")
	// ErrInvalidStatusRequest reports an invalid tenant scope.
	ErrInvalidStatusRequest = errors.New("dataplane: invalid status request")
	// ErrStatusReaderUnavailable reports a resolver without a tenant reader.
	ErrStatusReaderUnavailable = errors.New("dataplane: status reader unavailable")
	// ErrInvalidStatusOutput reports malformed adapter output.
	ErrInvalidStatusOutput = errors.New("dataplane: invalid status output")
)

// StatusReaderResolver selects a tenant-scoped queue status reader.
type StatusReaderResolver interface {
	ResolveStatusReader(context.Context, string) (queue.StatusReader, error)
}

// StatusSource validates tenant routing, requests, and all adapter output.
type StatusSource struct {
	resolver StatusReaderResolver
}

// NewStatusSource creates a tenant-scoped worker and queue status source.
func NewStatusSource(resolver StatusReaderResolver) (*StatusSource, error) {
	if nilInterface(resolver) {
		return nil, ErrInvalidStatusConfiguration
	}

	return &StatusSource{resolver: resolver}, nil
}

// ListWorkers returns one validated worker-status page.
func (s *StatusSource) ListWorkers(
	ctx context.Context,
	tenant string,
	request queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	reader, err := s.reader(ctx, tenant, request.Validate())
	if err != nil {
		return queue.WorkerStatusPage{}, err
	}
	page, err := reader.ListWorkers(ctx, request)
	if err != nil {
		return queue.WorkerStatusPage{}, err
	}
	if page.Validate() != nil {
		return queue.WorkerStatusPage{}, ErrInvalidStatusOutput
	}

	return page, nil
}

// ListQueues returns one validated queue-status page.
func (s *StatusSource) ListQueues(
	ctx context.Context,
	tenant string,
	request queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	reader, err := s.reader(ctx, tenant, request.Validate())
	if err != nil {
		return queue.QueueStatusPage{}, err
	}
	page, err := reader.ListQueues(ctx, request)
	if err != nil {
		return queue.QueueStatusPage{}, err
	}
	if page.Validate() != nil {
		return queue.QueueStatusPage{}, ErrInvalidStatusOutput
	}

	return page, nil
}

func (s *StatusSource) reader(
	ctx context.Context,
	tenant string,
	requestErr error,
) (queue.StatusReader, error) {
	if requestErr != nil {
		return nil, requestErr
	}
	if strings.TrimSpace(tenant) == "" || len(tenant) > controlplane.MaxIdentityBytes {
		return nil, ErrInvalidStatusRequest
	}
	reader, err := s.resolver.ResolveStatusReader(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if nilInterface(reader) {
		return nil, ErrStatusReaderUnavailable
	}

	return reader, nil
}
