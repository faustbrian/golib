package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/dataplane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/managementhttp"
)

const (
	maxManagementTenants     = 1_000
	maxManagementURLBytes    = 2_048
	maxManagementPathBytes   = 4_096
	maxManagementTokenBytes  = 4_096
	managementCommandTimeout = 30 * time.Second
)

var (
	// ErrInvalidManagementRuntime is a stable secret-safe startup error.
	ErrInvalidManagementRuntime = errors.New("queue-control-plane: invalid management runtime")
	// ErrManagementTenantUnavailable reports an unconfigured tenant endpoint.
	ErrManagementTenantUnavailable = errors.New("queue-control-plane: management tenant unavailable")
)

type managementRuntime struct {
	Workers    apihttp.RemoteWorkerSource
	Queues     apihttp.QueueSource
	Records    apihttp.RecordSource
	Dispatcher control.Dispatcher
}

type managementFileOpener func(string) (io.ReadCloser, error)
type managementStatusFactory func(string, string) (queue.StatusReader, error)
type managementSourceFactory func(dataplane.StatusReaderResolver) (*dataplane.StatusSource, error)
type managementRecordFactory func(dataplane.RecordReaderResolver) (*dataplane.RecordSource, error)
type managementFleetFactory func(dataplane.WorkerStatusSource) (*dataplane.FleetSource, error)
type managementDispatcherFactory func(dataplane.ControllerResolver) (control.Dispatcher, error)

type managementTenantDocument struct {
	Tenants []managementTenantEntry `json:"tenants"`
}

type managementTenantEntry struct {
	ID        string `json:"id"`
	BaseURL   string `json:"base_url"`
	TokenFile string `json:"token_file"`
}

type managementStatusResolver struct {
	readers     map[string]queue.StatusReader
	controllers map[string]queue.Controller
	records     map[string]queue.RecordReader
}

func loadManagementRuntime(
	path string,
	maxBytes int64,
	open managementFileOpener,
	newStatus managementStatusFactory,
) (managementRuntime, error) {
	return loadManagementRuntimeWithSource(
		path, maxBytes, open, newStatus, dataplane.NewStatusSource,
		dataplane.NewRecordSource, dataplane.NewFleetSource,
		newProductionManagementDispatcher,
	)
}

func loadManagementRuntimeWithSource(
	path string,
	maxBytes int64,
	open managementFileOpener,
	newStatus managementStatusFactory,
	newSource managementSourceFactory,
	newRecords managementRecordFactory,
	newFleet managementFleetFactory,
	newDispatcher managementDispatcherFactory,
) (managementRuntime, error) {
	if strings.TrimSpace(path) == "" || maxBytes < 1 ||
		maxBytes > defaultAccessDocumentSize || open == nil || newStatus == nil ||
		newSource == nil || newRecords == nil || newFleet == nil || newDispatcher == nil {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	file, err := open(path)
	if err != nil {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	defer func() { _ = file.Close() }()
	data, err := readBoundedManagement(file, maxBytes)
	if err != nil {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	var document managementTenantDocument
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decodeManagementEOF(decoder) != nil ||
		len(document.Tenants) == 0 || len(document.Tenants) > maxManagementTenants {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}

	readers := make(map[string]queue.StatusReader, len(document.Tenants))
	controllers := make(map[string]queue.Controller, len(document.Tenants))
	records := make(map[string]queue.RecordReader, len(document.Tenants))
	for _, tenant := range document.Tenants {
		if invalidManagementTenant(tenant) {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		if _, exists := readers[tenant.ID]; exists {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		token, err := readManagementToken(tenant.TokenFile, open)
		if err != nil {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		reader, err := newStatus(tenant.BaseURL, token)
		if err != nil || missingDependency(reader) {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		controller, ok := reader.(queue.Controller)
		if !ok || missingDependency(controller) {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		recordReader, ok := reader.(queue.RecordReader)
		if !ok || missingDependency(recordReader) {
			return managementRuntime{}, ErrInvalidManagementRuntime
		}
		readers[tenant.ID] = reader
		controllers[tenant.ID] = controller
		records[tenant.ID] = recordReader
	}
	resolver := &managementStatusResolver{
		readers: readers, controllers: controllers, records: records,
	}
	source, err := newSource(resolver)
	if err != nil || missingDependency(source) {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	recordSource, err := newRecords(resolver)
	if err != nil || missingDependency(recordSource) {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	workers, err := newFleet(source)
	if err != nil || missingDependency(workers) {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}
	dispatcher, err := newDispatcher(resolver)
	if err != nil || missingDependency(dispatcher) {
		return managementRuntime{}, ErrInvalidManagementRuntime
	}

	return managementRuntime{
		Workers: workers, Queues: source, Records: recordSource,
		Dispatcher: dispatcher,
	}, nil
}

func (r *managementStatusResolver) ResolveRecordReader(
	_ context.Context,
	tenant string,
) (queue.RecordReader, error) {
	reader, exists := r.records[tenant]
	if !exists {
		return nil, ErrManagementTenantUnavailable
	}

	return reader, nil
}

func (r *managementStatusResolver) ResolveController(
	_ context.Context,
	tenant string,
) (queue.Controller, error) {
	controller, exists := r.controllers[tenant]
	if !exists {
		return nil, ErrManagementTenantUnavailable
	}

	return controller, nil
}

func (r *managementStatusResolver) ResolveStatusReader(
	_ context.Context,
	stringTenant string,
) (queue.StatusReader, error) {
	reader, exists := r.readers[stringTenant]
	if !exists {
		return nil, ErrManagementTenantUnavailable
	}

	return reader, nil
}

func readManagementToken(path string, open managementFileOpener) (string, error) {
	file, err := open(path)
	if err != nil {
		return "", ErrInvalidManagementRuntime
	}
	defer func() { _ = file.Close() }()
	data, err := readBoundedManagement(file, maxManagementTokenBytes)
	if err != nil {
		return "", ErrInvalidManagementRuntime
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", ErrInvalidManagementRuntime
	}

	return token, nil
}

func readBoundedManagement(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, ErrInvalidManagementRuntime
	}

	return data, nil
}

func decodeManagementEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidManagementRuntime
	}

	return nil
}

func invalidManagementTenant(tenant managementTenantEntry) bool {
	return invalidManagementTenantWithParser(tenant, url.Parse)
}

func invalidManagementTenantWithParser(
	tenant managementTenantEntry,
	parse func(string) (*url.URL, error),
) bool {
	if strings.TrimSpace(tenant.ID) == "" || len(tenant.ID) > controlplane.MaxIdentityBytes ||
		strings.TrimSpace(tenant.BaseURL) == "" || len(tenant.BaseURL) > maxManagementURLBytes ||
		strings.TrimSpace(tenant.TokenFile) == "" || len(tenant.TokenFile) > maxManagementPathBytes {
		return true
	}
	endpoint, err := parse(tenant.BaseURL)
	if err != nil || endpoint == nil {
		return true
	}

	return endpoint.Scheme != "https" || endpoint.Host == "" ||
		endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		strings.TrimSpace(tenant.TokenFile) == "" || len(tenant.TokenFile) > maxManagementPathBytes
}

func loadProductionManagement(path string, maxBytes int64) (managementRuntime, error) {
	return loadManagementRuntime(path, maxBytes, openManagementFile, newProductionManagementStatus)
}

func openManagementFile(path string) (io.ReadCloser, error) {
	return os.Open(path) //nolint:gosec // The operator explicitly configures this bounded file path.
}

func newProductionManagementStatus(baseURL, token string) (queue.StatusReader, error) {
	return managementhttp.NewClient(managementhttp.ClientConfig{BaseURL: baseURL, Token: token})
}

func newProductionManagementDispatcher(
	resolver dataplane.ControllerResolver,
) (control.Dispatcher, error) {
	return dataplane.NewControllerDispatcher(
		resolver,
		queue.ProtocolVersion{Major: 1},
		managementCommandTimeout,
		time.Now,
	)
}
