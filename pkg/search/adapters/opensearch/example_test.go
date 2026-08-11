package opensearch_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	"github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

type exampleTransport struct{}

func (exampleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"locations-v1","_id":"hel","_version":7,"_source":{"name":"Helsinki"},"sort":["hel"]}]}}`,
		)),
	}, nil
}

func ExampleClient_Search() {
	limits := search.DefaultLimits()
	codec, err := search.NewCursorCodec(
		[]byte("replace-with-32-byte-secret-key!!"), time.Now, 4096,
	)
	if err != nil {
		panic(err)
	}
	client, err := opensearch.New(opensearch.Config{
		Endpoints:            []string{"https://search.example.internal:9200"},
		Transport:            exampleTransport{},
		TransportOwnership:   opensearch.TransportBorrowed,
		RequestTimeout:       2 * time.Second,
		MaximumResponseBytes: 1 << 20,
		Search: &opensearch.SearchConfig{
			Limits: limits, CursorCodec: codec,
			Authorizer: opensearch.SearchAuthorizerFunc(func(context.Context, opensearch.SearchAuthorization) error { return nil }),
			Resolver: opensearch.IndexResolverFunc(func(context.Context, string, string, opensearch.IndexAccess) (opensearch.IndexTarget, error) {
				return opensearch.IndexTarget{Name: "locations-v1", PhysicalName: "locations-v1", Fingerprint: "mapping-v1"}, nil
			}),
		},
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.Search(context.Background(), search.Request{
		Tenant: "tenant-a", Index: "locations",
		Query: search.TermQuery{Field: "country", Value: search.StringValue("FI")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Hits()[0].ID, result.Total().Value)
	// Output: hel 1
}
