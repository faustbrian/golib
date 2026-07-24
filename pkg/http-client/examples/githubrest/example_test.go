package githubrest_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	httpclient "github.com/faustbrian/golib/pkg/http-client"
)

func Example() {
	type repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/octocat/Hello-World" ||
			request.Header.Get("Authorization") != "Bearer example-token" ||
			request.Header.Get("Accept") != "application/vnd.github+json" {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":1,"full_name":"octocat/Hello-World"}`)
	}))
	defer server.Close()

	bearer, err := httpclient.NewBearerAuth("example-token")
	if err != nil {
		panic(err)
	}
	authentication, err := httpclient.NewAuthenticationMiddleware(
		httpclient.AuthenticationOptions{
			Name: "github-auth", Layer: httpclient.MiddlewareClient,
		},
		bearer,
	)
	if err != nil {
		panic(err)
	}
	retry, err := httpclient.NewRetryMiddleware(httpclient.RetryOptions{
		Name: "github-read-retry", Layer: httpclient.MiddlewareEndpoint,
		MaximumAttempts: 3, MaximumElapsed: 5 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	middleware := append(authentication, retry)
	client, err := httpclient.New(httpclient.Config{
		Middleware: middleware,
		Transport:  server.Client().Transport,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = client.Close() }()

	getRepository := func(ctx context.Context, owner string, name string) (repository, error) {
		spec, specErr := httpclient.NewRequestSpec(
			server.URL+"/", "repos/"+owner+"/"+name,
		)
		if specErr != nil {
			return repository{}, specErr
		}
		spec, specErr = spec.WithHeader(
			httpclient.LayerEndpoint, "Accept", "application/vnd.github+json",
		)
		if specErr != nil {
			return repository{}, specErr
		}
		request, requestErr := spec.Build(ctx, http.MethodGet)
		if requestErr != nil {
			return repository{}, requestErr
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return repository{}, requestErr
		}
		if statusErr := httpclient.ClassifyResponse(response, httpclient.StatusOptions{}); statusErr != nil {
			return repository{}, statusErr
		}
		return httpclient.DecodeJSONResponse[repository](response, httpclient.DecodeOptions{
			MaximumBodyBytes: 1 << 20,
		})
	}

	result, err := getRepository(context.Background(), "octocat", "Hello-World")
	if err != nil {
		panic(err)
	}
	fmt.Println(result.ID, result.FullName)
	// Output:
	// 1 octocat/Hello-World
}
