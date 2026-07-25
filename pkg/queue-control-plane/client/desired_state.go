package client

import (
	"context"
	"errors"
	"net/http"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

// ErrInvalidResponse reports malformed or mismatched successful API output.
var ErrInvalidResponse = errors.New("control-plane client: invalid response")

// GetDesiredState returns one validated tenant-scoped convergence record.
func (c *Client) GetDesiredState(
	ctx context.Context,
	tenant string,
	target queue.Target,
) (queue.DesiredRecord, error) {
	if invalidIdentity(tenant) || !validDesiredTarget(target) {
		return queue.DesiredRecord{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath(
		"v1", "tenants", tenant, "desired-state", string(target.Kind), target.Name,
	)
	var record queue.DesiredRecord
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &record); err != nil {
		return queue.DesiredRecord{}, err
	}
	if record.Validate() != nil || record.Target != target {
		return queue.DesiredRecord{}, ErrInvalidResponse
	}

	return record, nil
}

// DesiredStateReader binds one tenant for use by a queue reconciler.
func (c *Client) DesiredStateReader(tenant string) (queue.DesiredStateReader, error) {
	if invalidIdentity(tenant) {
		return nil, ErrInvalidRequest
	}

	return &tenantDesiredStateReader{client: c, tenant: tenant}, nil
}

type tenantDesiredStateReader struct {
	client *Client
	tenant string
}

func (r *tenantDesiredStateReader) GetDesiredState(
	ctx context.Context,
	target queue.Target,
) (queue.DesiredRecord, error) {
	return r.client.GetDesiredState(ctx, r.tenant, target)
}

func validDesiredTarget(target queue.Target) bool {
	if invalidIdentity(target.Name) {
		return false
	}
	switch target.Kind {
	case queue.TargetQueue, queue.TargetWorker, queue.TargetWorkerGroup:
		return true
	default:
		return false
	}
}

var _ queue.DesiredStateReader = (*tenantDesiredStateReader)(nil)
