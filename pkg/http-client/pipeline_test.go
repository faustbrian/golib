package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPipelineResolvesLayerOverridesAndDeterministicOrder(t *testing.T) {
	t.Parallel()

	clientHeaders := mustRequestMiddleware(t, MiddlewareOptions{
		Name:     "headers",
		Scope:    ScopeOperation,
		Layer:    MiddlewareClient,
		Priority: 100,
	}, passThroughMiddleware)
	endpointHeaders := mustRequestMiddleware(t, MiddlewareOptions{
		Name:     "headers",
		Scope:    ScopeOperation,
		Layer:    MiddlewareEndpoint,
		Priority: 50,
	}, passThroughMiddleware)
	requestA := mustRequestMiddleware(t, MiddlewareOptions{
		Name:     "a-request",
		Scope:    ScopeOperation,
		Layer:    MiddlewareRequest,
		Priority: 20,
	}, passThroughMiddleware)
	requestB := mustRequestMiddleware(t, MiddlewareOptions{
		Name:     "b-request",
		Scope:    ScopeOperation,
		Layer:    MiddlewareRequest,
		Priority: 20,
	}, passThroughMiddleware)
	attemptTransport := mustTransportMiddleware(t, MiddlewareOptions{
		Name:     "network-policy",
		Scope:    ScopeAttempt,
		Layer:    MiddlewareClient,
		Priority: -10,
	}, passThroughMiddleware)
	attemptResponse := mustResponseMiddleware(t, MiddlewareOptions{
		Name:     "observe-response",
		Scope:    ScopeAttempt,
		Layer:    MiddlewareClient,
		Priority: 0,
	}, func(_ *http.Request, response *http.Response) (*http.Response, error) {
		return response, nil
	})

	base, err := NewPipeline(clientHeaders, requestB, attemptResponse)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	resolved, err := base.With(endpointHeaders, requestA, attemptTransport)
	if err != nil {
		t.Fatalf("With() error = %v", err)
	}

	baseInspection := base.Inspect()
	if got := middlewareNames(baseInspection.Operation); !reflect.DeepEqual(got, []string{"b-request", "headers"}) {
		t.Fatalf("base operation middleware = %#v", got)
	}
	inspection := resolved.Inspect()
	if got := middlewareNames(inspection.Operation); !reflect.DeepEqual(got, []string{"a-request", "b-request", "headers"}) {
		t.Fatalf("operation middleware = %#v", got)
	}
	if got := middlewareNames(inspection.Attempt); !reflect.DeepEqual(got, []string{"network-policy", "observe-response"}) {
		t.Fatalf("attempt middleware = %#v", got)
	}
	if inspection.Operation[2].Layer != MiddlewareEndpoint || inspection.Operation[2].Priority != 50 {
		t.Fatalf("resolved headers = %#v, want endpoint override", inspection.Operation[2])
	}
	if inspection.Operation[0].Stage != StageRequest || inspection.Attempt[0].Stage != StageTransport {
		t.Fatalf("resolved stages = %#v %#v", inspection.Operation[0], inspection.Attempt[0])
	}

	inspection.Operation[2].Name = "mutated"
	if resolved.Inspect().Operation[2].Name != "headers" {
		t.Fatal("Inspect() aliases pipeline state")
	}
}

func TestPipelineRejectsInvalidMiddlewareDefinitions(t *testing.T) {
	t.Parallel()

	valid := MiddlewareOptions{
		Name:  "valid",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}

	tests := map[string]func() error{
		"empty name": func() error {
			_, err := NewRequestMiddleware(MiddlewareOptions{}, passThroughMiddleware)
			return err
		},
		"invalid name": func() error {
			options := valid
			options.Name = "not valid"
			_, err := NewRequestMiddleware(options, passThroughMiddleware)
			return err
		},
		"unknown scope": func() error {
			options := valid
			options.Scope = MiddlewareScope(255)
			_, err := NewRequestMiddleware(options, passThroughMiddleware)
			return err
		},
		"unknown layer": func() error {
			options := valid
			options.Layer = MiddlewareLayer(255)
			_, err := NewRequestMiddleware(options, passThroughMiddleware)
			return err
		},
		"nil request handler": func() error {
			_, err := NewRequestMiddleware(valid, nil)
			return err
		},
		"nil transport handler": func() error {
			_, err := NewTransportMiddleware(valid, nil)
			return err
		},
		"nil response handler": func() error {
			_, err := NewResponseMiddleware(valid, nil)
			return err
		},
		"nil error handler": func() error {
			_, err := NewErrorMiddleware(valid, nil)
			return err
		},
		"nil completion handler": func() error {
			_, err := NewCompletionMiddleware(valid, nil)
			return err
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := test(); !errors.Is(err, ErrInvalidMiddleware) {
				t.Fatalf("error = %v, want ErrInvalidMiddleware", err)
			}
		})
	}
}

func TestPipelineRejectsDuplicateMiddlewareAtSameLayer(t *testing.T) {
	t.Parallel()

	options := MiddlewareOptions{
		Name:  "duplicate",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}
	first := mustRequestMiddleware(t, options, passThroughMiddleware)
	second := mustRequestMiddleware(t, options, passThroughMiddleware)

	_, err := NewPipeline(first, second)
	if !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("NewPipeline() error = %v, want ErrInvalidMiddleware", err)
	}
}

func TestPipelineExecutesNestedLifecycleInDeterministicOrder(t *testing.T) {
	t.Parallel()

	var events []string
	operationRequest := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "operation-request",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, recordingAround(&events, "operation-request"))
	operationTransport := mustTransportMiddleware(t, MiddlewareOptions{
		Name:  "operation-transport",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, recordingAround(&events, "operation-transport"))
	attemptRequest := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "attempt-request",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, recordingAround(&events, "attempt-request"))
	attemptTransport := mustTransportMiddleware(t, MiddlewareOptions{
		Name:  "attempt-transport",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, recordingAround(&events, "attempt-transport"))
	attemptResponse := mustResponseMiddleware(t, MiddlewareOptions{
		Name:  "attempt-response",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, response *http.Response) (*http.Response, error) {
		events = append(events, "attempt-response")
		response.Header.Set("X-Attempt", "observed")

		return response, nil
	})
	attemptCompletion := mustCompletionMiddleware(t, MiddlewareOptions{
		Name:  "attempt-completion",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, _ *http.Response, _ error) error {
		events = append(events, "attempt-completion")

		return nil
	})
	operationResponse := mustResponseMiddleware(t, MiddlewareOptions{
		Name:  "operation-response",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, response *http.Response) (*http.Response, error) {
		events = append(events, "operation-response")
		if response.Header.Get("X-Attempt") != "observed" {
			t.Fatal("operation response did not receive attempt response")
		}

		return response, nil
	})
	operationCompletion := mustCompletionMiddleware(t, MiddlewareOptions{
		Name:  "operation-completion",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, _ *http.Response, _ error) error {
		events = append(events, "operation-completion")

		return nil
	})

	pipeline, err := NewPipeline(
		operationRequest,
		operationTransport,
		attemptRequest,
		attemptTransport,
		attemptResponse,
		attemptCompletion,
		operationResponse,
		operationCompletion,
	)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		events = append(events, "network")

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d", response.StatusCode)
	}

	want := []string{
		"operation-request:before",
		"operation-transport:before",
		"attempt-request:before",
		"attempt-transport:before",
		"network",
		"attempt-transport:after",
		"attempt-request:after",
		"attempt-response",
		"attempt-completion",
		"operation-transport:after",
		"operation-request:after",
		"operation-response",
		"operation-completion",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestPipelineRunsAttemptScopeForEveryAttempt(t *testing.T) {
	t.Parallel()

	operationCalls := 0
	attemptCalls := 0
	networkCalls := 0
	operation := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "operation",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		operationCalls++

		return next(request)
	})
	retry := mustTransportMiddleware(t, MiddlewareOptions{
		Name:  "two-attempts",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		first, err := next(request)
		if err != nil {
			return nil, err
		}
		if err := first.Body.Close(); err != nil {
			return nil, err
		}

		return next(request)
	})
	attempt := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "attempt",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		attemptCalls++

		return next(request)
	})

	pipeline, err := NewPipeline(operation, retry, attempt)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		networkCalls++

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("Body.Close() error = %v", err)
		}
	}()

	if operationCalls != 1 || attemptCalls != 2 || networkCalls != 2 {
		t.Fatalf(
			"calls = operation:%d attempt:%d network:%d, want 1/2/2",
			operationCalls,
			attemptCalls,
			networkCalls,
		)
	}
}

func TestPipelineShortCircuitSkipsAttemptAndNetwork(t *testing.T) {
	t.Parallel()

	attemptCalls := 0
	networkCalls := 0
	completionCalls := 0
	shortCircuit := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "fixture",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(*http.Request, Next) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusAccepted}, nil
	})
	attempt := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "attempt",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		attemptCalls++

		return next(request)
	})
	completion := mustCompletionMiddleware(t, MiddlewareOptions{
		Name:  "completion",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, response *http.Response, failure error) error {
		completionCalls++
		if response == nil || response.StatusCode != http.StatusAccepted || failure != nil {
			t.Fatalf("completion result = %#v, %v", response, failure)
		}

		return nil
	})
	pipeline, err := NewPipeline(shortCircuit, attempt, completion)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		networkCalls++

		return nil, errors.New("unexpected network call")
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if response.Header == nil || response.Body == nil || response.Request == nil {
		t.Fatalf("short response was not normalized: %#v", response)
	}
	if attemptCalls != 0 || networkCalls != 0 || completionCalls != 1 {
		t.Fatalf("calls = attempt:%d network:%d completion:%d", attemptCalls, networkCalls, completionCalls)
	}
}

func TestPipelineResponseReplacementClosesSupersededBody(t *testing.T) {
	t.Parallel()

	originalBody := &trackingBody{closed: make(chan struct{})}
	replacementBody := &trackingBody{closed: make(chan struct{})}
	replace := mustResponseMiddleware(t, MiddlewareOptions{
		Name:  "replace",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, _ *http.Response) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       replacementBody,
		}, nil
	})
	pipeline, err := NewPipeline(replace)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       originalBody,
			Request:    request,
		}, nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated || response.Body != replacementBody {
		t.Fatalf("response = %#v", response)
	}
	select {
	case <-originalBody.closed:
	default:
		t.Fatal("superseded response body was not closed")
	}
}

func TestPipelineAfterStagesReceiveIndependentStableRequestSnapshots(t *testing.T) {
	t.Parallel()

	body := io.NopCloser(strings.NewReader("secret payload"))
	mutate := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "mutate",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		request.Header.Set("X-Stable", "yes")

		return next(request)
	})
	firstResponse := mustResponseMiddleware(t, MiddlewareOptions{
		Name:     "first",
		Scope:    ScopeOperation,
		Layer:    MiddlewareClient,
		Priority: 0,
	}, func(request *http.Request, response *http.Response) (*http.Response, error) {
		if request.Header.Get("X-Stable") != "yes" {
			t.Fatalf("first snapshot X-Stable = %q", request.Header.Get("X-Stable"))
		}
		if request.Body == body {
			t.Fatal("snapshot aliases request body")
		}
		request.Header.Set("X-Stable", "mutated")
		request.URL.Path = "/mutated"

		return response, nil
	})
	secondResponse := mustResponseMiddleware(t, MiddlewareOptions{
		Name:     "second",
		Scope:    ScopeOperation,
		Layer:    MiddlewareClient,
		Priority: 1,
	}, func(request *http.Request, response *http.Response) (*http.Response, error) {
		if request.Header.Get("X-Stable") != "yes" || request.URL.Path != "/original" {
			t.Fatalf("second snapshot was mutated: %q %q", request.Header.Get("X-Stable"), request.URL.Path)
		}

		return response, nil
	})
	pipeline, err := NewPipeline(mutate, firstResponse, secondResponse)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/original", body)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	request.Header = make(http.Header)

	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if request.Header.Get("X-Stable") != "" {
		t.Fatalf("caller request was mutated: %#v", request.Header)
	}
}

func TestPipelineErrorMiddlewareCanRecoverResponseFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("decode policy failed")
	failedBody := &trackingBody{closed: make(chan struct{})}
	responseFailure := mustResponseMiddleware(t, MiddlewareOptions{
		Name:  "response-failure",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, response *http.Response) (*http.Response, error) {
		return response, wantErr
	})
	recoverFailure := mustErrorMiddleware(t, MiddlewareOptions{
		Name:  "recover",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, failure error) (*http.Response, error) {
		if !errors.Is(failure, wantErr) {
			t.Fatalf("error middleware failure = %v", failure)
		}

		return &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
	})
	pipeline, err := NewPipeline(responseFailure, recoverFailure)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failedBody, Request: request}, nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d", response.StatusCode)
	}
	select {
	case <-failedBody.closed:
	default:
		t.Fatal("failed response body was not closed before recovery")
	}
}

func TestPipelineContainsMiddlewareAndTransportPanics(t *testing.T) {
	t.Parallel()

	secret := "secret panic value"
	panicking := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "panic",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(*http.Request, Next) (*http.Response, error) {
		panic(secret)
	})
	pipeline, err := NewPipeline(panicking)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unreachable")
	}))
	var panicErr *MiddlewarePanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("Execute() error = %T %v, want *MiddlewarePanicError", err, err)
	}
	if panicErr.Middleware.Name != "panic" || panicErr.Value != secret {
		t.Fatalf("MiddlewarePanicError = %#v", panicErr)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("panic error %q contains panic value", err)
	}

	empty, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	_, err = empty.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		panic(secret)
	}))
	if !errors.As(err, &panicErr) || panicErr.Middleware.Name != "net-http-transport" {
		t.Fatalf("transport panic error = %T %v", err, err)
	}
}

func TestPipelinePropagatesCancellationAndStillCompletesOperation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	middlewareCalls := 0
	completionCalls := 0
	requestMiddleware := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "request",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		middlewareCalls++

		return next(request)
	})
	completion := mustCompletionMiddleware(t, MiddlewareOptions{
		Name:  "completion",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, _ *http.Response, failure error) error {
		completionCalls++
		if !errors.Is(failure, context.Canceled) {
			t.Fatalf("completion failure = %v", failure)
		}

		return nil
	})
	pipeline, err := NewPipeline(requestMiddleware, completion)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unreachable")
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
	if middlewareCalls != 0 || completionCalls != 1 {
		t.Fatalf("calls = middleware:%d completion:%d", middlewareCalls, completionCalls)
	}
}

func TestPipelineRejectsInvalidExecutionResults(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("secret transport detail")
	pipeline, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	tests := map[string]struct {
		request   *http.Request
		transport http.RoundTripper
	}{
		"nil request": {
			transport: TransportFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		},
		"nil transport": {
			request: request,
		},
		"nil result": {
			request: request,
			transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				return nil, nil
			}),
		},
		"response and error": {
			request: request,
			transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, secretCause
			}),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response, err := pipeline.Execute(test.request, test.transport)
			if !errors.Is(err, ErrInvalidMiddlewareResult) {
				t.Fatalf("Execute() error = %v, want ErrInvalidMiddlewareResult", err)
			}
			if response != nil {
				t.Fatalf("Execute() response = %#v, want nil", response)
			}
			if name == "response and error" {
				if !errors.Is(err, secretCause) {
					t.Fatalf("Execute() error = %v, want preserved cause", err)
				}
				if strings.Contains(err.Error(), secretCause.Error()) {
					t.Fatalf("Execute() error %q contains cause", err)
				}
			}
		})
	}
}

func TestClientRunsOperationOnceAndAttemptMiddlewareForRedirects(t *testing.T) {
	t.Parallel()

	operationCalls := 0
	attemptCalls := 0
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serverCalls++
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)

			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	operation := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "operation",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		operationCalls++

		return next(request)
	})
	attempt := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "attempt",
		Scope: ScopeAttempt,
		Layer: MiddlewareClient,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		attemptCalls++

		return next(request)
	})
	client, err := New(Config{Middleware: []Middleware{operation, attempt}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/start", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
	if operationCalls != 1 || attemptCalls != 2 || serverCalls != 2 {
		t.Fatalf(
			"calls = operation:%d attempt:%d server:%d, want 1/2/2",
			operationCalls,
			attemptCalls,
			serverCalls,
		)
	}
	inspection := client.InspectPipeline()
	if len(inspection.Operation) != 2 ||
		inspection.Operation[0].Name != "httpclient.operation-identity" ||
		inspection.Operation[1].Name != "operation" ||
		len(inspection.Attempt) != 1 {
		t.Fatalf("InspectPipeline() = %#v", inspection)
	}
}

func TestClientOperationMiddlewareCanShortCircuitNetwork(t *testing.T) {
	t.Parallel()

	shortCircuit := mustTransportMiddleware(t, MiddlewareOptions{
		Name:  "cache-hit",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(*http.Request, Next) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	client, err := New(Config{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network must not run")
		}),
		Middleware: []Middleware{shortCircuit},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}

func TestClientDoWithMiddlewareDoesNotMutateBasePipeline(t *testing.T) {
	t.Parallel()

	oneShotCalls := 0
	oneShot := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "one-shot",
		Scope: ScopeOperation,
		Layer: MiddlewareOneShot,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		oneShotCalls++

		return next(request)
	})
	client, err := New(Config{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	newRequest := func() *http.Request {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
		if requestErr != nil {
			t.Fatalf("NewRequestWithContext() error = %v", requestErr)
		}

		return request
	}
	response, err := client.DoWithMiddleware(newRequest(), oneShot)
	if err != nil {
		t.Fatalf("DoWithMiddleware() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("first Body.Close() error = %v", err)
	}
	response, err = client.Do(newRequest())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("second Body.Close() error = %v", err)
	}
	if oneShotCalls != 1 {
		t.Fatalf("one-shot calls = %d, want 1", oneShotCalls)
	}
	inspection := client.InspectPipeline()
	if len(inspection.Operation) != 1 || inspection.Operation[0].Name != "httpclient.operation-identity" {
		t.Fatal("DoWithMiddleware mutated base pipeline")
	}
}

func TestNewClientRejectsInvalidMiddlewarePipeline(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Middleware: []Middleware{{}}})
	if !errors.Is(err, ErrInvalidConfig) || !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig and ErrInvalidMiddleware", err)
	}
}

func TestClientDoWithMiddlewareRejectsInvalidDerivedPipeline(t *testing.T) {
	t.Parallel()

	base := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "duplicate",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, passThroughMiddleware)
	client, err := New(Config{Middleware: []Middleware{base}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = client.DoWithMiddleware(request, base)
	if !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("DoWithMiddleware() error = %v, want ErrInvalidMiddleware", err)
	}
}

func TestClientUsesDefaultTransportWhenStandardTransportIsNil(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.HTTPClient().Transport = nil
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}

func TestPipelineRejectsNilRequestsPassedByMiddleware(t *testing.T) {
	t.Parallel()

	passNil := mustRequestMiddleware(t, MiddlewareOptions{
		Name:  "nil-request",
		Scope: ScopeOperation,
		Layer: MiddlewareClient,
	}, func(_ *http.Request, next Next) (*http.Response, error) {
		return next(nil)
	})
	pipeline, err := NewPipeline(passNil)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unreachable")
	}))
	if !errors.Is(err, ErrInvalidMiddlewareResult) {
		t.Fatalf("Execute() error = %v, want ErrInvalidMiddlewareResult", err)
	}

	_, err = executeMiddlewareScope(
		[]Middleware{passNil},
		ScopeOperation,
		nil,
		func(*http.Request) (*http.Response, error) { return nil, errors.New("unreachable") },
	)
	if !errors.Is(err, ErrInvalidMiddlewareResult) {
		t.Fatalf("executeMiddlewareScope(nil) error = %v", err)
	}
}

func TestPipelineWithoutAroundMiddlewareRejectsCanceledRequest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	pipeline, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unreachable")
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
}

func TestPipelineRejectsInvalidAfterStageResults(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("middleware failure")
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	terminal := TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	})

	tests := map[string]Middleware{
		"nil response": mustResponseMiddleware(t, MiddlewareOptions{
			Name: "nil-response", Scope: ScopeOperation, Layer: MiddlewareClient,
		}, func(*http.Request, *http.Response) (*http.Response, error) {
			return nil, nil
		}),
		"swallowed error": mustErrorMiddleware(t, MiddlewareOptions{
			Name: "swallow", Scope: ScopeOperation, Layer: MiddlewareClient,
		}, func(*http.Request, error) (*http.Response, error) {
			return nil, nil
		}),
	}

	for name, middleware := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pipeline, pipelineErr := NewPipeline(middleware)
			if pipelineErr != nil {
				t.Fatalf("NewPipeline() error = %v", pipelineErr)
			}
			selectedTerminal := http.RoundTripper(terminal)
			if name == "swallowed error" {
				selectedTerminal = TransportFunc(func(*http.Request) (*http.Response, error) {
					return nil, wantErr
				})
			}
			_, executeErr := pipeline.Execute(request, selectedTerminal)
			if !errors.Is(executeErr, ErrInvalidMiddlewareResult) {
				t.Fatalf("Execute() error = %v, want ErrInvalidMiddlewareResult", executeErr)
			}
		})
	}
}

func TestPipelineClosesResponsesReturnedWithErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("middleware failure")
	body := &trackingBody{closed: make(chan struct{})}
	propagate := mustErrorMiddleware(t, MiddlewareOptions{
		Name: "propagate", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, error) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body}, wantErr
	})
	pipeline, err := NewPipeline(propagate)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failure")
	}))
	var executionErr *MiddlewareExecutionError
	if !errors.As(err, &executionErr) || !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %T %v", err, err)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("error response body was not closed")
	}
	if strings.Contains(executionErr.Error(), wantErr.Error()) {
		t.Fatalf("MiddlewareExecutionError %q contains cause", executionErr)
	}
}

func TestPipelineCompletionFailureClosesSuccessfulResponse(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("completion failure")
	body := &trackingBody{closed: make(chan struct{})}
	completion := mustCompletionMiddleware(t, MiddlewareOptions{
		Name: "completion-failure", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, *http.Response, error) error {
		return wantErr
	})
	pipeline, err := NewPipeline(completion)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Request: request}, nil
	}))
	if response != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Execute() = %#v, %v", response, err)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("completion failure did not close response body")
	}
}

func TestPipelineContainsPanicsInEveryAfterStage(t *testing.T) {
	t.Parallel()

	secret := "secret after-stage panic"
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	responsePanic := mustResponseMiddleware(t, MiddlewareOptions{
		Name: "response-panic", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, *http.Response) (*http.Response, error) {
		panic(secret)
	})
	errorPanic := mustErrorMiddleware(t, MiddlewareOptions{
		Name: "error-panic", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, error) (*http.Response, error) {
		panic(secret)
	})
	completionPanic := mustCompletionMiddleware(t, MiddlewareOptions{
		Name: "completion-panic", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, *http.Response, error) error {
		panic(secret)
	})

	tests := map[string]struct {
		middleware Middleware
		transport  http.RoundTripper
	}{
		"response": {
			middleware: responsePanic,
			transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			}),
		},
		"error": {
			middleware: errorPanic,
			transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport failure")
			}),
		},
		"completion": {
			middleware: completionPanic,
			transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			}),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pipeline, pipelineErr := NewPipeline(test.middleware)
			if pipelineErr != nil {
				t.Fatalf("NewPipeline() error = %v", pipelineErr)
			}
			_, executeErr := pipeline.Execute(request, test.transport)
			var panicErr *MiddlewarePanicError
			if !errors.As(executeErr, &panicErr) || panicErr.Value != secret {
				t.Fatalf("Execute() error = %T %v", executeErr, executeErr)
			}
			if strings.Contains(executeErr.Error(), secret) {
				t.Fatalf("panic error %q contains panic value", executeErr)
			}
		})
	}
}

func TestPipelineResponseReplacementCloseFailureClosesReplacement(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("close failed")
	replacementBody := &trackingBody{closed: make(chan struct{})}
	replace := mustResponseMiddleware(t, MiddlewareOptions{
		Name: "replace", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, *http.Response) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Body: replacementBody}, nil
	})
	pipeline, err := NewPipeline(replace)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       closeErrorBody{err: wantErr},
			Request:    request,
		}, nil
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
	select {
	case <-replacementBody.closed:
	default:
		t.Fatal("replacement body was not closed after superseded close failure")
	}
}

func TestMiddlewareConstructorAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	invalid := MiddlewareOptions{Name: "", Scope: ScopeOperation, Layer: MiddlewareClient}
	if _, err := NewResponseMiddleware(invalid, func(*http.Request, *http.Response) (*http.Response, error) {
		return nil, nil
	}); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("NewResponseMiddleware() error = %v", err)
	}
	if _, err := NewErrorMiddleware(invalid, func(*http.Request, error) (*http.Response, error) {
		return nil, nil
	}); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("NewErrorMiddleware() error = %v", err)
	}
	if _, err := NewCompletionMiddleware(invalid, func(*http.Request, *http.Response, error) error {
		return nil
	}); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("NewCompletionMiddleware() error = %v", err)
	}
	if _, err := newMiddleware(
		MiddlewareOptions{Name: "valid", Scope: ScopeOperation, Layer: MiddlewareClient},
		MiddlewareStage(255),
	); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("newMiddleware(invalid stage) error = %v", err)
	}

	validInfo := MiddlewareInfo{Name: "valid", Scope: ScopeOperation, Layer: MiddlewareClient}
	invalidHandlers := []Middleware{
		{information: withStage(validInfo, StageRequest)},
		{information: withStage(validInfo, StageResponse)},
		{information: withStage(validInfo, StageError)},
		{information: withStage(validInfo, StageCompletion)},
	}
	for index, middleware := range invalidHandlers {
		if _, err := NewPipeline(middleware); !errors.Is(err, ErrInvalidMiddleware) {
			t.Fatalf("NewPipeline(invalid handler %d) error = %v", index, err)
		}
	}

	wantStages := map[MiddlewareStage]string{
		StageRequest:         "request",
		StageTransport:       "transport",
		StageResponse:        "response",
		StageError:           "error",
		StageCompletion:      "completion",
		MiddlewareStage(255): "stage(255)",
	}
	for stage, want := range wantStages {
		if stage.String() != want {
			t.Fatalf("%d.String() = %q, want %q", stage, stage.String(), want)
		}
	}
	if closeResponse(nil) != nil || closeResponse(&http.Response{}) != nil {
		t.Fatal("closeResponse empty response returned an error")
	}
	if snapshotRequest(nil) != nil {
		t.Fatal("snapshotRequest(nil) is non-nil")
	}
}

func withStage(information MiddlewareInfo, stage MiddlewareStage) MiddlewareInfo {
	information.Stage = stage

	return information
}

func middlewareNames(information []MiddlewareInfo) []string {
	names := make([]string, len(information))
	for index, middleware := range information {
		names[index] = middleware.Name
	}

	return names
}

func passThroughMiddleware(request *http.Request, next Next) (*http.Response, error) {
	return next(request)
}

func recordingAround(events *[]string, name string) AroundMiddlewareFunc {
	return func(request *http.Request, next Next) (*http.Response, error) {
		*events = append(*events, name+":before")
		response, err := next(request)
		*events = append(*events, name+":after")

		return response, err
	}
}

func mustRequestMiddleware(t *testing.T, options MiddlewareOptions, handler AroundMiddlewareFunc) Middleware {
	t.Helper()

	middleware, err := NewRequestMiddleware(options, handler)
	if err != nil {
		t.Fatalf("NewRequestMiddleware() error = %v", err)
	}

	return middleware
}

func mustTransportMiddleware(t *testing.T, options MiddlewareOptions, handler AroundMiddlewareFunc) Middleware {
	t.Helper()

	middleware, err := NewTransportMiddleware(options, handler)
	if err != nil {
		t.Fatalf("NewTransportMiddleware() error = %v", err)
	}

	return middleware
}

func mustResponseMiddleware(t *testing.T, options MiddlewareOptions, handler ResponseMiddlewareFunc) Middleware {
	t.Helper()

	middleware, err := NewResponseMiddleware(options, handler)
	if err != nil {
		t.Fatalf("NewResponseMiddleware() error = %v", err)
	}

	return middleware
}

func mustCompletionMiddleware(t *testing.T, options MiddlewareOptions, handler CompletionMiddlewareFunc) Middleware {
	t.Helper()

	middleware, err := NewCompletionMiddleware(options, handler)
	if err != nil {
		t.Fatalf("NewCompletionMiddleware() error = %v", err)
	}

	return middleware
}

func mustErrorMiddleware(t *testing.T, options MiddlewareOptions, handler ErrorMiddlewareFunc) Middleware {
	t.Helper()

	middleware, err := NewErrorMiddleware(options, handler)
	if err != nil {
		t.Fatalf("NewErrorMiddleware() error = %v", err)
	}

	return middleware
}
