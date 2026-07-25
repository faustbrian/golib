package httpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type benchmarkTelemetryObserver struct{}

func (benchmarkTelemetryObserver) Start(ctx context.Context, _ TelemetryEvent) context.Context {
	return ctx
}

func (benchmarkTelemetryObserver) Finish(context.Context, TelemetryEvent) {}

type benchmarkRetryClock struct{ now time.Time }

func (clock *benchmarkRetryClock) Now() time.Time { return clock.now }

func (clock *benchmarkRetryClock) Wait(ctx context.Context, duration time.Duration) error {
	clock.now = clock.now.Add(duration)
	return ctx.Err()
}

type benchmarkCacheLoadBarrier struct {
	CacheStore
	remaining atomic.Int64
	loaded    chan struct{}
}

func (store *benchmarkCacheLoadBarrier) Load(ctx context.Context, key string) ([]CacheEntry, error) {
	entries, err := store.CacheStore.Load(ctx, key)
	if store.remaining.Add(-1) == 0 {
		close(store.loaded)
	}
	return entries, err
}

func BenchmarkRequestPolicyPaths(b *testing.B) {
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/widgets", nil)
	terminal := Next(func(request *http.Request) (*http.Response, error) {
		return benchmarkResponse(request, http.StatusNoContent, nil, nil), nil
	})

	b.Run("direct", func(b *testing.B) {
		pipeline, err := NewPipeline()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPipeline(b, pipeline, request, terminal)
	})

	b.Run("instrumented", func(b *testing.B) {
		middleware, err := newTelemetryMiddleware(&TelemetryOptions{
			Observer: benchmarkTelemetryObserver{},
		})
		if err != nil {
			b.Fatal(err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			b.Fatal(err)
		}
		ctx, err := WithOperationIdentity(request.Context(), "benchmark-operation")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPipeline(b, pipeline, request.WithContext(ctx), terminal)
	})

	b.Run("authenticated", func(b *testing.B) {
		editor, err := NewBearerAuth("benchmark-token")
		if err != nil {
			b.Fatal(err)
		}
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name: "benchmark-auth", Layer: MiddlewareClient,
		}, editor)
		if err != nil {
			b.Fatal(err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPipeline(b, pipeline, request, terminal)
	})

	b.Run("retried", func(b *testing.B) {
		clock := &benchmarkRetryClock{now: time.Unix(1_700_000_000, 0)}
		retry, err := NewRetryMiddleware(RetryOptions{
			Name: "benchmark-retry", MaximumAttempts: 2, Clock: clock,
			Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
		})
		if err != nil {
			b.Fatal(err)
		}
		pipeline, err := NewPipeline(retry)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			attempt := 0
			response, executeErr := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
				attempt++
				status := http.StatusNoContent
				if attempt == 1 {
					status = http.StatusServiceUnavailable
				}
				return benchmarkResponse(request, status, nil, nil), nil
			}))
			if executeErr != nil {
				b.Fatal(executeErr)
			}
			_ = response.Body.Close()
		}
	})
}

func BenchmarkRequestConstructionAndSerialization(b *testing.B) {
	base, err := NewRequestSpec(
		"https://api.example.test/v1/",
		"widgets?existing=preserved#section",
	)
	if err != nil {
		b.Fatal(err)
	}
	base, err = base.WithHeader(LayerClient, "Accept", "application/json")
	if err != nil {
		b.Fatal(err)
	}
	base, err = base.WithHeader(LayerEndpoint, "X-Vendor-Version", "2026-07-16")
	if err != nil {
		b.Fatal(err)
	}

	b.Run("request-build", func(b *testing.B) {
		body, bodyErr := NewBytesBody("application/json", []byte(`{"name":"widget"}`))
		if bodyErr != nil {
			b.Fatal(bodyErr)
		}
		spec, specErr := base.WithBody(body)
		if specErr != nil {
			b.Fatal(specErr)
		}
		spec, specErr = spec.WithQuery(LayerRequest, "include", RepeatedQuery("owner", "labels"))
		if specErr != nil {
			b.Fatal(specErr)
		}
		benchmarkRequestBuild(b, spec, http.MethodPost)
	})

	styles := []struct {
		name  string
		value QueryValue
	}{
		{name: "repeated", value: RepeatedQuery("alpha", "beta", "gamma")},
		{name: "comma", value: mustBenchmarkQueryValue(b, QueryCommaDelimited)},
		{name: "space", value: mustBenchmarkQueryValue(b, QuerySpaceDelimited)},
		{name: "pipe", value: mustBenchmarkQueryValue(b, QueryPipeDelimited)},
		{name: "deep-object", value: DeepObjectQuery(map[string]string{
			"name": "widget", "state": "active", "unicode": "hyvä",
		})},
	}
	custom, err := CustomQuery(QueryEncoderFunc(func(name string) ([]QueryPart, error) {
		return []QueryPart{
			{Name: name + "[kind]", Value: "widget", HasValue: true},
			{Name: name + "[enabled]", Value: "true", HasValue: true},
		}, nil
	}))
	if err != nil {
		b.Fatal(err)
	}
	styles = append(styles, struct {
		name  string
		value QueryValue
	}{name: "custom", value: custom})
	for _, style := range styles {
		b.Run("query-"+style.name, func(b *testing.B) {
			spec, specErr := base.WithQuery(LayerRequest, "filter", style.value)
			if specErr != nil {
				b.Fatal(specErr)
			}
			benchmarkRequestBuild(b, spec, http.MethodGet)
		})
	}

	b.Run("form", func(b *testing.B) {
		values := url.Values{
			"name":  {"widget"},
			"state": {"active", "queued"},
			"note":  {"reserved &= unicode hyvä"},
		}
		b.ReportAllocs()
		for range b.N {
			body, bodyErr := NewFormBody(values)
			if bodyErr != nil {
				b.Fatal(bodyErr)
			}
			reader, openErr := body.Open()
			if openErr != nil {
				b.Fatal(openErr)
			}
			_, _ = io.Copy(io.Discard, reader)
			_ = reader.Close()
		}
	})
}

func mustBenchmarkQueryValue(b *testing.B, style QueryStyle) QueryValue {
	b.Helper()
	value, err := QueryValues(style, "alpha", "beta", "gamma")
	if err != nil {
		b.Fatal(err)
	}
	return value
}

func benchmarkRequestBuild(b *testing.B, spec RequestSpec, method string) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		request, err := spec.Build(context.Background(), method)
		if err != nil {
			b.Fatal(err)
		}
		if request.URL.RawQuery == "" {
			b.Fatal("serialized request lost query")
		}
		if request.Body != nil {
			_ = request.Body.Close()
		}
	}
}

func benchmarkPipeline(b *testing.B, pipeline Pipeline, request *http.Request, terminal Next) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, err := pipeline.Execute(request, TransportFunc(terminal))
		if err != nil {
			b.Fatal(err)
		}
		_ = response.Body.Close()
	}
}

func BenchmarkPaginationThroughput(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		paginator, err := NewPaginator(PaginationOptions[int, int]{
			Fetch: func(_ context.Context, page int) (PaginationPage[int, int], error) {
				return PaginationPage[int, int]{
					Items: []int{page * 2, page*2 + 1}, Next: page + 1,
					HasNext: page < 9, ResponseBytes: 128,
				}, nil
			},
			Key: func(page int) (string, error) { return strconv.Itoa(page), nil },
		})
		if err != nil {
			b.Fatal(err)
		}
		for {
			_, ok, nextErr := paginator.Next(context.Background())
			if nextErr != nil {
				b.Fatal(nextErr)
			}
			if !ok {
				break
			}
		}
	}
}

func BenchmarkRequestPoolThroughput(b *testing.B) {
	pool, err := NewPool(PoolOptions[int, int]{
		Concurrency: 4,
		Execute: func(_ context.Context, input int) (PoolValue[int], error) {
			return PoolValue[int]{Value: input * 2, ResponseBytes: 64, MemoryBytes: 8}, nil
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	inputs := make([]int, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		results, runErr := pool.RunSlice(context.Background(), inputs)
		if runErr != nil || len(results) != len(inputs) {
			b.Fatalf("pool run = %d, %v", len(results), runErr)
		}
	}
}

func BenchmarkCachePolicyPaths(b *testing.B) {
	b.Run("hit", benchmarkCacheHit)
	b.Run("miss", benchmarkCacheMiss)
	b.Run("revalidation", benchmarkCacheRevalidation)
	b.Run("stale", benchmarkCacheStale)
	b.Run("concurrent-stampede", benchmarkCacheStampede)
}

func benchmarkCacheHit(b *testing.B) {
	pipeline, request := newBenchmarkCache(b, nil)
	terminal := benchmarkCacheTerminal("max-age=3600", http.StatusOK)
	primeBenchmarkCache(b, pipeline, request, terminal)
	benchmarkCachedRequests(b, pipeline, request, terminal, CacheHit)
}

func benchmarkCacheMiss(b *testing.B) {
	pipeline, request := newBenchmarkCache(b, nil)
	terminal := benchmarkCacheTerminal("max-age=3600", http.StatusOK)
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		current := request.Clone(request.Context())
		current.URL = cloneURL(request.URL)
		current.URL.RawQuery = "item=" + strconv.Itoa(index)
		response, err := pipeline.Execute(current, TransportFunc(terminal))
		if err != nil {
			b.Fatal(err)
		}
		if metadata, ok := CacheMetadataFromResponse(response); !ok || metadata.Provenance != CacheMiss {
			b.Fatalf("cache miss provenance = %#v, %t", metadata, ok)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
}

func benchmarkCacheRevalidation(b *testing.B) {
	pipeline, request := newBenchmarkCache(b, nil)
	prime := Next(func(request *http.Request) (*http.Response, error) {
		return benchmarkResponse(request, http.StatusOK, http.Header{
			"Cache-Control": {"max-age=0"}, "Etag": {`"v1"`},
		}, []byte("cached")), nil
	})
	primeBenchmarkCache(b, pipeline, request, prime)
	terminal := Next(func(request *http.Request) (*http.Response, error) {
		return benchmarkResponse(request, http.StatusNotModified, http.Header{
			"Cache-Control": {"max-age=0"},
		}, nil), nil
	})
	benchmarkCachedRequests(b, pipeline, request, terminal, CacheRevalidated)
}

func benchmarkCacheStale(b *testing.B) {
	pipeline, request := newBenchmarkCache(b, nil)
	prime := benchmarkCacheTerminal("max-age=0", http.StatusOK)
	primeBenchmarkCache(b, pipeline, request, prime)
	request = request.Clone(request.Context())
	request.Header.Set("Cache-Control", "max-stale=3600")
	terminal := Next(func(*http.Request) (*http.Response, error) {
		b.Fatal("stale cache benchmark reached origin")
		return nil, nil
	})
	benchmarkCachedRequests(b, pipeline, request, terminal, CacheStale)
}

func benchmarkCacheStampede(b *testing.B) {
	var originCalls atomic.Int64
	b.ReportAllocs()
	for range b.N {
		memory, err := NewMemoryCache(MemoryCacheOptions{})
		if err != nil {
			b.Fatal(err)
		}
		store := &benchmarkCacheLoadBarrier{CacheStore: memory, loaded: make(chan struct{})}
		store.remaining.Store(8)
		pipeline, request := newBenchmarkCacheWithStore(b, nil, store)
		terminal := Next(func(request *http.Request) (*http.Response, error) {
			originCalls.Add(1)
			<-store.loaded
			return benchmarkResponse(request, http.StatusOK, http.Header{
				"Cache-Control": {"max-age=3600"},
			}, []byte("cached")), nil
		})
		var wait sync.WaitGroup
		wait.Add(8)
		for range 8 {
			go func() {
				defer wait.Done()
				response, err := pipeline.Execute(request, TransportFunc(terminal))
				if err != nil {
					b.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}()
		}
		wait.Wait()
	}
	b.ReportMetric(float64(originCalls.Load())/float64(b.N), "origin_calls/op")
}

func newBenchmarkCache(b *testing.B, clock RetryClock) (Pipeline, *http.Request) {
	b.Helper()
	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		b.Fatal(err)
	}
	return newBenchmarkCacheWithStore(b, clock, store)
}

func newBenchmarkCacheWithStore(
	b *testing.B,
	clock RetryClock,
	store CacheStore,
) (Pipeline, *http.Request) {
	b.Helper()
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "benchmark-cache", Layer: MiddlewareClient, Store: store, Clock: clock,
	})
	if err != nil {
		b.Fatal(err)
	}
	pipeline, err := NewPipeline(middleware)
	if err != nil {
		b.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/widgets", nil)
	return pipeline, request
}

func benchmarkCacheTerminal(cacheControl string, status int) Next {
	return func(request *http.Request) (*http.Response, error) {
		return benchmarkResponse(request, status, http.Header{
			"Cache-Control": {cacheControl},
		}, []byte("cached")), nil
	}
}

func primeBenchmarkCache(b *testing.B, pipeline Pipeline, request *http.Request, terminal Next) {
	b.Helper()
	response, err := pipeline.Execute(request, TransportFunc(terminal))
	if err != nil {
		b.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

func benchmarkCachedRequests(
	b *testing.B,
	pipeline Pipeline,
	request *http.Request,
	terminal Next,
	want CacheProvenance,
) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, err := pipeline.Execute(request, TransportFunc(terminal))
		if err != nil {
			b.Fatal(err)
		}
		if metadata, ok := CacheMetadataFromResponse(response); !ok || metadata.Provenance != want {
			b.Fatalf("cache provenance = %#v, %t; want %d", metadata, ok, want)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
}

func BenchmarkLimiterCircuitComposition(b *testing.B) {
	limiter, err := NewTokenBucketLimiter(TokenBucketOptions{Rate: 1_000_000, Burst: 1_000_000})
	if err != nil {
		b.Fatal(err)
	}
	rate, err := NewRateLimitMiddleware(RateLimitOptions{Name: "benchmark-limit", Limiter: limiter})
	if err != nil {
		b.Fatal(err)
	}
	breaker, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "benchmark-breaker",
		Breaker: CircuitBreakerFunc(func(ctx context.Context, operation func(context.Context) (*http.Response, error)) (*http.Response, error) {
			return operation(ctx)
		}),
	})
	if err != nil {
		b.Fatal(err)
	}
	pipeline, err := NewPipeline(append(rate, breaker)...)
	if err != nil {
		b.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/widgets", nil)
	benchmarkPipeline(b, pipeline, request, func(request *http.Request) (*http.Response, error) {
		return benchmarkResponse(request, http.StatusNoContent, nil, nil), nil
	})
}

func BenchmarkBodyProcessingPaths(b *testing.B) {
	payload := bytes.Repeat([]byte("payload"), 512)

	b.Run("multipart", func(b *testing.B) {
		body, err := NewBytesBody("application/octet-stream", payload)
		if err != nil {
			b.Fatal(err)
		}
		multipartBody, err := NewMultipartBody(MultipartOptions{
			Boundary: "benchmark-boundary",
			Parts:    []MultipartPart{{Name: "file", FileName: "data.bin", Body: body}},
		})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			reader, openErr := multipartBody.Open()
			if openErr != nil {
				b.Fatal(openErr)
			}
			_, _ = io.Copy(io.Discard, reader)
			_ = reader.Close()
		}
	})

	b.Run("stream", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			response := benchmarkResponse(nil, http.StatusOK, nil, payload)
			if _, err := CopyResponse(context.Background(), response, io.Discard, TransferOptions{
				MaximumBytes: int64(len(payload)),
			}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("decode", func(b *testing.B) {
		content := []byte(`{"name":"widget","count":42}`)
		b.ReportAllocs()
		for range b.N {
			response := benchmarkResponse(nil, http.StatusOK, http.Header{
				"Content-Type": {"application/json"},
			}, content)
			if _, err := DecodeJSONResponse[struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			}](response, DecodeOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("decompress", func(b *testing.B) {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = writer.Write(payload)
		_ = writer.Close()
		middleware, err := NewCompressionMiddleware(CompressionOptions{Name: "benchmark-compression"})
		if err != nil {
			b.Fatal(err)
		}
		pipeline, err := NewPipeline(middleware)
		if err != nil {
			b.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/data", nil)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			response, executeErr := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
				return benchmarkResponse(request, http.StatusOK, http.Header{
					"Content-Encoding": {"gzip"},
				}, compressed.Bytes()), nil
			}))
			if executeErr != nil {
				b.Fatal(executeErr)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	})
}

func BenchmarkPolicyScopeResolution(b *testing.B) {
	scope, err := NewPolicyScope(PolicyScopeOptions{
		Endpoint: "widgets", Credential: "credential", Tenant: "tenant", Account: "account",
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx, err := WithPolicyScope(context.Background(), scope)
	if err != nil {
		b.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test/widgets", nil)
	b.ReportAllocs()
	for range b.N {
		if _, resolveErr := ResolvePolicyScope(request, PolicyResourceCache); resolveErr != nil {
			b.Fatal(resolveErr)
		}
	}
}

func BenchmarkFixturePaths(b *testing.B) {
	body := bytes.Repeat([]byte("fixture"), 128<<10)
	fixture := Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions: []FixtureInteraction{{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://api.example.test/data"},
			Response: FixtureResponse{StatusCode: http.StatusOK, Body: body},
		}},
	}

	b.Run("large-replay", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			replay, err := NewReplayTransport(fixture, ReplayOptions{MaximumBodyBytes: int64(len(body))})
			if err != nil {
				b.Fatal(err)
			}
			request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/data", nil)
			response, err := replay.RoundTrip(request)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	})

	b.Run("large-record", func(b *testing.B) {
		base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return benchmarkResponse(request, http.StatusOK, nil, body), nil
		})
		b.ReportAllocs()
		for range b.N {
			recorder, err := NewRecorderTransport(base, RecorderOptions{
				MaximumBodyBytes: int64(len(body)),
				ResponseBodyRedactor: FixtureBodyRedactorFunc(func(content []byte) ([]byte, error) {
					return content, nil
				}),
			})
			if err != nil {
				b.Fatal(err)
			}
			request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/data", nil)
			response, err := recorder.RoundTrip(request)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	})
}

func benchmarkResponse(
	request *http.Request,
	status int,
	header http.Header,
	body []byte,
) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)), Request: request,
	}
}
