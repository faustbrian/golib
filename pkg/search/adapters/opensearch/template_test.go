package opensearch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
