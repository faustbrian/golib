package opensearch_test

import (
	"bytes"
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

func TestWritesFailClosedWithoutDurableGuard(t *testing.T) {
	t.Parallel()

	resolverCalls, transportCalls := 0, 0
	client := writeAuthorizationClient(t, nil, &resolverCalls, &transportCalls, nil)
	capabilities, err := client.Capabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.ExternalVersion || capabilities.BulkPartialOutcomes {
		t.Fatalf("write capabilities without guard = %#v", capabilities)
	}
	operation := guardedWriteOperation(t, "event-1", 7, `{"name":"current"}`)
	if _, err := client.Write(t.Context(), operation, search.RefreshWaitFor); !errors.Is(err, adapter.ErrWriteDisabled) {
		t.Fatalf("Write() error = %v, want ErrWriteDisabled", err)
	}
	if _, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: search.RefreshWaitFor}); !errors.Is(err, adapter.ErrWriteDisabled) {
		t.Fatalf("Bulk() error = %v, want ErrWriteDisabled", err)
	}
	if resolverCalls != 0 || transportCalls != 0 {
		t.Fatalf("disabled writes reached resolver/transport: %d/%d", resolverCalls, transportCalls)
	}
}

func TestDurableWriteGuardReceivesImmutableBoundedBulkBeforeResolution(t *testing.T) {
	t.Parallel()

	resolverCalls, transportCalls, guardCalls := 0, 0, 0
	first := guardedWriteOperation(t, "event-1", 7, `{"name":"first"}`)
	second := search.DeleteDocument("tenant-a", "events", "event-2", 11)
	request := search.BulkRequest{Operations: []search.WriteOperation{first, second}, Refresh: search.RefreshWaitFor}
	guard := adapter.WriteGuardFunc(func(_ context.Context, authorization adapter.WriteAuthorization) error {
		guardCalls++
		if resolverCalls != 0 || transportCalls != 0 {
			t.Fatalf("guard ran after resolver/transport: %d/%d", resolverCalls, transportCalls)
		}
		if authorization.Refresh() != request.Refresh {
			t.Fatalf("guard refresh = %q", authorization.Refresh())
		}
		operations := authorization.Operations()
		if len(operations) != 2 || operations[0].Tenant != "tenant-a" || operations[0].Index != "events" ||
			operations[0].ID != "event-1" || operations[0].Version != 7 || operations[0].Action != search.ActionIndex ||
			string(operations[0].Source) != `{"name":"first"}` || operations[1].Tenant != second.Tenant ||
			operations[1].Index != second.Index || operations[1].ID != second.ID || operations[1].Version != second.Version ||
			operations[1].Action != second.Action || len(operations[1].Source) != 0 {
			t.Fatalf("guard operations = %#v", operations)
		}
		operations[0].Tenant = "mutated"
		operations[0].Source[2] = 'x'
		if fresh := authorization.Operations(); fresh[0].Tenant != "tenant-a" || string(fresh[0].Source) != `{"name":"first"}` {
			t.Fatalf("authorization accessor leaked mutable state = %#v", fresh[0])
		}
		return nil
	})
	var executed []byte
	client := writeAuthorizationClient(t, guard, &resolverCalls, &transportCalls, func(body []byte) {
		executed = bytes.Clone(body)
	})
	result, err := client.Bulk(t.Context(), request)
	if err != nil || result.Partial() {
		t.Fatalf("Bulk() = %#v/%v", result.Items(), err)
	}
	if guardCalls != 1 || resolverCalls != 2 || transportCalls != 1 {
		t.Fatalf("guard/resolver/transport calls = %d/%d/%d", guardCalls, resolverCalls, transportCalls)
	}
	if !bytes.Contains(executed, []byte(`{"name":"first"}`)) || bytes.Contains(executed, []byte(`{"xame":"first"}`)) {
		t.Fatalf("guard mutation reached transport: %s", executed)
	}
}

func TestCallerCannotMutateWriteAfterGuardSnapshot(t *testing.T) {
	t.Parallel()

	entered, release := make(chan struct{}), make(chan struct{})
	guard := adapter.WriteGuardFunc(func(context.Context, adapter.WriteAuthorization) error {
		close(entered)
		<-release
		return nil
	})
	resolverCalls, transportCalls := 0, 0
	var executed []byte
	client := writeAuthorizationClient(t, guard, &resolverCalls, &transportCalls, func(body []byte) {
		executed = bytes.Clone(body)
	})
	source := []byte(`{"name":"current"}`)
	operation := guardedWriteOperation(t, "event-1", 7, string(source))
	operation.Source = source
	result := make(chan error, 1)
	go func() {
		_, err := client.Write(t.Context(), operation, search.RefreshWaitFor)
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("write guard was not reached")
	}
	copy(source, `{"name":"poison!"}`)
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if string(executed) != `{"name":"current"}` {
		t.Fatalf("caller mutation reached transport: %s", executed)
	}
}

func TestWriteGuardFailuresAreSanitizedAndClassifiedBeforeDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		want  error
	}{
		{name: "policy detail", cause: errors.New("database password and row detail"), want: adapter.ErrWriteDenied},
		{name: "cancelled", cause: context.Canceled, want: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, want: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resolverCalls, transportCalls := 0, 0
			client := writeAuthorizationClient(t, adapter.WriteGuardFunc(func(context.Context, adapter.WriteAuthorization) error {
				return test.cause
			}), &resolverCalls, &transportCalls, nil)
			_, err := client.Write(t.Context(), guardedWriteOperation(t, "event-1", 7, `{"name":"current"}`), search.RefreshNone)
			if !errors.Is(err, test.want) {
				t.Fatalf("Write() error = %v, want %v", err, test.want)
			}
			if errors.Is(test.want, adapter.ErrWriteDenied) && bytes.Contains([]byte(err.Error()), []byte("password")) {
				t.Fatalf("Write() leaked guard detail: %v", err)
			}
			if resolverCalls != 0 || transportCalls != 0 {
				t.Fatalf("denied write reached resolver/transport: %d/%d", resolverCalls, transportCalls)
			}
		})
	}
}

func TestBulkWriteGuardDenialStopsResolutionAndDispatch(t *testing.T) {
	t.Parallel()
	resolverCalls, transportCalls := 0, 0
	client := writeAuthorizationClient(t, adapter.WriteGuardFunc(func(context.Context, adapter.WriteAuthorization) error {
		return errors.New("durable version policy denied")
	}), &resolverCalls, &transportCalls, nil)
	document, err := search.NewDocument("tenant-a", "events", "event-1", 7, json.RawMessage(`{"value":"safe"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, bulkErr := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshWaitFor}); !errors.Is(bulkErr, adapter.ErrWriteDenied) {
		t.Fatalf("Bulk() error = %v, want ErrWriteDenied", bulkErr)
	}
	if resolverCalls != 0 || transportCalls != 0 {
		t.Fatalf("denied bulk resolution/transport calls = %d/%d", resolverCalls, transportCalls)
	}
}

func writeAuthorizationClient(
	t *testing.T,
	guard adapter.WriteGuard,
	resolverCalls, transportCalls *int,
	inspect func([]byte),
) *adapter.Client {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			*transportCalls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if inspect != nil {
				inspect(body)
			}
			status := http.StatusCreated
			response := `{"_index":"events-v1","_id":"event-1","_version":7,"result":"created"}`
			if request.URL.Path == "/_bulk" {
				status = http.StatusOK
				response = `{"took":1,"errors":false,"items":[{"index":{"_index":"events-v1","_id":"event-1","_version":7,"status":201,"result":"created"}},{"delete":{"_index":"events-v1","_id":"event-2","_version":11,"status":200,"result":"deleted"}}]}`
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(response))}, nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec, Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: guard,
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				*resolverCalls++
				return adapter.IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition-v1"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func guardedWriteOperation(t *testing.T, id string, version uint64, source string) search.WriteOperation {
	t.Helper()
	document, err := search.NewDocument("tenant-a", "events", id, version, []byte(source), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return search.IndexDocument(document)
}
