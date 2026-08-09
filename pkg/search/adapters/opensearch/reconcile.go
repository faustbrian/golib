package opensearch

import (
	"context"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

const ReconciliationPITKeepAlive = 2 * time.Minute

var ErrPartialResults = errors.New("search/opensearch: partial results cannot be reconciled")

// Read implements search.ReconciliationReader with a stable ID-ordered PIT
// projection. Sources are hashed locally and are not retained in diagnostics.
func (c *Client) Read(ctx context.Context, tenant, index, cursor string, pageSize int) (search.ReconciliationPage, error) {
	result, err := c.Search(ctx, search.Request{
		Tenant: tenant, Index: index, Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
		Page: search.CursorPage{Size: pageSize, Cursor: cursor, KeepAlive: ReconciliationPITKeepAlive},
	})
	if err != nil {
		return search.ReconciliationPage{}, err
	}
	if result.Diagnostics().Partial {
		return search.ReconciliationPage{}, ErrPartialResults
	}
	hits := result.Hits()
	records := make([]search.ReconciliationRecord, len(hits))
	for position, hit := range hits {
		if hit.Version == 0 || len(hit.Source) == 0 {
			return search.ReconciliationPage{}, malformedFailure(OperationSearch, ErrMalformedResponse)
		}
		records[position] = search.IndexRecord(hit.ID, hit.Version, search.SourceDigest(hit.Source))
	}
	return search.ReconciliationPage{Records: records, Cursor: result.NextCursor(), Done: result.NextCursor() == ""}, nil
}

var _ search.ReconciliationReader = (*Client)(nil)
