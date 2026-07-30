package managementhttp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/queue/management"
)

// MaxFleetEndpoints bounds one resolved management fleet and all operation fan-out.
const MaxFleetEndpoints = 100

var (
	// ErrInvalidFleetConfiguration reports an unusable dynamic fleet client.
	ErrInvalidFleetConfiguration = errors.New("managementhttp: invalid fleet configuration")
	// ErrInvalidFleetEndpoints reports malformed, duplicate, or unbounded resolved endpoints.
	ErrInvalidFleetEndpoints = errors.New("managementhttp: invalid fleet endpoints")
	// ErrFleetTargetUnavailable reports a worker target absent from the current fleet snapshot.
	ErrFleetTargetUnavailable = errors.New("managementhttp: fleet target unavailable")
	// ErrFleetUnavailable reports a fleet operation with no trustworthy remote result.
	ErrFleetUnavailable = errors.New("managementhttp: fleet unavailable")
)

// Endpoint is one stable worker-management transport target.
type Endpoint struct {
	ID      string
	BaseURL string
}

// EndpointResolver returns the current bounded worker-management fleet.
type EndpointResolver interface {
	ResolveEndpoints(context.Context) ([]Endpoint, error)
}

// EndpointResolverFunc adapts a function into an EndpointResolver.
type EndpointResolverFunc func(context.Context) ([]Endpoint, error)

// ResolveEndpoints implements EndpointResolver.
func (resolve EndpointResolverFunc) ResolveEndpoints(ctx context.Context) ([]Endpoint, error) {
	return resolve(ctx)
}

// FleetClientConfig configures dynamic authenticated worker-management access.
type FleetClientConfig struct {
	Resolver         EndpointResolver
	Token            string
	HTTPClient       *http.Client
	MaxResponseBytes int64
}

// FleetClient aggregates bounded worker status and routes control to current endpoints.
type FleetClient struct {
	resolver         EndpointResolver
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
}

// NewFleetClient creates a dynamic multi-endpoint management client.
func NewFleetClient(config FleetClientConfig) (*FleetClient, error) {
	if nilInterface(config.Resolver) || invalidToken(config.Token) ||
		config.MaxResponseBytes < 0 {
		return nil, ErrInvalidFleetConfiguration
	}

	return &FleetClient{
		resolver: config.Resolver, token: config.Token,
		httpClient: config.HTTPClient, maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

func (client *FleetClient) ListWorkers(
	ctx context.Context,
	request management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	if request.Validate() != nil {
		return management.WorkerStatusPage{}, ErrInvalidRequest
	}
	remotes, err := client.resolve(ctx)
	if err != nil {
		return management.WorkerStatusPage{}, err
	}
	workers, err := client.workerStatuses(ctx, remotes)
	if err != nil {
		return management.WorkerStatusPage{}, err
	}
	providers := make([]management.WorkerStatusProvider, len(workers))
	for index := range workers {
		providers[index] = fleetWorkerStatus{value: workers[index]}
	}
	reader, err := management.NewStatusReader(management.StatusReaderConfig{
		Workers: providers,
	})
	if err != nil {
		return management.WorkerStatusPage{}, ErrFleetUnavailable
	}

	return reader.ListWorkers(ctx, request)
}

func (client *FleetClient) ListQueues(
	ctx context.Context,
	request management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	if request.Validate() != nil {
		return management.QueueStatusPage{}, ErrInvalidRequest
	}
	remotes, err := client.resolve(ctx)
	if err != nil {
		return management.QueueStatusPage{}, err
	}
	queues, err := client.queueStatuses(ctx, remotes)
	if err != nil {
		return management.QueueStatusPage{}, err
	}
	providers := make([]management.QueueStatusProvider, len(queues))
	for index := range queues {
		providers[index] = fleetQueueStatus{value: queues[index]}
	}
	reader, err := management.NewStatusReader(management.StatusReaderConfig{
		Queues: providers,
	})
	if err != nil {
		return management.QueueStatusPage{}, ErrFleetUnavailable
	}

	return reader.ListQueues(ctx, request)
}

func (client *FleetClient) Execute(
	ctx context.Context,
	command management.Command,
) (management.CommandResult, error) {
	if ctx == nil || command.Validate() != nil {
		return management.CommandResult{}, ErrInvalidRequest
	}
	remotes, err := client.resolve(ctx)
	if err != nil {
		return management.CommandResult{}, err
	}
	switch command.Target.Kind {
	case management.TargetWorker:
		return client.executeWorker(ctx, remotes, command)
	case management.TargetQueue, management.TargetWorkerGroup:
		return client.executeFleet(ctx, remotes, command)
	}

	return remotes[0].client.Execute(ctx, command)
}

func (client *FleetClient) ListFailures(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	if request.Validate() != nil {
		return management.RecordPage{}, ErrInvalidRequest
	}
	remote, err := client.first(ctx)
	if err != nil {
		return management.RecordPage{}, err
	}

	return remote.client.ListFailures(ctx, request)
}

func (client *FleetClient) ListDeadLetters(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	if request.Validate() != nil {
		return management.RecordPage{}, ErrInvalidRequest
	}
	remote, err := client.first(ctx)
	if err != nil {
		return management.RecordPage{}, err
	}

	return remote.client.ListDeadLetters(ctx, request)
}

func (client *FleetClient) Inspect(
	ctx context.Context,
	request management.InspectRequest,
) (management.JobRecord, error) {
	if request.Validate() != nil {
		return management.JobRecord{}, ErrInvalidRequest
	}
	remote, err := client.first(ctx)
	if err != nil {
		return management.JobRecord{}, err
	}

	return remote.client.Inspect(ctx, request)
}

type fleetRemote struct {
	id     string
	client *Client
}

func (client *FleetClient) first(ctx context.Context) (fleetRemote, error) {
	remotes, err := client.resolve(ctx)
	if err != nil {
		return fleetRemote{}, err
	}

	return remotes[0], nil
}

func (client *FleetClient) resolve(ctx context.Context) ([]fleetRemote, error) {
	if client == nil || nilInterface(client.resolver) || ctx == nil {
		return nil, ErrInvalidFleetConfiguration
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoints, err := client.resolver.ResolveEndpoints(ctx)
	if err != nil {
		return nil, fleetOperationError(ctx, err)
	}
	if len(endpoints) == 0 || len(endpoints) > MaxFleetEndpoints {
		return nil, ErrInvalidFleetEndpoints
	}
	endpoints = slices.Clone(endpoints)
	slices.SortFunc(endpoints, func(left, right Endpoint) int {
		return strings.Compare(left.ID, right.ID)
	})
	remotes := make([]fleetRemote, len(endpoints))
	seenBaseURLs := make(map[string]struct{}, len(endpoints))
	for index, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.ID) != endpoint.ID || endpoint.ID == "" ||
			len(endpoint.ID) > 256 || strings.TrimSpace(endpoint.BaseURL) == "" ||
			(index > 0 && endpoints[index-1].ID == endpoint.ID) {
			return nil, ErrInvalidFleetEndpoints
		}
		remote, remoteErr := NewClient(ClientConfig{
			BaseURL: endpoint.BaseURL, Token: client.token,
			HTTPClient: client.httpClient, MaxResponseBytes: client.maxResponseBytes,
		})
		if remoteErr != nil {
			return nil, ErrInvalidFleetEndpoints
		}
		normalizedBaseURL := *remote.baseURL
		normalizedBaseURL.Host = strings.ToLower(normalizedBaseURL.Host)
		if _, exists := seenBaseURLs[normalizedBaseURL.String()]; exists {
			return nil, ErrInvalidFleetEndpoints
		}
		seenBaseURLs[normalizedBaseURL.String()] = struct{}{}
		remotes[index] = fleetRemote{id: endpoint.ID, client: remote}
	}

	return remotes, nil
}

func (client *FleetClient) workerStatuses(
	ctx context.Context,
	remotes []fleetRemote,
) ([]management.WorkerStatus, error) {
	type result struct {
		workers []management.WorkerStatus
		err     error
	}
	results := make([]result, len(remotes))
	var wait sync.WaitGroup
	wait.Add(len(remotes))
	for index := range remotes {
		go func() {
			defer wait.Done()
			results[index].workers, results[index].err = readAllFleetWorkers(
				ctx,
				remotes[index].client,
			)
		}()
	}
	wait.Wait()
	workersByID := make(map[string]management.WorkerStatus, len(remotes))
	for _, result := range results {
		if result.err != nil {
			return nil, fleetOperationError(ctx, result.err)
		}
		for _, worker := range result.workers {
			if _, exists := workersByID[worker.ID]; exists {
				return nil, ErrInvalidFleetEndpoints
			}
			workersByID[worker.ID] = worker
		}
	}
	workers := make([]management.WorkerStatus, 0, len(workersByID))
	for _, worker := range workersByID {
		workers = append(workers, worker)
	}

	return workers, nil
}

func (client *FleetClient) queueStatuses(
	ctx context.Context,
	remotes []fleetRemote,
) ([]management.QueueStatus, error) {
	type result struct {
		queues []management.QueueStatus
		err    error
	}
	results := make([]result, len(remotes))
	var wait sync.WaitGroup
	wait.Add(len(remotes))
	for index := range remotes {
		go func() {
			defer wait.Done()
			results[index].queues, results[index].err = readAllFleetQueues(
				ctx,
				remotes[index].client,
			)
		}()
	}
	wait.Wait()
	queuesByName := make(map[string]management.QueueStatus, len(remotes))
	for _, result := range results {
		if result.err != nil {
			return nil, fleetOperationError(ctx, result.err)
		}
		for _, queue := range result.queues {
			current, exists := queuesByName[queue.Queue]
			if !exists || queue.ObservedAt.After(current.ObservedAt) {
				queuesByName[queue.Queue] = queue
			}
		}
	}
	queues := make([]management.QueueStatus, 0, len(queuesByName))
	for _, queue := range queuesByName {
		queues = append(queues, queue)
	}

	return queues, nil
}

func readAllFleetWorkers(
	ctx context.Context,
	client *Client,
) ([]management.WorkerStatus, error) {
	items := make([]management.WorkerStatus, 0, 1)
	cursor := ""
	seen := make(map[string]struct{})
	for {
		page, err := client.ListWorkers(ctx, management.StatusPageRequest{
			Cursor: cursor, Limit: management.MaxStatusPageSize,
		})
		if err != nil {
			return nil, fleetOperationError(ctx, err)
		}
		items = append(items, page.Items...)
		if len(items) > MaxFleetEndpoints || page.NextCursor == "" {
			if len(items) > MaxFleetEndpoints {
				return nil, ErrInvalidFleetEndpoints
			}
			return items, nil
		}
		if _, exists := seen[page.NextCursor]; exists {
			return nil, ErrInvalidFleetEndpoints
		}
		seen[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func readAllFleetQueues(
	ctx context.Context,
	client *Client,
) ([]management.QueueStatus, error) {
	items := make([]management.QueueStatus, 0, 1)
	cursor := ""
	seen := make(map[string]struct{})
	for {
		page, err := client.ListQueues(ctx, management.StatusPageRequest{
			Cursor: cursor, Limit: management.MaxStatusPageSize,
		})
		if err != nil {
			return nil, fleetOperationError(ctx, err)
		}
		items = append(items, page.Items...)
		if len(items) > MaxFleetEndpoints || page.NextCursor == "" {
			if len(items) > MaxFleetEndpoints {
				return nil, ErrInvalidFleetEndpoints
			}
			return items, nil
		}
		if _, exists := seen[page.NextCursor]; exists {
			return nil, ErrInvalidFleetEndpoints
		}
		seen[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func (client *FleetClient) executeWorker(
	ctx context.Context,
	remotes []fleetRemote,
	command management.Command,
) (management.CommandResult, error) {
	var target *Client
	for _, remote := range remotes {
		workers, err := readAllFleetWorkers(ctx, remote.client)
		if err != nil {
			return management.CommandResult{}, fleetOperationError(ctx, err)
		}
		for _, worker := range workers {
			if worker.ID == command.Target.Name {
				if target != nil {
					return management.CommandResult{}, ErrInvalidFleetEndpoints
				}
				target = remote.client
			}
		}
	}
	if target != nil {
		return target.Execute(ctx, command)
	}

	return management.CommandResult{}, ErrFleetTargetUnavailable
}

func (client *FleetClient) executeFleet(
	ctx context.Context,
	remotes []fleetRemote,
	command management.Command,
) (management.CommandResult, error) {
	results := make([]management.CommandResult, len(remotes))
	errorsByEndpoint := make([]error, len(remotes))
	var wait sync.WaitGroup
	wait.Add(len(remotes))
	for index := range remotes {
		go func() {
			defer wait.Done()
			results[index], errorsByEndpoint[index] = remotes[index].client.Execute(ctx, command)
		}()
	}
	wait.Wait()
	completed := 0
	unanimous := true
	var aggregateStatus management.CommandResultStatus
	aggregateFailureCode := ""
	completedAt := command.RequestedAt
	for index, result := range results {
		if errorsByEndpoint[index] == nil {
			if completed == 0 {
				aggregateStatus = result.Status
				aggregateFailureCode = result.FailureCode
			} else if result.Status != aggregateStatus ||
				result.FailureCode != aggregateFailureCode {
				unanimous = false
			}
			completed++
		}
		if result.CompletedAt.After(completedAt) {
			completedAt = result.CompletedAt
		}
	}
	if completed == 0 {
		return management.CommandResult{}, fleetOperationError(ctx, ErrFleetUnavailable)
	}
	status := management.CommandPartial
	failureCode := "fleet_partial"
	if completed == len(remotes) && unanimous {
		status = aggregateStatus
		failureCode = aggregateFailureCode
	}
	result := management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "fleet", Protocol: command.Protocol, Status: status,
		FailureCode: failureCode, CompletedAt: completedAt,
	}
	return result, nil
}

func fleetOperationError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return ErrFleetUnavailable
}

type fleetWorkerStatus struct{ value management.WorkerStatus }

func (status fleetWorkerStatus) ObserveWorker(
	context.Context,
) (management.WorkerStatus, error) {
	return status.value, nil
}

type fleetQueueStatus struct{ value management.QueueStatus }

func (status fleetQueueStatus) ObserveQueue(
	context.Context,
) (management.QueueStatus, error) {
	return status.value, nil
}

var (
	_ management.StatusReader = (*FleetClient)(nil)
	_ management.Controller   = (*FleetClient)(nil)
	_ management.RecordReader = (*FleetClient)(nil)
)
