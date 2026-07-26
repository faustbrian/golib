package discovery_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestServiceProducesCanonicalDiscoverySnapshot(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "ping")
	service, err := discovery.NewService(discovery.Static(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(snapshot.ETag(), `"`) || !strings.HasSuffix(snapshot.ETag(), `"`) {
		t.Fatalf("ETag = %q", snapshot.ETag())
	}
	if snapshot.Revision() != strings.Trim(snapshot.ETag(), `"`) {
		t.Fatalf("revision = %q, ETag = %q", snapshot.Revision(), snapshot.ETag())
	}
	if len(snapshot.Document().Methods()) != 1 || !strings.Contains(string(snapshot.Bytes()), `"name":"ping"`) {
		t.Fatalf("snapshot = %s", snapshot.Bytes())
	}
	first := snapshot.Bytes()
	first[0] = '['
	if snapshot.Bytes()[0] != '{' {
		t.Fatal("Bytes exposed mutable snapshot storage")
	}
}

func TestServiceAppliesContextVisibilityPolicy(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "secret")
	filter := discovery.FilterFunc(func(_ context.Context, input openrpc.Document) (openrpc.Document, error) {
		return withMethods(t, input, []openrpc.MethodOrReference{}), nil
	})
	service, err := discovery.NewService(discovery.Static(document), filter)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Document().Methods()) != 0 || !strings.Contains(string(snapshot.Bytes()), `"methods":[]`) {
		t.Fatalf("filtered snapshot = %s", snapshot.Bytes())
	}
}

func TestServiceReportsProviderFilterValidationAndCancellationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider discovery.Provider
		filter   discovery.Filter
		want     error
	}{
		{
			name: "provider",
			provider: discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
				return openrpc.Document{}, errors.New("provider details")
			}),
			want: discovery.ErrProvider,
		},
		{
			name:     "filter",
			provider: discovery.Static(testDocument(t, "ok")),
			filter: discovery.FilterFunc(func(context.Context, openrpc.Document) (openrpc.Document, error) {
				return openrpc.Document{}, errors.New("filter details")
			}),
			want: discovery.ErrFilter,
		},
		{
			name:     "invalid",
			provider: discovery.Static(openrpc.Document{}),
			want:     discovery.ErrInvalidDocument,
		},
		{
			name: "semantic validation",
			provider: func() discovery.Provider {
				document := testDocument(t, "duplicate")
				method := document.Methods()[0]
				return discovery.Static(withMethods(t, document, []openrpc.MethodOrReference{method, method}))
			}(),
			want: discovery.ErrInvalidDocument,
		},
	}
	for _, test := range tests {
		service, err := discovery.NewService(test.provider, test.filter)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Discover(context.Background()); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}

	service, err := discovery.NewService(discovery.Static(testDocument(t, "ok")), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestNewServiceRequiresProvider(t *testing.T) {
	t.Parallel()

	if discovery.MethodName != "rpc.discover" {
		t.Fatalf("MethodName = %q", discovery.MethodName)
	}
	if _, err := discovery.NewService(nil, nil); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("NewService error = %v", err)
	}
}

func TestServiceEnforcesCanonicalOutputBudget(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "bounded")
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	options := discovery.DefaultOptions()
	options.MaxOutputBytes = len(encoded) - 1
	service, err := discovery.NewServiceWithOptions(discovery.Static(document), nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Discover(context.Background()); !errors.Is(err, discovery.ErrDiscoveryLimit) {
		t.Fatalf("output limit error = %v", err)
	}

	options.MaxOutputBytes = len(encoded)
	service, err = discovery.NewServiceWithOptions(discovery.Static(document), nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Discover(context.Background()); err != nil {
		t.Fatalf("exact output limit failed: %v", err)
	}

	options.MaxOutputBytes = 0
	if _, err := discovery.NewServiceWithOptions(discovery.Static(document), nil, options); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("invalid output options error = %v", err)
	}
	options = discovery.DefaultOptions()
	options.Validation.MaxDiagnostics = 0
	if _, err := discovery.NewServiceWithOptions(discovery.Static(document), nil, options); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("invalid diagnostic options error = %v", err)
	}
	options = discovery.DefaultOptions()
	options.Validation.Mode = validate.Mode(255)
	if _, err := discovery.NewServiceWithOptions(discovery.Static(document), nil, options); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("invalid validation mode error = %v", err)
	}
	options.Validation.Mode = validate.FailFast
	options.Validation.MaxDiagnostics = 1
	options.MaxOutputBytes = len(encoded)
	service, err = discovery.NewServiceWithOptions(discovery.Static(document), nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Discover(context.Background()); err != nil {
		t.Fatalf("exact fail-fast options failed: %v", err)
	}
}

func TestServiceRejectsInvalidStateAndPreservesContextErrors(t *testing.T) {
	t.Parallel()

	var nilService *discovery.Service
	if _, err := nilService.Discover(context.Background()); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("nil service error = %v", err)
	}
	zeroService := &discovery.Service{}
	if _, err := zeroService.Discover(context.Background()); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("zero service error = %v", err)
	}
	service, err := discovery.NewService(discovery.Static(testDocument(t, "valid")), nil)
	if err != nil {
		t.Fatal(err)
	}
	var invalidContext context.Context
	if _, err := service.Discover(invalidContext); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("nil context error = %v", err)
	}

	providerContext, cancelProvider := context.WithCancel(context.Background())
	providerService, err := discovery.NewService(discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
		cancelProvider()
		return openrpc.Document{}, errors.New("provider detail")
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := providerService.Discover(providerContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider context error = %v", err)
	}

	filterContext, cancelFilter := context.WithCancel(context.Background())
	filterService, err := discovery.NewService(discovery.Static(testDocument(t, "valid")), discovery.FilterFunc(
		func(context.Context, openrpc.Document) (openrpc.Document, error) {
			cancelFilter()
			return openrpc.Document{}, errors.New("filter detail")
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filterService.Discover(filterContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("filter context error = %v", err)
	}

	postFilterContext, cancelPostFilter := context.WithCancel(context.Background())
	postFilterService, err := discovery.NewService(discovery.Static(testDocument(t, "valid")), discovery.FilterFunc(
		func(_ context.Context, document openrpc.Document) (openrpc.Document, error) {
			cancelPostFilter()
			return document, nil
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postFilterService.Discover(postFilterContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-filter context error = %v", err)
	}
}

func testDocument(t *testing.T, methodName string) openrpc.Document {
	t.Helper()
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Discovery", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:   methodName,
		Params: []openrpc.ContentDescriptorOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{openrpc.MethodValue(method)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func withMethods(t *testing.T, document openrpc.Document, methods []openrpc.MethodOrReference) openrpc.Document {
	t.Helper()
	schemaURI, explicitSchema := document.SchemaURI()
	var schema *string
	if explicitSchema {
		schema = &schemaURI
	}
	servers, hasServers := document.Servers()
	components, hasComponents := document.Components()
	var componentInput *openrpc.Components
	if hasComponents {
		componentInput = &components
	}
	externalDocs, hasExternalDocs := document.ExternalDocs()
	var docs *openrpc.ExternalDocumentation
	if hasExternalDocs {
		docs = &externalDocs
	}
	info := document.Info()
	filtered, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version:       document.Version(),
		SchemaURI:     schema,
		Info:          &info,
		ExternalDocs:  docs,
		Servers:       servers,
		HasServers:    hasServers,
		Methods:       methods,
		Components:    componentInput,
		Extensions:    document.Extensions(),
		UnknownFields: document.UnknownFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return filtered
}
