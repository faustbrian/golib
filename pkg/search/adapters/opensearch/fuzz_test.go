package opensearch_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

type fuzzResponseTransport struct{ body string }

func (transport fuzzResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(transport.body))}, nil
}

func FuzzInfoResponseBoundary(f *testing.F) {
	f.Add(`{"name":"node","cluster_name":"cluster","cluster_uuid":"uuid","version":{"number":"3.2.0"}}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 8192 {
			t.Skip()
		}
		client, err := adapter.New(adapter.Config{Endpoints: []string{"http://127.0.0.1:9200"}, AllowInsecureHTTP: true, Transport: fuzzResponseTransport{body: body}, TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = client.Info(t.Context())
		_ = client.Close()
	})
}
