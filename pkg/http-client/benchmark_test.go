package httpclient

import (
	"context"
	"net/http"
	"testing"
)

func BenchmarkMiddlewarePipeline(b *testing.B) {
	middleware, err := NewRequestMiddleware(MiddlewareOptions{
		Name: "benchmark-request", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		return next(request)
	})
	if err != nil {
		b.Fatal(err)
	}
	pipeline, err := NewPipeline(middleware)
	if err != nil {
		b.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "https://api.example.test/widgets", nil,
	)
	transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: http.NoBody, Request: request,
		}, nil
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, executeErr := pipeline.Execute(request, transport)
		if executeErr != nil {
			b.Fatal(executeErr)
		}
		_ = response.Body.Close()
	}
}

func BenchmarkRetryFirstAttempt(b *testing.B) {
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "benchmark-retry", Layer: MiddlewareClient, MaximumAttempts: 3,
	})
	if err != nil {
		b.Fatal(err)
	}
	pipeline, err := NewPipeline(retry)
	if err != nil {
		b.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "https://api.example.test/widgets", nil,
	)
	transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: http.NoBody, Request: request,
		}, nil
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, executeErr := pipeline.Execute(request, transport)
		if executeErr != nil {
			b.Fatal(executeErr)
		}
		_ = response.Body.Close()
	}
}
