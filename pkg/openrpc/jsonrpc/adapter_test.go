package jsonrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	adapter "github.com/faustbrian/golib/pkg/openrpc/jsonrpc"
)

func TestDiscoveryHandlerReturnsCanonicalRawDocument(t *testing.T) {
	t.Parallel()

	service, err := discovery.NewService(discovery.Static(document(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := adapter.DiscoveryHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handler(context.Background(), json.RawMessage(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := result.(json.RawMessage)
	if !ok || !json.Valid(raw) || string(raw) == `{}` {
		t.Fatalf("result = %#v", result)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded["openrpc"] != "1.4.1" {
		t.Fatalf("document = %#v, error = %v", decoded, err)
	}
}

func TestDiscoveryHandlerValidatesParamsAndSanitizesFailures(t *testing.T) {
	t.Parallel()

	service, err := discovery.NewService(discovery.Static(document(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := adapter.DiscoveryHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []json.RawMessage{nil, json.RawMessage(`[]`), json.RawMessage(`{}`)} {
		if _, err := handler(context.Background(), params); err != nil {
			t.Errorf("params %s error = %v", params, err)
		}
	}
	for _, params := range []json.RawMessage{json.RawMessage(`[1]`), json.RawMessage(`{"x":1}`), json.RawMessage(`null`), json.RawMessage(`[`)} {
		if _, err := handler(context.Background(), params); !errors.Is(err, adapter.ErrInvalidParams) {
			t.Errorf("params %s error = %v", params, err)
		}
	}
	failing, err := discovery.NewService(discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
		return openrpc.Document{}, errors.New("secret provider detail")
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err = adapter.DiscoveryHandler(failing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), nil); !errors.Is(err, adapter.ErrDiscovery) || err.Error() != adapter.ErrDiscovery.Error() {
		t.Fatalf("failure = %v", err)
	}
}

func TestDiscoveryHandlerRequiresServiceAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	if _, err := adapter.DiscoveryHandler(nil); !errors.Is(err, adapter.ErrInvalidAdapter) {
		t.Fatalf("DiscoveryHandler error = %v", err)
	}
	service, err := discovery.NewService(discovery.Static(document(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := adapter.DiscoveryHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handler(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

type systemHandler func(context.Context, json.RawMessage) (any, error)

type systemRegistry struct {
	name    string
	handler systemHandler
	err     error
}

func (registry *systemRegistry) RegisterSystem(name string, handler systemHandler) error {
	registry.name = name
	registry.handler = handler
	return registry.err
}

func TestRegisterDiscoveryUsesTypedSystemMethodContract(t *testing.T) {
	t.Parallel()

	service, err := discovery.NewService(discovery.Static(document(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := &systemRegistry{}
	if err := adapter.RegisterDiscovery(registry, service); err != nil {
		t.Fatal(err)
	}
	if registry.name != discovery.MethodName || registry.handler == nil {
		t.Fatalf("registration = (%q, %#v)", registry.name, registry.handler)
	}
	result, err := registry.handler(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := result.(json.RawMessage); !ok || !json.Valid(raw) {
		t.Fatalf("result = %#v", result)
	}

	failing := &systemRegistry{err: errors.New("registry detail")}
	if err := adapter.RegisterDiscovery(failing, service); !errors.Is(err, adapter.ErrRegistration) ||
		err.Error() != adapter.ErrRegistration.Error() {
		t.Fatalf("registration error = %v", err)
	}
	if err := adapter.RegisterDiscovery[systemHandler](nil, service); !errors.Is(err, adapter.ErrInvalidAdapter) {
		t.Fatalf("nil registry error = %v", err)
	}
	if err := adapter.RegisterDiscovery(registry, nil); !errors.Is(err, adapter.ErrInvalidAdapter) {
		t.Fatalf("nil discoverer error = %v", err)
	}
}

func TestDiscoveryHandlerRejectsNilContext(t *testing.T) {
	t.Parallel()

	service, err := discovery.NewService(discovery.Static(document(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := adapter.DiscoveryHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	var invalidContext context.Context
	if _, err := handler(invalidContext, nil); !errors.Is(err, adapter.ErrInvalidAdapter) {
		t.Fatalf("nil context error = %v", err)
	}
}

func document(t *testing.T) openrpc.Document {
	t.Helper()
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Adapter", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{Version: version, Info: &info, Methods: []openrpc.MethodOrReference{}})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
