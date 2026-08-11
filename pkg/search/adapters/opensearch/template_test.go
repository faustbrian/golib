package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestIndexTemplatesUseAuthorizedComposableTemplateAPI(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(context.Background())
		body, _ := io.ReadAll(request.Body)
		if request.Method == http.MethodPut && !json.Valid(body) {
			t.Errorf("template body is invalid: %s", body)
		}
		_, _ = io.WriteString(writer, `{"acknowledged":true}`)
	}))
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 8192,
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	definition, _ := search.NewIndexDefinition("events-v2", json.RawMessage(`{"index":{"number_of_shards":1}}`), json.RawMessage(`{"properties":{"status":{"type":"keyword"}}}`), search.DefaultLimits())
	if err := client.PutIndexTemplate(t.Context(), "tenant-a", "events-template", []string{"events-v*"}, 100, definition); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteIndexTemplate(t.Context(), "tenant-a", "events-template"); err != nil {
		t.Fatal(err)
	}
	put, deleteRequest := <-requests, <-requests
	if put.Method != http.MethodPut || put.URL.Path != "/_index_template/events-template" || deleteRequest.Method != http.MethodDelete || deleteRequest.URL.Path != "/_index_template/events-template" {
		t.Fatalf("template requests = %s %s / %s %s", put.Method, put.URL.Path, deleteRequest.Method, deleteRequest.URL.Path)
	}
}

func TestDeleteIndexTemplateTreatsOnlyExactMissingTemplateAsIdempotent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		response string
		wantErr  bool
	}{
		"missing template": {
			response: `{"error":{"type":"resource_not_found_exception"},"status":404}`,
		},
		"missing index": {
			response: `{"error":{"type":"index_not_found_exception"},"status":404}`,
			wantErr:  true,
		},
		"unattributed not found": {
			response: `{}`,
			wantErr:  true,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(writer, test.response)
			}))
			t.Cleanup(server.Close)
			client, err := adapter.New(adapter.Config{
				Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed,
				RequestTimeout: time.Second, MaximumResponseBytes: 8192,
				Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })

			err = client.DeleteIndexTemplate(t.Context(), "tenant-a", "events-template")
			if test.wantErr {
				var failure *adapter.Failure
				if !errors.As(err, &failure) || failure.Operation != adapter.OperationTemplate {
					t.Fatalf("DeleteIndexTemplate() error = %v, want explicit template failure", err)
				}
			} else if err != nil {
				t.Fatalf("DeleteIndexTemplate() error = %v", err)
			}
		})
	}
}

func TestIndexTemplateOperationsRejectOversizedTenantBeforeAuthorizationOrIO(t *testing.T) {
	t.Parallel()

	authorizations, requests := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(writer, `{"acknowledged":true}`)
	}))
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 8192,
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
			authorizations++
			return nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tenant := strings.Repeat("t", search.DefaultLimits().MaxTenantBytes+1)

	if err := client.PutIndexTemplate(t.Context(), tenant, "events-template", []string{"events-*"}, 1, definition); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("PutIndexTemplate() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if err := client.DeleteIndexTemplate(t.Context(), tenant, "events-template"); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("DeleteIndexTemplate() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if authorizations != 0 || requests != 0 {
		t.Fatalf("oversized tenant reached authorization/IO = %d/%d", authorizations, requests)
	}
}
