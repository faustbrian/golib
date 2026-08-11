package opensearch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	official "github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchtransport"
)

type benchmarkTransport struct{}

func (benchmarkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"name":"node","cluster_name":"cluster","cluster_uuid":"uuid","version":{"number":"3.2.0"}}`))}, nil
}

// BenchmarkSyntheticInfoTransport measures adapter overhead without a backend.
// Real-backend indexing and query workloads live behind the integration tag.
func BenchmarkSyntheticInfoTransport(b *testing.B) {
	b.Run("adapter", func(b *testing.B) {
		client, err := adapter.New(adapter.Config{Endpoints: []string{"http://127.0.0.1:9200"}, AllowInsecureHTTP: true, Transport: benchmarkTransport{}, TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096})
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = client.Close() })
		b.ReportAllocs()
		for b.Loop() {
			if _, err := client.Info(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("direct_official_client", func(b *testing.B) {
		client := &official.Client{Transport: benchmarkOfficialTransport{roundTripper: benchmarkTransport{}}}
		b.ReportAllocs()
		for b.Loop() {
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			response, err := client.Stream(request)
			if err != nil {
				b.Fatal(err)
			}
			var result struct {
				Name        string `json:"name"`
				ClusterName string `json:"cluster_name"`
				ClusterUUID string `json:"cluster_uuid"`
				Version     struct {
					Number string `json:"number"`
				} `json:"version"`
			}
			err = json.NewDecoder(response.Body).Decode(&result)
			closeErr := response.Body.Close()
			if err != nil || closeErr != nil || result.Name == "" || result.ClusterName == "" || result.ClusterUUID == "" || result.Version.Number == "" {
				b.Fatal(result, err, closeErr)
			}
		}
	})
}

type benchmarkOfficialTransport struct{ roundTripper http.RoundTripper }

func (transport benchmarkOfficialTransport) Perform(request *http.Request) (*http.Response, error) {
	return transport.roundTripper.RoundTrip(request)
}

func (transport benchmarkOfficialTransport) Stream(request *http.Request) (*http.Response, error) {
	return transport.roundTripper.RoundTrip(request)
}

var _ opensearchtransport.Interface = benchmarkOfficialTransport{}
