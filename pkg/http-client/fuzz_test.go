package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func FuzzRequestSpecURL(fuzz *testing.F) {
	fuzz.Add("https://api.example.test/v1/", "widgets?state=active")
	fuzz.Add("https://api.example.test/a%2Fb/", "../items#section")
	fuzz.Add("http://127.0.0.1:8080/", "?q=%00")
	fuzz.Fuzz(func(t *testing.T, base string, reference string) {
		if len(base)+len(reference) > 8<<10 {
			t.Skip()
		}
		spec, err := NewRequestSpec(base, reference)
		if err != nil {
			return
		}
		request, err := spec.Build(context.Background(), http.MethodGet)
		if err != nil {
			return
		}
		if request.URL.User != nil || request.URL.Scheme != "http" && request.URL.Scheme != "https" {
			t.Fatalf("unsafe accepted URL: %s", request.URL)
		}
	})
}

func FuzzHeaderValidation(fuzz *testing.F) {
	fuzz.Add("X-Vendor-Mode", "stable")
	fuzz.Add("Content-Type", "application/json")
	fuzz.Add("bad header", "value\r\ninjected: true")
	fuzz.Fuzz(func(t *testing.T, name string, value string) {
		if len(name)+len(value) > 8<<10 {
			t.Skip()
		}
		canonical, err := validateHeader(name, []string{value})
		if err != nil {
			return
		}
		if canonical == "" || !validHeaderValue(value) {
			t.Fatal("header validation accepted a non-canonical unsafe value")
		}
	})
}

func FuzzAuthenticationInputs(fuzz *testing.F) {
	fuzz.Add("user", "password", "token-value", "X-API-Key")
	fuzz.Add("user:name", "secret", "bad token", "bad header")
	fuzz.Fuzz(func(t *testing.T, username string, password string, token string, header string) {
		if len(username)+len(password)+len(token)+len(header) > 8<<10 {
			t.Skip()
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, constructor := range []func() (RequestEditor, error){
			func() (RequestEditor, error) { return NewBasicAuth(username, password) },
			func() (RequestEditor, error) { return NewBearerAuth(token) },
			func() (RequestEditor, error) { return NewAPIKeyHeader(header, token) },
		} {
			editor, constructErr := constructor()
			if constructErr == nil {
				clone := request.Clone(request.Context())
				if editErr := editor.EditRequest(clone); editErr != nil {
					t.Fatalf("accepted authentication input failed to apply: %v", editErr)
				}
			}
		}
	})
}

func FuzzAuthenticationChallengeHeaders(fuzz *testing.F) {
	fuzz.Add(`Bearer realm="vendor", error="invalid_token"`)
	fuzz.Add("Basic realm=\"legacy\"")
	fuzz.Fuzz(func(t *testing.T, challenge string) {
		if len(challenge) > 8<<10 {
			t.Skip()
		}
		response := &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header: http.Header{
				"Www-Authenticate": []string{challenge},
			},
			Body: http.NoBody,
		}
		err := ClassifyResponse(response, StatusOptions{})
		if err == nil || len(challenge) >= 8 && bytes.Contains([]byte(err.Error()), []byte(challenge)) {
			t.Fatal("authentication challenge escaped safe status classification")
		}
	})
}

func FuzzErrorPayloadClassification(fuzz *testing.F) {
	fuzz.Add([]byte(`{"error":"invalid token"}`), 401)
	fuzz.Add([]byte{0, 1, 2, 255}, 503)
	fuzz.Fuzz(func(t *testing.T, payload []byte, status int) {
		if len(payload) > 1<<20 {
			t.Skip()
		}
		if status < 100 || status > 999 {
			status = http.StatusInternalServerError
		}
		response := &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}
		err := ClassifyResponse(response, StatusOptions{
			MaximumExcerptBytes: 256,
			RedactExcerpt: func(content []byte) ([]byte, error) {
				for index := range content {
					content[index] = '*'
				}
				return content, nil
			},
		})
		if status >= 200 && status < 300 {
			if err != nil {
				t.Fatalf("accepted status failed: %v", err)
			}
			_ = response.Body.Close()
			return
		}
		if err == nil || len(payload) >= 8 && bytes.Contains([]byte(err.Error()), payload) {
			t.Fatal("rejected payload was not safely classified")
		}
	})
}

func FuzzRedirectCredentialBoundary(fuzz *testing.F) {
	fuzz.Add("https://api.example.test/next")
	fuzz.Add("https://other.example.test/next")
	fuzz.Add("https://user:secret@api.example.test/next")
	fuzz.Add("http://api.example.test/next")
	fuzz.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8<<10 {
			t.Skip()
		}
		candidate, err := url.Parse(raw)
		if err != nil {
			return
		}
		editor, err := NewBearerAuth("token")
		if err != nil {
			t.Fatal(err)
		}
		middleware, err := NewAuthenticationMiddleware(AuthenticationOptions{
			Name: "fuzz-auth", Layer: MiddlewareClient,
		}, editor)
		if err != nil {
			t.Fatal(err)
		}
		pipeline, err := NewPipeline(middleware...)
		if err != nil {
			t.Fatal(err)
		}
		initial, err := http.NewRequest(http.MethodGet, "https://api.example.test/start", nil)
		if err != nil {
			t.Fatal(err)
		}
		operationResponse, operationErr := pipeline.executeOperation(initial, func(operation *http.Request) (*http.Response, error) {
			redirect := operation.Clone(operation.Context())
			redirect.URL = candidate
			redirect.Header.Set("Authorization", "caller-secret")
			transportCalled := false
			response, executeErr := pipeline.executeAttempt(redirect, Next(func(request *http.Request) (*http.Response, error) {
				transportCalled = true
				origin, originErr := canonicalOrigin(candidate)
				trusted := originErr == nil && origin == "https://api.example.test"
				if trusted && request.Header.Get("Authorization") != "Bearer token" {
					t.Fatal("trusted redirect lost attempt credential")
				}
				if !trusted && request.Header.Get("Authorization") != "" {
					t.Fatal("credential crossed redirect trust boundary")
				}

				return &http.Response{StatusCode: http.StatusNoContent}, nil
			}))
			if executeErr == nil {
				_ = response.Body.Close()
			}
			if !transportCalled && executeErr == nil {
				t.Fatal("redirect disappeared without a typed failure")
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		})
		if operationErr != nil {
			t.Fatalf("execute redirect operation: %v", operationErr)
		}
		_ = operationResponse.Body.Close()
	})
}

func FuzzRetryPolicy(fuzz *testing.F) {
	fuzz.Add(http.MethodGet, http.StatusServiceUnavailable, "1", 3, false, false)
	fuzz.Add(http.MethodPost, http.StatusServiceUnavailable, "999999999", 2, true, true)
	fuzz.Add(http.MethodGet, http.StatusOK, "invalid", 101, false, false)
	fuzz.Fuzz(func(
		t *testing.T,
		method string,
		status int,
		retryAfter string,
		attempts int,
		unsafe bool,
		hasIdempotency bool,
	) {
		if len(method)+len(retryAfter) > 8<<10 {
			t.Skip()
		}
		options := RetryOptions{
			MaximumAttempts:   attempts,
			MaximumElapsed:    time.Second,
			MaximumRetryAfter: time.Second,
		}
		resolved, err := resolveRetryOptions(options)
		if attempts < 2 || attempts > maximumRetryAttempts {
			if err == nil {
				t.Fatal("invalid attempt bound was accepted")
			}
			return
		}
		if err != nil {
			t.Fatalf("bounded retry options failed: %v", err)
		}
		response := &http.Response{
			StatusCode: status,
			Header:     http.Header{"Retry-After": {retryAfter}},
		}
		delay := retryDelay(response, 1, resolved)
		if delay < 0 || delay > resolved.maximumRetryAfter {
			t.Fatalf("retry delay escaped bounds: %s", delay)
		}
		request, requestErr := http.NewRequest(method, "https://api.example.test", nil)
		if requestErr != nil {
			return
		}
		policy := defaultRetryPolicy{unsafeIdempotency: unsafe}
		if policy.ShouldRetry(RetryAttempt{
			Request: request, Response: response, Attempt: 1,
			BodyReplayable: true, HasIdempotency: hasIdempotency,
		}) && !retrySafeMethod(request.Method) && (!unsafe || !hasIdempotency) {
			t.Fatal("unsafe operation was accepted for retry")
		}
	})
}
