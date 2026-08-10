package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestLifecycleMutationsReportAmbiguousTransportAndMalformedOutcomes(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition(
		"events-v2",
		json.RawMessage(`{"number_of_shards":1}`),
		json.RawMessage(`{"properties":{"status":{"type":"keyword"}}}`),
		search.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		invoke func(*adapter.Client) error
	}{
		{name: "create index", invoke: func(client *adapter.Client) error {
			return client.CreateIndex(t.Context(), "tenant-a", definition)
		}},
		{name: "start reindex", invoke: func(client *adapter.Client) error {
			_, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "")
			return err
		}},
		{name: "swap alias", invoke: func(client *adapter.Client) error {
			return client.SwapAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2")
		}},
		{name: "add alias", invoke: func(client *adapter.Client) error {
			return client.AddAlias(t.Context(), "tenant-a", "events-write", "events-v2", true)
		}},
		{name: "delete index", invoke: func(client *adapter.Client) error {
			return client.DeleteIndex(t.Context(), "tenant-a", "events-v1")
		}},
		{name: "put template", invoke: func(client *adapter.Client) error {
			return client.PutIndexTemplate(t.Context(), "tenant-a", "events-template", []string{"events-v*"}, 100, definition)
		}},
		{name: "delete template", invoke: func(client *adapter.Client) error {
			return client.DeleteIndexTemplate(t.Context(), "tenant-a", "events-template")
		}},
	}
	faults := []struct {
		name     string
		category adapter.FailureCategory
		response func() (*http.Response, error)
	}{
		{name: "response lost after dispatch", category: adapter.FailureTransport, response: func() (*http.Response, error) {
			return nil, errors.New("connection reset after dispatch")
		}},
		{name: "truncated success response", category: adapter.FailureMalformed, response: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &truncatedResponseBody{}}, nil
		}},
		{name: "malformed success response", category: adapter.FailureMalformed, response: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(http.NoBody)}, nil
		}},
	}

	for _, mutation := range mutations {
		for _, fault := range faults {
			t.Run(mutation.name+"/"+fault.name, func(t *testing.T) {
				t.Parallel()

				client := newLifecycleFailureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					return fault.response()
				}))
				err := mutation.invoke(client)
				var failure *adapter.Failure
				if !errors.As(err, &failure) || failure.Category != fault.category || failure.OutcomeKnown {
					t.Fatalf("mutation error = %#v / %v", failure, err)
				}
			})
		}
	}
}

func TestReadOnlyLifecycleFailuresKeepKnownOutcomes(t *testing.T) {
	t.Parallel()

	reads := []struct {
		name   string
		invoke func(*adapter.Client) error
	}{
		{name: "poll reindex", invoke: func(client *adapter.Client) error {
			_, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "node:task-1")
			return err
		}},
		{name: "verify index", invoke: func(client *adapter.Client) error {
			_, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2")
			return err
		}},
		{name: "resolve alias", invoke: func(client *adapter.Client) error {
			_, err := client.ResolveAlias(t.Context(), "tenant-a", "events-read")
			return err
		}},
	}
	faults := []struct {
		name     string
		category adapter.FailureCategory
		response func() (*http.Response, error)
	}{
		{name: "transport", category: adapter.FailureTransport, response: func() (*http.Response, error) {
			return nil, errors.New("connection reset")
		}},
		{name: "malformed", category: adapter.FailureMalformed, response: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &truncatedResponseBody{}}, nil
		}},
	}

	for _, read := range reads {
		for _, fault := range faults {
			t.Run(read.name+"/"+fault.name, func(t *testing.T) {
				t.Parallel()

				client := newLifecycleFailureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					return fault.response()
				}))
				err := read.invoke(client)
				var failure *adapter.Failure
				if !errors.As(err, &failure) || failure.Category != fault.category || !failure.OutcomeKnown {
					t.Fatalf("read error = %#v / %v", failure, err)
				}
			})
		}
	}
}

func TestReindexRejectsMalformedTaskCursorBeforeNetworkAccess(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newLifecycleFailureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected network access")
	}))
	_, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "%")
	if !errors.Is(err, adapter.ErrLifecycleRejected) {
		t.Fatalf("Reindex() error = %v, want ErrLifecycleRejected", err)
	}
	if requests != 0 {
		t.Fatalf("Reindex() requests = %d, want 0", requests)
	}
}

func newLifecycleFailureClient(t *testing.T, transport http.RoundTripper) *adapter.Client {
	t.Helper()

	client, err := adapter.New(adapter.Config{
		Endpoints:          []string{"https://search.example.test"},
		Transport:          transport,
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second,
		MaximumResponseBytes: 4 << 10,
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(
			func(context.Context, string, []string) error { return nil },
		)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client
}

type truncatedResponseBody struct{ read bool }

func (body *truncatedResponseBody) Read(buffer []byte) (int, error) {
	if !body.read {
		body.read = true
		return copy(buffer, "{"), nil
	}

	return 0, io.ErrUnexpectedEOF
}

func (*truncatedResponseBody) Close() error { return nil }
