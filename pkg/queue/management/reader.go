package management

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"sort"
	"strconv"
)

const MaxStatusProviders = 1_000

var (
	// ErrInvalidStatusProviders reports an empty, nil, or oversized source set.
	ErrInvalidStatusProviders = errors.New("management: invalid status providers")
	// ErrInvalidStatusCursor reports pagination state outside the source set.
	ErrInvalidStatusCursor = errors.New("management: invalid status cursor")
	// ErrInvalidStatusProviderOutput reports malformed or duplicate observations.
	ErrInvalidStatusProviderOutput = errors.New("management: invalid status provider output")
)

// WorkerStatusProvider emits one native worker observation.
type WorkerStatusProvider interface {
	ObserveWorker(context.Context) (WorkerStatus, error)
}

// QueueStatusProvider emits one logical backend queue observation.
type QueueStatusProvider interface {
	ObserveQueue(context.Context) (QueueStatus, error)
}

// StatusProvider emits both worker and queue observations.
type StatusProvider interface {
	WorkerStatusProvider
	QueueStatusProvider
}

// StatusReaderConfig separates worker cardinality from logical queue sources.
type StatusReaderConfig struct {
	Workers []WorkerStatusProvider
	Queues  []QueueStatusProvider
}

// ProviderStatusReader composes bounded native adapter observations.
type ProviderStatusReader struct {
	workers []WorkerStatusProvider
	queues  []QueueStatusProvider
}

// NewStatusReader creates a deterministic provider-backed status reader.
func NewStatusReader(config StatusReaderConfig) (*ProviderStatusReader, error) {
	if (len(config.Workers) == 0 && len(config.Queues) == 0) ||
		len(config.Workers) > MaxStatusProviders || len(config.Queues) > MaxStatusProviders {
		return nil, ErrInvalidStatusProviders
	}
	workers := append([]WorkerStatusProvider(nil), config.Workers...)
	for _, provider := range workers {
		if nilStatusProvider(provider) {
			return nil, ErrInvalidStatusProviders
		}
	}
	queues := append([]QueueStatusProvider(nil), config.Queues...)
	for _, provider := range queues {
		if nilStatusProvider(provider) {
			return nil, ErrInvalidStatusProviders
		}
	}

	return &ProviderStatusReader{workers: workers, queues: queues}, nil
}

// ListWorkers returns one worker-ID-sorted bounded provider page.
func (r *ProviderStatusReader) ListWorkers(
	ctx context.Context,
	request StatusPageRequest,
) (WorkerStatusPage, error) {
	start, err := statusPageStart(request)
	if err != nil {
		return WorkerStatusPage{}, err
	}
	items := make([]WorkerStatus, 0, len(r.workers))
	for _, provider := range r.workers {
		item, err := provider.ObserveWorker(ctx)
		if err != nil {
			return WorkerStatusPage{}, err
		}
		if item.Validate() != nil {
			return WorkerStatusPage{}, ErrInvalidStatusProviderOutput
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for index := 1; index < len(items); index++ {
		if items[index-1].ID == items[index].ID {
			return WorkerStatusPage{}, ErrInvalidStatusProviderOutput
		}
	}
	pageItems, next, err := statusPage(items, start, request.Limit)
	if err != nil {
		return WorkerStatusPage{}, err
	}

	return WorkerStatusPage{Items: pageItems, NextCursor: next}, nil
}

// ListQueues returns one queue-name-sorted bounded provider page.
func (r *ProviderStatusReader) ListQueues(
	ctx context.Context,
	request StatusPageRequest,
) (QueueStatusPage, error) {
	start, err := statusPageStart(request)
	if err != nil {
		return QueueStatusPage{}, err
	}
	items := make([]QueueStatus, 0, len(r.queues))
	for _, provider := range r.queues {
		item, err := provider.ObserveQueue(ctx)
		if err != nil {
			return QueueStatusPage{}, err
		}
		if item.Validate() != nil {
			return QueueStatusPage{}, ErrInvalidStatusProviderOutput
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Queue != items[j].Queue {
			return items[i].Queue < items[j].Queue
		}
		return items[i].Backend < items[j].Backend
	})
	for index := 1; index < len(items); index++ {
		if items[index-1].Queue == items[index].Queue &&
			items[index-1].Backend == items[index].Backend {
			return QueueStatusPage{}, ErrInvalidStatusProviderOutput
		}
	}
	pageItems, next, err := statusPage(items, start, request.Limit)
	if err != nil {
		return QueueStatusPage{}, err
	}

	return QueueStatusPage{Items: pageItems, NextCursor: next}, nil
}

func statusPageStart(request StatusPageRequest) (int, error) {
	if err := request.Validate(); err != nil {
		return 0, err
	}
	if request.Cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(request.Cursor)
	if err != nil {
		return 0, ErrInvalidStatusCursor
	}
	offset, err := strconv.ParseUint(string(decoded), 10, 16)
	if err != nil || offset > MaxStatusProviders {
		return 0, ErrInvalidStatusCursor
	}

	return int(offset), nil
}

func statusPage[T any](items []T, start int, limit uint32) ([]T, string, error) {
	if start > len(items) {
		return nil, "", ErrInvalidStatusCursor
	}
	end := min(start+int(limit), len(items))
	page := append([]T(nil), items[start:end]...)
	if end == len(items) {
		return page, "", nil
	}
	next := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))

	return page, next, nil
}

func nilStatusProvider(provider any) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
