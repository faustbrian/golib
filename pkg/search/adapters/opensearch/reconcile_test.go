package opensearch_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestReconciliationReaderUsesStablePITProjection(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/tenant-a-locations-v3/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pit_id":"pit-reconcile"}`)
		case "/_search":
			_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-a-locations-v3","_id":"a","_version":7,"_source":{"name":"Helsinki"},"sort":["a"]}]}}`)
		case "/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pits":[{"pit_id":"pit-reconcile","successful":true}]}`)
		}
	}))
	t.Cleanup(server.Close)
	client := newSearchClient(t, server, time.Now)
	page, err := client.Read(t.Context(), "tenant-a", "locations", "", 2)
	if err != nil || !page.Done || page.Cursor != "" || len(page.Records) != 1 || page.Records[0].ID != "a" || page.Records[0].Version != 7 || page.Records[0].Digest != search.SourceDigest([]byte(`{"name":"Helsinki"}`)) {
		t.Fatalf("Read() = %#v/%v", page, err)
	}
}

func TestReconciliationReaderRejectsMissingSource(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/tenant-a-locations-v3/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pit_id":"pit-reconcile"}`)
		case "/_search":
			_, _ = io.WriteString(writer, `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-a-locations-v3","_id":"a","_version":7,"sort":["a"]}]}}`)
		case "/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pits":[{"pit_id":"pit-reconcile","successful":true}]}`)
		}
	}))
	t.Cleanup(server.Close)
	client := newSearchClient(t, server, time.Now)
	if _, err := client.Read(t.Context(), "tenant-a", "locations", "", 2); !errors.Is(err, adapter.ErrMalformedResponse) {
		t.Fatalf("Read() error = %v, want ErrMalformedResponse", err)
	}
}
