package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIdempotencyKeyIsStableAcrossRetriesAndDistinctAcrossOperations(t *testing.T) {
	t.Parallel()

	var generated atomic.Int64
	idempotency, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name:  "create-widget-idempotency",
		Layer: MiddlewareEndpoint,
		Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{
				Value:       fmt.Sprintf("key-%d", generated.Add(1)),
				EntropyBits: 128,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct idempotency middleware: %v", err)
	}
	retry := mustTransportMiddleware(t, MiddlewareOptions{
		Name: "test-retry", Scope: ScopeOperation, Layer: MiddlewareClient,
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
	var mutex sync.Mutex
	var keys []IdempotencyKey
	var operationIDs []string
	transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
		key, ok := IdempotencyKeyFromContext(request.Context())
		if !ok {
			t.Fatal("attempt has no idempotency context")
		}
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok {
			t.Fatal("attempt has no operation identity")
		}
		if request.Header.Get("Idempotency-Key") != key.Value {
			t.Fatalf("header key = %q, context key = %#v", request.Header.Get("Idempotency-Key"), key)
		}
		mutex.Lock()
		keys = append(keys, key)
		operationIDs = append(operationIDs, identity.ID)
		mutex.Unlock()

		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	})
	client, err := New(Config{
		Transport:  transport,
		Middleware: append([]Middleware{retry}, idempotency...),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()

	for range 2 {
		request, err := http.NewRequest(http.MethodPost, "https://api.example.test/widgets", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
		if request.Header.Get("Idempotency-Key") != "" {
			t.Fatal("client mutated caller request header")
		}
	}

	mutex.Lock()
	defer mutex.Unlock()
	if len(keys) != 4 || keys[0] != keys[1] || keys[2] != keys[3] || keys[0] == keys[2] {
		t.Fatalf("idempotency keys = %#v", keys)
	}
	if keys[0].Value != "key-1" || keys[2].Value != "key-2" ||
		keys[0].Provenance != IdempotencyGenerated || keys[2].Provenance != IdempotencyGenerated {
		t.Fatalf("idempotency key metadata = %#v", keys)
	}
	if operationIDs[0] != operationIDs[1] || operationIDs[2] != operationIDs[3] || operationIDs[0] == operationIDs[2] {
		t.Fatalf("operation identities = %#v", operationIDs)
	}
}

func TestIdempotencyRedirectPolicyPreservesOnlyMatchingOperationIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		location      string
		attemptPolicy IdempotencyAttemptPolicy
		wantMethod    string
		wantKey       string
	}{
		{
			name: "same origin method-preserving redirect", status: http.StatusTemporaryRedirect,
			location: "/finish", wantMethod: http.MethodPost, wantKey: "caller-key",
		},
		{
			name: "method-changing redirect", status: http.StatusFound,
			location: "/finish", wantMethod: http.MethodGet,
		},
		{
			name: "cross-origin redirect", status: http.StatusTemporaryRedirect,
			location: "https://uploads.example.test/finish", wantMethod: http.MethodPost,
		},
		{
			name: "explicit cross-origin preservation", status: http.StatusTemporaryRedirect,
			location: "https://uploads.example.test/finish", wantMethod: http.MethodPost, wantKey: "caller-key",
			attemptPolicy: IdempotencyAttemptPolicyFunc(func(*http.Request, *http.Request) bool {
				return true
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			idempotency, err := NewIdempotencyMiddleware(IdempotencyOptions{
				Name: "create-widget-idempotency", Layer: MiddlewareEndpoint,
				Mode: IdempotencyRequireCaller, AttemptPolicy: test.attemptPolicy,
			})
			if err != nil {
				t.Fatalf("construct idempotency middleware: %v", err)
			}
			attempts := 0
			transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					key, ok := IdempotencyKeyFromContext(request.Context())
					if !ok || key.Provenance != IdempotencyCallerHeader || key.Value != "caller-key" {
						t.Fatalf("caller header provenance = %#v, %v", key, ok)
					}
					if got := request.Header.Get("Idempotency-Key"); got != "caller-key" {
						t.Fatalf("initial key = %q", got)
					}

					return &http.Response{
						StatusCode: test.status,
						Header:     http.Header{"Location": {test.location}},
						Body:       http.NoBody,
					}, nil
				}
				if request.Method != test.wantMethod {
					t.Fatalf("redirect method = %q, want %q", request.Method, test.wantMethod)
				}
				if got := request.Header.Get("Idempotency-Key"); got != test.wantKey {
					t.Fatalf("redirect key = %q, want %q", got, test.wantKey)
				}

				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
			})
			client, err := New(Config{Transport: transport, Middleware: idempotency})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close client: %v", err)
				}
			}()
			request, err := http.NewRequest(http.MethodPost, "https://api.example.test/start", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			request.Header.Set("Idempotency-Key", "caller-key")
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("execute request: %v", err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("close response: %v", err)
			}
			if attempts != 2 {
				t.Fatalf("attempts = %d, want 2", attempts)
			}
		})
	}
}

func TestCallerContextIdempotencyKeyHasExplicitProvenance(t *testing.T) {
	t.Parallel()

	ctx, err := WithIdempotencyKey(context.Background(), "context-key")
	if err != nil {
		t.Fatalf("attach idempotency key: %v", err)
	}
	idempotency, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name: "create-widget-idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller,
	})
	if err != nil {
		t.Fatalf("construct idempotency middleware: %v", err)
	}
	client, err := New(Config{
		Middleware: idempotency,
		Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
			key, ok := IdempotencyKeyFromContext(request.Context())
			if !ok || key.Value != "context-key" || key.Provenance != IdempotencyCallerContext {
				t.Fatalf("idempotency context = %#v, %v", key, ok)
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.test/widgets", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
}

func TestIdempotencyPrecedesAuthenticationByDefault(t *testing.T) {
	t.Parallel()

	idempotency, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name: "create-widget-idempotency", Layer: MiddlewareEndpoint,
		Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "generated-key", EntropyBits: 128}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct idempotency middleware: %v", err)
	}
	authentication, err := NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "vendor-auth", Layer: MiddlewareClient,
	}, RequestEditorFunc(func(request *http.Request) error {
		if got := request.Header.Get("Idempotency-Key"); got != "generated-key" {
			t.Fatalf("authentication observed idempotency key %q", got)
		}
		request.Header.Set("Authorization", "Bearer token")

		return nil
	}))
	if err != nil {
		t.Fatalf("construct authentication middleware: %v", err)
	}
	client, err := New(Config{
		Middleware: append(authentication, idempotency...),
		Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Authorization") != "Bearer token" {
				t.Fatal("transport did not receive authentication")
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	request, err := http.NewRequest(http.MethodPost, "https://api.example.test/widgets", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
}

func TestDefaultIdempotencyGeneratorUsesSecureIndependentKeysAndCustomHeader(t *testing.T) {
	t.Parallel()

	middleware, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name: "vendor-idempotency", Layer: MiddlewareEndpoint, Header: "X-Vendor-Idempotency",
	})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	var keys []string
	client, err := New(Config{
		Middleware: middleware,
		Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
			key, ok := IdempotencyKeyFromContext(request.Context())
			if !ok || key.Provenance != IdempotencyGenerated || len(key.Value) != 22 {
				t.Fatalf("generated key = %#v, %v", key, ok)
			}
			if request.Header.Get("X-Vendor-Idempotency") != key.Value || request.Header.Get("Idempotency-Key") != "" {
				t.Fatalf("custom idempotency headers = %#v", request.Header)
			}
			keys = append(keys, key.Value)

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	for range 2 {
		request, err := http.NewRequest(http.MethodPost, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("generated keys = %v", keys)
	}
}

func TestIdempotencyRejectsInvalidPolicyAndCallerKeys(t *testing.T) {
	t.Parallel()

	var typedNilGenerator *identityTestGenerator
	var typedNilAttempt *idempotencyTestAttemptPolicy
	validGenerator := IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
		return GeneratedIdentifier{Value: "generated", EntropyBits: 128}, nil
	})
	tests := []IdempotencyOptions{
		{Name: "Invalid", Layer: MiddlewareEndpoint},
		{Name: "valid", Layer: MiddlewareLayer(99)},
		{Name: "valid", Layer: MiddlewareEndpoint, Mode: IdempotencyMode(99)},
		{Name: "valid", Layer: MiddlewareEndpoint, Header: "Bad Header"},
		{Name: "valid", Layer: MiddlewareEndpoint, MaximumLength: -1},
		{Name: "valid", Layer: MiddlewareEndpoint, MaximumLength: maximumIdempotencyKeyLength + 1},
		{Name: "valid", Layer: MiddlewareEndpoint, MinimumEntropyBits: 95},
		{Name: "valid", Layer: MiddlewareEndpoint, MinimumEntropyBits: 513},
		{Name: "valid", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller, Generator: validGenerator},
		{Name: "valid", Layer: MiddlewareEndpoint, Generator: typedNilGenerator},
		{Name: "valid", Layer: MiddlewareEndpoint, AttemptPolicy: typedNilAttempt},
	}
	for _, options := range tests {
		if _, err := NewIdempotencyMiddleware(options); err == nil {
			t.Fatalf("invalid policy accepted: %#v", options)
		}
	}

	var nilContext context.Context
	for _, test := range []struct {
		ctx context.Context
		key string
	}{
		{ctx: nilContext, key: "valid"},
		{ctx: context.Background(), key: ""},
		{ctx: context.Background(), key: "contains space"},
		{ctx: context.Background(), key: "nön-ascii"},
		{ctx: context.Background(), key: strings.Repeat("a", maximumIdempotencyKeyLength+1)},
	} {
		if _, err := WithIdempotencyKey(test.ctx, test.key); !errors.Is(err, ErrInvalidIdempotencyKey) {
			t.Fatalf("caller key %q error = %v", test.key, err)
		}
	}
	if _, ok := IdempotencyKeyFromContext(nilContext); ok {
		t.Fatal("nil context returned idempotency key")
	}
}

func TestIdempotencyFailuresAreTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("idempotency generator do-not-render")
	tests := []struct {
		name       string
		options    IdempotencyOptions
		prepare    func(*testing.T, *http.Request) *http.Request
		wantCause  error
		credential string
	}{
		{
			name: "required key missing",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller,
			},
			wantCause: ErrIdempotencyKeyRequired,
		},
		{
			name: "duplicate header",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller,
			},
			prepare: func(t *testing.T, request *http.Request) *http.Request {
				t.Helper()
				request.Header.Add("Idempotency-Key", "first")
				request.Header.Add("Idempotency-Key", "second")

				return request
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
		{
			name: "context and header conflict",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller,
			},
			prepare: func(t *testing.T, request *http.Request) *http.Request {
				t.Helper()
				ctx, err := WithIdempotencyKey(request.Context(), "context-key")
				if err != nil {
					t.Fatalf("attach key: %v", err)
				}
				request = request.WithContext(ctx)
				request.Header.Set("Idempotency-Key", "header-key")

				return request
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
		{
			name: "invalid context provenance",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller,
			},
			prepare: func(t *testing.T, request *http.Request) *http.Request {
				t.Helper()
				ctx := context.WithValue(request.Context(), idempotencyKeyContextKey{}, IdempotencyKey{
					Value: "generated-looking", Provenance: IdempotencyGenerated,
				})

				return request.WithContext(ctx)
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
		{
			name: "endpoint maximum",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint, Mode: IdempotencyRequireCaller, MaximumLength: 4,
			},
			prepare: func(t *testing.T, request *http.Request) *http.Request {
				t.Helper()
				request.Header.Set("Idempotency-Key", "too-long")

				return request
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
		{
			name: "generator failure",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint,
				Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
					return GeneratedIdentifier{}, secretCause
				}),
			},
			wantCause: secretCause, credential: "do-not-render",
		},
		{
			name: "generator entropy",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint,
				Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
					return GeneratedIdentifier{Value: "candidate", EntropyBits: 8}, nil
				}),
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
		{
			name: "generator value",
			options: IdempotencyOptions{
				Name: "idempotency", Layer: MiddlewareEndpoint,
				Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
					return GeneratedIdentifier{Value: "invalid value", EntropyBits: 128}, nil
				}),
			},
			wantCause: ErrInvalidIdempotencyKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			middleware, err := NewIdempotencyMiddleware(test.options)
			if err != nil {
				t.Fatalf("construct middleware: %v", err)
			}
			client, err := New(Config{
				Middleware: middleware,
				Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("transport must not run")

					return nil, nil
				}),
			})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close client: %v", err)
				}
			}()
			request, err := http.NewRequest(http.MethodPost, "https://api.example.test", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			if test.prepare != nil {
				request = test.prepare(t, request)
			}
			_, err = client.Do(request)
			var idempotencyError *IdempotencyError
			if !errors.As(err, &idempotencyError) || !errors.Is(err, test.wantCause) {
				t.Fatalf("idempotency error = %#v", err)
			}
			if test.credential != "" && strings.Contains(err.Error(), test.credential) {
				t.Fatalf("idempotency error rendered credential: %q", err)
			}
		})
	}
}

func TestIdempotencyContextAndAttemptBoundaries(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	if idempotencyPolicyApplied(nilContext) {
		t.Fatal("nil context reported applied idempotency policy")
	}

	middleware, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name: "idempotency", Layer: MiddlewareEndpoint,
		Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "generated", EntropyBits: 128}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	operationOnly, err := NewPipeline(middleware[0])
	if err != nil {
		t.Fatalf("construct operation pipeline: %v", err)
	}
	_, err = operationOnly.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if !errors.Is(err, ErrInvalidOperationIdentity) {
		t.Fatalf("missing operation identity error = %v", err)
	}
	attemptOnly, err := NewPipeline(middleware[1])
	if err != nil {
		t.Fatalf("construct attempt pipeline: %v", err)
	}
	_, err = attemptOnly.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if !errors.Is(err, ErrInvalidIdempotencyPolicy) {
		t.Fatalf("missing idempotency state error = %v", err)
	}

	identity, err := newOperationIdentityMiddleware(IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
		return GeneratedIdentifier{Value: "original-operation", EntropyBits: 128}, nil
	}))
	if err != nil {
		t.Fatalf("construct identity middleware: %v", err)
	}
	tamper := mustRequestMiddleware(t, MiddlewareOptions{
		Name: "tamper-operation", Scope: ScopeOperation, Layer: MiddlewareOneShot,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		ctx := context.WithValue(request.Context(), operationIdentityContextKey{}, OperationIdentity{
			ID: "different-operation", Provenance: IdentityCaller,
		})

		return next(request.WithContext(ctx))
	})
	pipeline, err := NewPipeline(identity, middleware[0], middleware[1], tamper)
	if err != nil {
		t.Fatalf("construct tampered pipeline: %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run")

		return nil, nil
	}))
	if !errors.Is(err, ErrInvalidOperationIdentity) {
		t.Fatalf("mismatched operation identity error = %v", err)
	}
}

func TestIdempotencyKeyFormattingIsRedacted(t *testing.T) {
	t.Parallel()

	key := IdempotencyKey{Value: "do-not-render", Provenance: IdempotencyCallerHeader}
	for _, rendered := range []string{fmt.Sprintf("%v", key), fmt.Sprintf("%#v", key), key.String()} {
		if strings.Contains(rendered, "do-not-render") {
			t.Fatalf("formatted key leaked value: %q", rendered)
		}
	}
}

type idempotencyTestAttemptPolicy struct{}

func (*idempotencyTestAttemptPolicy) PreserveKey(*http.Request, *http.Request) bool { return true }
