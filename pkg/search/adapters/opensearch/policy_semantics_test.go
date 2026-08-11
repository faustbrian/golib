package opensearch

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestPolicyCallbacksPreserveCancellationClassification(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 1},
	}
	operation := search.IndexDocument(mustInternalPolicyDocument(t))

	for name, run := range map[string]func(*Client) error{
		"search authorizer": func(client *Client) error {
			client.search.Authorizer = SearchAuthorizerFunc(func(context.Context, SearchAuthorization) error {
				return context.DeadlineExceeded
			})
			_, err := client.Search(t.Context(), request)
			return err
		},
		"search resolver": func(client *Client) error {
			client.search.Resolver = internalResolver{err: context.DeadlineExceeded}
			_, err := client.Search(t.Context(), request)
			return err
		},
		"write resolver": func(client *Client) error {
			client.search.Resolver = internalResolver{err: context.DeadlineExceeded}
			_, err := client.Write(t.Context(), operation, search.RefreshNone)
			return err
		},
		"bulk resolver": func(client *Client) error {
			client.search.Resolver = internalResolver{err: context.DeadlineExceeded}
			_, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: search.RefreshNone})
			return err
		},
	} {
		run := run
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := internalClient(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("policy failure reached transport")
				return nil, nil
			}, internalResolver{target: IndexTarget{Name: "documents", PhysicalName: "documents", Fingerprint: "v1"}}, nil)
			if err := run(client); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("policy error = %v, want preserved deadline", err)
			}
		})
	}

	lifecycle := internalClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("lifecycle policy failure reached transport")
		return nil, nil
	}, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
		return context.Canceled
	}))
	if _, err := lifecycle.ResolveAlias(t.Context(), "tenant", "documents"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lifecycle policy error = %v, want preserved cancellation", err)
	}
}

func TestSearchConfigDoesNotRequireDeprecatedClock(t *testing.T) {
	t.Parallel()

	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoints: []string{"https://search.example"}, Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return internalResponse(200, `{}`), nil
		}), TransportOwnership: TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Search: &SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec,
			Resolver: internalResolver{target: IndexTarget{Name: "documents", PhysicalName: "documents", Fingerprint: "v1"}},
		},
	})
	if err != nil {
		t.Fatalf("New() with omitted deprecated Clock = %v", err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}

func mustInternalPolicyDocument(t *testing.T) search.Document {
	t.Helper()
	document, err := search.NewDocument("tenant", "documents", "id", 1, []byte(`{"value":"safe"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return document
}
