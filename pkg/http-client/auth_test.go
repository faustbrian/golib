package httpclient

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCredentialEditorsApplyBasicBearerAndAPIKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		new    func() (RequestEditor, error)
		assert func(*testing.T, *http.Request)
	}{
		{
			name: "basic",
			new: func() (RequestEditor, error) {
				return NewBasicAuth("client", "secret")
			},
			assert: func(t *testing.T, request *http.Request) {
				t.Helper()
				username, password, ok := request.BasicAuth()
				if !ok || username != "client" || password != "secret" {
					t.Fatalf("unexpected basic credentials: %q %q %v", username, password, ok)
				}
			},
		},
		{
			name: "bearer",
			new: func() (RequestEditor, error) {
				return NewBearerAuth("opaque.token~+/=")
			},
			assert: func(t *testing.T, request *http.Request) {
				t.Helper()
				if got := request.Header.Get("Authorization"); got != "Bearer opaque.token~+/=" {
					t.Fatalf("unexpected authorization: %q", got)
				}
			},
		},
		{
			name: "header API key",
			new: func() (RequestEditor, error) {
				return NewAPIKeyHeader("X-API-Key", "header-secret")
			},
			assert: func(t *testing.T, request *http.Request) {
				t.Helper()
				if got := request.Header.Values("X-API-Key"); len(got) != 1 || got[0] != "header-secret" {
					t.Fatalf("unexpected API key header: %q", got)
				}
			},
		},
		{
			name: "explicit query API key",
			new: func() (RequestEditor, error) {
				return NewAPIKeyQuery("api_key", "query secret&value")
			},
			assert: func(t *testing.T, request *http.Request) {
				t.Helper()
				if got := request.URL.Query()["api_key"]; len(got) != 1 || got[0] != "query secret&value" {
					t.Fatalf("unexpected API key query: %q", got)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			editor, err := test.new()
			if err != nil {
				t.Fatalf("construct editor: %v", err)
			}
			request, err := http.NewRequest(http.MethodGet, "https://api.example.test/items?keep=1", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			request.Header.Set("Authorization", "old")
			request.Header.Add("X-API-Key", "old")

			if err := editor.EditRequest(request); err != nil {
				t.Fatalf("edit request: %v", err)
			}
			test.assert(t, request)
		})
	}
}

func TestCredentialEditorsRejectInvalidConfigurationWithoutRenderingSecrets(t *testing.T) {
	t.Parallel()

	secret := "do-not-render\r\nsecret"
	tests := []struct {
		name string
		new  func() (RequestEditor, error)
	}{
		{name: "basic colon", new: func() (RequestEditor, error) { return NewBasicAuth("bad:user", secret) }},
		{name: "bearer empty", new: func() (RequestEditor, error) { return NewBearerAuth("") }},
		{name: "bearer syntax", new: func() (RequestEditor, error) { return NewBearerAuth(secret) }},
		{name: "API key header name", new: func() (RequestEditor, error) { return NewAPIKeyHeader("Bad Header", secret) }},
		{name: "API key header value", new: func() (RequestEditor, error) { return NewAPIKeyHeader("X-Key", secret) }},
		{name: "API key query name", new: func() (RequestEditor, error) { return NewAPIKeyQuery("", secret) }},
		{name: "API key query value", new: func() (RequestEditor, error) { return NewAPIKeyQuery("key", "") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.new()
			if !errors.Is(err, ErrInvalidAuthentication) {
				t.Fatalf("expected invalid authentication, got %v", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error rendered credential: %q", err)
			}
		})
	}
}

func TestRequestEditorMiddlewareAdaptsGeneratedClientEditors(t *testing.T) {
	t.Parallel()

	editor := RequestEditorFunc(func(request *http.Request) error {
		request.Header.Set("X-Generated-Client", "edited")

		return nil
	})
	middleware, err := NewRequestEditorMiddleware(MiddlewareOptions{
		Name:  "generated-client-editor",
		Scope: ScopeAttempt,
		Layer: MiddlewareEndpoint,
	}, editor)
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	pipeline, err := NewPipeline(middleware)
	if err != nil {
		t.Fatalf("construct pipeline: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.example.test/items", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}

	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-Generated-Client"); got != "edited" {
			t.Fatalf("editor header = %q", got)
		}

		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if err != nil {
		t.Fatalf("execute pipeline: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	if request.Header.Get("X-Generated-Client") != "" {
		t.Fatal("editor mutated the caller request")
	}
}

func TestAuthenticationMiddlewareReappliesOnlyWithinTrustedOrigin(t *testing.T) {
	t.Parallel()

	var trustedCalls atomic.Int64
	trusted := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/start":
			if got := request.Header.Get("Authorization"); got != "Bearer token" {
				t.Errorf("initial authorization = %q", got)
			}
			http.Redirect(writer, request, "/finish", http.StatusFound)
		case "/finish":
			trustedCalls.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer token" {
				t.Errorf("redirect authorization = %q", got)
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected trusted path %q", request.URL.Path)
		}
	}))
	defer trusted.Close()

	var untrustedAuthorization string
	var untrustedAPIKey string
	untrusted := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		untrustedAuthorization = request.Header.Get("Authorization")
		untrustedAPIKey = request.Header.Get("X-API-Key")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer untrusted.Close()

	t.Run("same origin", func(t *testing.T) {
		editor, err := NewBearerAuth("token")
		if err != nil {
			t.Fatalf("construct editor: %v", err)
		}
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name:          "vendor-auth",
			Layer:         MiddlewareClient,
			AllowInsecure: true,
		}, editor)
		if err != nil {
			t.Fatalf("construct authentication middleware: %v", err)
		}
		client, err := New(Config{Middleware: middleware})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		defer func() {
			if err := client.Close(); err != nil {
				t.Errorf("close client: %v", err)
			}
		}()
		request, err := http.NewRequest(http.MethodGet, trusted.URL+"/start", nil)
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
		if trustedCalls.Load() != 1 {
			t.Fatalf("trusted redirect calls = %d", trustedCalls.Load())
		}
	})

	t.Run("cross origin", func(t *testing.T) {
		redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, untrusted.URL, http.StatusFound)
		}))
		defer redirector.Close()

		editor, err := NewAPIKeyHeader("X-API-Key", "header-secret")
		if err != nil {
			t.Fatalf("construct editor: %v", err)
		}
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name:             "vendor-auth",
			Layer:            MiddlewareClient,
			SensitiveHeaders: []string{"X-API-Key"},
			AllowInsecure:    true,
		}, editor)
		if err != nil {
			t.Fatalf("construct authentication middleware: %v", err)
		}
		client, err := New(Config{Middleware: middleware})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		defer func() {
			if err := client.Close(); err != nil {
				t.Errorf("close client: %v", err)
			}
		}()
		request, err := http.NewRequest(http.MethodGet, redirector.URL, nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		request.Header.Set("Authorization", "caller-secret")
		request.Header.Set("X-API-Key", "caller-api-key")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
		if untrustedAuthorization != "" || untrustedAPIKey != "" {
			t.Fatalf("credentials crossed origin: authorization=%q API-key=%q", untrustedAuthorization, untrustedAPIKey)
		}
	})
}

func TestHMACAuthSnapshotsSecretAndDelegatesVendorCanonicalization(t *testing.T) {
	t.Parallel()

	secret := []byte("signing-secret")
	editor, err := NewHMACAuth(HMACOptions{
		Secret:  secret,
		NewHash: sha256.New,
		Canonicalize: func(request *http.Request) ([]byte, error) {
			return []byte(request.Method + "\n" + request.URL.EscapedPath() + "\n" + request.Header.Get("X-Key-ID")), nil
		},
		ApplySignature: func(request *http.Request, signature []byte) error {
			request.Header.Set("X-Signature", hex.EncodeToString(signature))

			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct HMAC editor: %v", err)
	}
	clear(secret)
	request, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1/items", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	request.Header.Set("X-Key-ID", "key-1")
	if err := editor.EditRequest(request); err != nil {
		t.Fatalf("edit request: %v", err)
	}

	mac := hmac.New(sha256.New, []byte("signing-secret"))
	_, _ = mac.Write([]byte("POST\n/v1/items\nkey-1"))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := request.Header.Get("X-Signature"); got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestHMACAuthErrorsPreserveCausesWithoutRenderingCredentials(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("signature failure includes do-not-render")
	tests := []struct {
		name    string
		options HMACOptions
		phase   HMACPhase
	}{
		{
			name: "canonicalization",
			options: HMACOptions{
				Secret:  []byte("secret"),
				NewHash: sha256.New,
				Canonicalize: func(*http.Request) ([]byte, error) {
					return nil, secretCause
				},
				ApplySignature: func(*http.Request, []byte) error { return nil },
			},
			phase: HMACCanonicalization,
		},
		{
			name: "application",
			options: HMACOptions{
				Secret:       []byte("secret"),
				NewHash:      sha256.New,
				Canonicalize: func(*http.Request) ([]byte, error) { return []byte("canonical"), nil },
				ApplySignature: func(*http.Request, []byte) error {
					return secretCause
				},
			},
			phase: HMACApplication,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			editor, err := NewHMACAuth(test.options)
			if err != nil {
				t.Fatalf("construct HMAC editor: %v", err)
			}
			request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			err = editor.EditRequest(request)
			var hmacError *HMACError
			if !errors.As(err, &hmacError) || hmacError.Phase != test.phase || !errors.Is(err, secretCause) {
				t.Fatalf("unexpected HMAC error: %#v", err)
			}
			if strings.Contains(err.Error(), "do-not-render") {
				t.Fatalf("error rendered sensitive cause: %q", err)
			}
		})
	}
}

func TestHMACAuthRejectsIncompletePolicy(t *testing.T) {
	t.Parallel()

	valid := HMACOptions{
		Secret:         []byte("secret"),
		NewHash:        sha256.New,
		Canonicalize:   func(*http.Request) ([]byte, error) { return nil, nil },
		ApplySignature: func(*http.Request, []byte) error { return nil },
	}
	tests := []struct {
		name   string
		mutate func(*HMACOptions)
	}{
		{name: "empty secret", mutate: func(options *HMACOptions) { options.Secret = nil }},
		{name: "nil hash", mutate: func(options *HMACOptions) { options.NewHash = nil }},
		{name: "nil canonicalizer", mutate: func(options *HMACOptions) { options.Canonicalize = nil }},
		{name: "nil signature writer", mutate: func(options *HMACOptions) { options.ApplySignature = nil }},
		{name: "nil hash result", mutate: func(options *HMACOptions) { options.NewHash = func() hash.Hash { return nil } }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := valid
			test.mutate(&options)
			_, err := NewHMACAuth(options)
			if !errors.Is(err, ErrInvalidAuthentication) {
				t.Fatalf("expected invalid authentication, got %v", err)
			}
		})
	}
}

func TestAuthenticationConfigurationAndRuntimeBoundaries(t *testing.T) {
	t.Parallel()

	var typedNil *authenticationTestEditor
	if _, err := NewRequestEditorMiddleware(MiddlewareOptions{}, typedNil); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("typed-nil request editor error = %v", err)
	}
	if _, err := NewAuthenticationMiddleware(AuthenticationOptions{}, typedNil); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("typed-nil authentication editor error = %v", err)
	}
	validEditor, err := NewBearerAuth("token")
	if err != nil {
		t.Fatalf("construct bearer editor: %v", err)
	}
	invalidOptions := []AuthenticationOptions{
		{Name: "Invalid", Layer: MiddlewareClient},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"https://user:secret@example.test"}},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"https://example.test/path"}},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"https://example.test:invalid"}},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"http://example.test"}},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"ftp://example.test"}},
		{Name: "auth", Layer: MiddlewareClient, AllowedOrigins: []string{"ftp://example.test"}, AllowInsecure: true},
		{Name: "auth", Layer: MiddlewareClient, SensitiveHeaders: []string{"Bad Header"}},
	}
	for _, options := range invalidOptions {
		if _, err := NewAuthenticationMiddleware(options, validEditor); err == nil {
			t.Fatalf("expected options rejection: %#v", options)
		}
	}

	t.Run("configured origin", func(t *testing.T) {
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name:           "auth",
			Layer:          MiddlewareClient,
			AllowedOrigins: []string{"https://API.EXAMPLE.test:443/"},
		}, validEditor)
		if err != nil {
			t.Fatalf("construct middleware: %v", err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			t.Fatalf("construct pipeline: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test/items", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
			if got := request.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("authorization = %q", got)
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		_ = response.Body.Close()
	})

	t.Run("missing operation context", func(t *testing.T) {
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{Name: "auth", Layer: MiddlewareClient}, validEditor)
		if err != nil {
			t.Fatalf("construct middleware: %v", err)
		}
		pipeline, err := NewPipeline(middleware[1])
		if err != nil {
			t.Fatalf("construct attempt-only pipeline: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}))
		if !errors.Is(err, ErrInvalidAuthentication) {
			t.Fatalf("missing context error = %v", err)
		}
	})

	t.Run("editor failure", func(t *testing.T) {
		secretCause := errors.New("credential do-not-render")
		middleware, err := NewRequestEditorMiddleware(MiddlewareOptions{
			Name: "failing-editor", Scope: ScopeAttempt, Layer: MiddlewareClient,
		}, RequestEditorFunc(func(*http.Request) error { return secretCause }))
		if err != nil {
			t.Fatalf("construct middleware: %v", err)
		}
		pipeline, err := NewPipeline(middleware)
		if err != nil {
			t.Fatalf("construct pipeline: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport must not run")

			return nil, nil
		}))
		var editorError *RequestEditorError
		if !errors.As(err, &editorError) || !errors.Is(err, secretCause) {
			t.Fatalf("editor error = %#v", err)
		}
		if strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("editor error rendered cause: %q", err)
		}
	})

	t.Run("authentication editor failure", func(t *testing.T) {
		secretCause := errors.New("credential do-not-render")
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name: "auth", Layer: MiddlewareClient,
		}, RequestEditorFunc(func(*http.Request) error { return secretCause }))
		if err != nil {
			t.Fatalf("construct middleware: %v", err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			t.Fatalf("construct pipeline: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport must not run")

			return nil, nil
		}))
		var editorError *RequestEditorError
		if !errors.As(err, &editorError) || !errors.Is(err, secretCause) {
			t.Fatalf("authentication editor error = %#v", err)
		}
	})

	t.Run("invalid operation and attempt URLs", func(t *testing.T) {
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{Name: "auth", Layer: MiddlewareClient}, validEditor)
		if err != nil {
			t.Fatalf("construct middleware: %v", err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			t.Fatalf("construct pipeline: %v", err)
		}
		_, err = pipeline.Execute(&http.Request{Method: http.MethodGet, Header: make(http.Header)}, TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}))
		if !errors.Is(err, ErrInvalidAuthentication) {
			t.Fatalf("nil operation URL error = %v", err)
		}

		breakURL := mustRequestMiddleware(t, MiddlewareOptions{
			Name: "break-url", Scope: ScopeOperation, Layer: MiddlewareOneShot,
		}, func(request *http.Request, next Next) (*http.Response, error) {
			request.URL = nil

			return next(request)
		})
		pipeline, err = NewPipeline(append(middleware, breakURL)...)
		if err != nil {
			t.Fatalf("construct URL-breaking pipeline: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}))
		if !errors.Is(err, ErrInvalidAuthentication) {
			t.Fatalf("nil attempt URL error = %v", err)
		}
	})
}

func TestCredentialEditorAndHMACHostileBoundaries(t *testing.T) {
	t.Parallel()

	constructors := []func() (RequestEditor, error){
		func() (RequestEditor, error) { return NewBasicAuth("user", "password") },
		func() (RequestEditor, error) { return NewBearerAuth("token") },
		func() (RequestEditor, error) { return NewAPIKeyHeader("X-Key", "secret") },
		func() (RequestEditor, error) { return NewAPIKeyQuery("key", "secret") },
	}
	for _, constructor := range constructors {
		editor, err := constructor()
		if err != nil {
			t.Fatalf("construct editor: %v", err)
		}
		if err := editor.EditRequest(nil); !errors.Is(err, ErrInvalidAuthentication) {
			t.Fatalf("nil request error = %v", err)
		}
	}
	queryEditor, err := NewAPIKeyQuery("key", "secret")
	if err != nil {
		t.Fatalf("construct query editor: %v", err)
	}
	if err := queryEditor.EditRequest(&http.Request{}); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("nil URL error = %v", err)
	}

	if got := HMACCalculation.String(); got != "calculation" {
		t.Fatalf("calculation phase = %q", got)
	}
	if got := HMACPhase(99).String(); got != "phase(99)" {
		t.Fatalf("unknown phase = %q", got)
	}
	if _, err := NewHMACAuth(HMACOptions{
		Secret: []byte("secret"),
		NewHash: func() hash.Hash {
			panic("do-not-render")
		},
		Canonicalize:   func(*http.Request) ([]byte, error) { return nil, nil },
		ApplySignature: func(*http.Request, []byte) error { return nil },
	}); !errors.Is(err, ErrInvalidAuthentication) || strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("panicking hash factory error = %v", err)
	}

	var calls atomic.Int64
	hmacEditor, err := NewHMACAuth(HMACOptions{
		Secret: []byte("secret"),
		NewHash: func() hash.Hash {
			if calls.Add(1) > 1 {
				panic("runtime do-not-render")
			}

			return sha256.New()
		},
		Canonicalize:   func(*http.Request) ([]byte, error) { return nil, nil },
		ApplySignature: func(*http.Request, []byte) error { return nil },
	})
	if err != nil {
		t.Fatalf("construct runtime-failing HMAC editor: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	err = hmacEditor.EditRequest(request)
	var hmacError *HMACError
	if !errors.As(err, &hmacError) || hmacError.Phase != HMACCalculation || strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("runtime hash failure = %#v", err)
	}

	validHMAC, err := NewHMACAuth(HMACOptions{
		Secret:         []byte("secret"),
		NewHash:        sha256.New,
		Canonicalize:   func(*http.Request) ([]byte, error) { return nil, nil },
		ApplySignature: func(*http.Request, []byte) error { return nil },
	})
	if err != nil {
		t.Fatalf("construct valid HMAC editor: %v", err)
	}
	if err := validHMAC.EditRequest(nil); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("nil HMAC request error = %v", err)
	}
}

func TestCanonicalOriginNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		candidate *url.URL
		want      string
	}{
		{candidate: &url.URL{Scheme: "https", Host: "EXAMPLE.test:443"}, want: "https://example.test"},
		{candidate: &url.URL{Scheme: "http", Host: "EXAMPLE.test:80"}, want: "http://example.test"},
		{candidate: &url.URL{Scheme: "https", Host: "EXAMPLE.test:8443"}, want: "https://example.test:8443"},
		{candidate: &url.URL{Scheme: "https", Host: "[2001:db8::1]"}, want: "https://[2001:db8::1]"},
		{candidate: &url.URL{Scheme: "https", Host: "[2001:db8::1]:8443"}, want: "https://[2001:db8::1]:8443"},
	}
	for _, test := range tests {
		got, err := canonicalOrigin(test.candidate)
		if err != nil || got != test.want {
			t.Fatalf("canonicalOrigin(%v) = %q, %v; want %q", test.candidate, got, err, test.want)
		}
	}
	invalid := []*url.URL{
		nil,
		{Scheme: "ftp", Host: "example.test"},
		{Scheme: "https"},
		{Scheme: "http", Host: ":80"},
		{Scheme: "https", Host: "example.test", User: url.UserPassword("user", "secret")},
		{Scheme: "https", Host: "example.test:invalid"},
		{Scheme: "https", Host: "example.test:0"},
		{Scheme: "https", Host: "example.test:65536"},
	}
	for _, candidate := range invalid {
		if _, err := canonicalOrigin(candidate); !errors.Is(err, ErrInvalidAuthentication) {
			t.Fatalf("invalid origin %v error = %v", candidate, err)
		}
	}
}

func TestAuthenticationRejectsCleartextCredentialTransport(t *testing.T) {
	t.Parallel()

	editor, err := NewBearerAuth("token")
	if err != nil {
		t.Fatalf("construct editor: %v", err)
	}
	middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "auth", Layer: MiddlewareClient,
	}, editor)
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	pipeline, err := NewPipeline(middleware...)
	if err != nil {
		t.Fatalf("construct pipeline: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "http://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("cleartext credential request reached transport")

		return nil, nil
	}))
	if !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("cleartext authentication error = %v", err)
	}

	middleware, err = NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "auth", Layer: MiddlewareClient, AllowInsecure: true,
	}, editor)
	if err != nil {
		t.Fatalf("construct insecure opt-in middleware: %v", err)
	}
	pipeline, err = NewPipeline(middleware...)
	if err != nil {
		t.Fatalf("construct insecure opt-in pipeline: %v", err)
	}
	response, err := pipeline.Execute(request, TransportFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("opt-in authorization = %q", got)
		}

		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if err != nil {
		t.Fatalf("execute insecure opt-in: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close insecure opt-in response: %v", closeErr)
	}

	request = request.WithContext(context.WithValue(request.Context(), authenticationContextKey{}, authenticationScope{
		origins: map[string]struct{}{"http://api.example.test": {}},
	}))
	pipeline, err = NewPipeline(middleware[1])
	if err != nil {
		t.Fatalf("construct attempt-only pipeline: %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("corrupt cleartext scope reached transport")
		return nil, nil
	}))
	if !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("corrupt cleartext scope error = %v", err)
	}
}

type authenticationTestEditor struct{}

func (*authenticationTestEditor) EditRequest(*http.Request) error { return nil }
