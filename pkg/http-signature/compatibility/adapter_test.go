package compatibility_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-signature/compatibility"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type observedBody struct {
	reader io.Reader
	closed bool
}

func (body *observedBody) Read(buffer []byte) (int, error) {
	if body.closed {
		return 0, errors.New("read after close")
	}
	return body.reader.Read(buffer)
}

func (body *observedBody) Close() error {
	body.closed = true
	return nil
}

func TestExplicitLegacySigningAdaptersRemainProtocolSeparated(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		new  func(compatibility.SigningRoundTripperConfig) (*compatibility.SigningRoundTripper, error)
		want compatibility.Protocol
	}{
		{"cavage", compatibility.NewCavageSigningRoundTripper, compatibility.CavageDraft},
		{"aws", compatibility.NewAWSSigV4SigningRoundTripper, compatibility.AWSSigV4},
		{"oauth", compatibility.NewOAuth1SigningRoundTripper, compatibility.OAuth1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			original := httptest.NewRequest(http.MethodPost, "https://example.com/resource", strings.NewReader("body"))
			var delegated *http.Request
			adapter, err := test.new(compatibility.SigningRoundTripperConfig{
				Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					delegated = request
					return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
				}),
				Sign: func(_ context.Context, request *http.Request) error {
					request.Header.Set("Authorization", "external-format")
					return nil
				},
				ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			if adapter.Protocol() != test.want {
				t.Fatalf("protocol = %q, want %q", adapter.Protocol(), test.want)
			}
			response, err := adapter.RoundTrip(original)
			if err != nil || response.StatusCode != http.StatusNoContent {
				t.Fatalf("RoundTrip() = %#v, %v", response, err)
			}
			if delegated == original || original.Header.Get("Authorization") != "" || delegated.Header.Get("Authorization") == "" {
				t.Fatal("adapter did not isolate protocol-specific request mutation")
			}
			if delegated.Body != original.Body {
				t.Fatal("adapter replaced caller body")
			}
		})
	}
}

func TestSigningCallbackCannotContaminateRFCRequestIdentity(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: strings.NewReader("body")}
	original, err := http.NewRequest(http.MethodPost, "https://origin.example/resource?one=1", body)
	if err != nil {
		t.Fatal(err)
	}
	original.Host = "origin.example"
	original.RequestURI = "/resource?one=1"
	original.Header.Set("Signature-Input", `rfc=("@method");keyid="rfc"`)
	original.Header.Set("Signature", "rfc=:AQID:")
	original.Trailer = http.Header{"Signature": []string{"rfc=:BAUG:"}}

	var delegated *http.Request
	adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			delegated = request
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
		}),
		Sign: func(_ context.Context, request *http.Request) error {
			if _, err := request.Body.Read(make([]byte, 1)); err == nil {
				return errors.New("callback request body was readable")
			}
			request.Method = http.MethodDelete
			request.URL.Scheme = "http"
			request.URL.Host = "attacker.example"
			request.URL.Path = "/other"
			request.URL.RawQuery = "two=2"
			request.Host = "attacker.example"
			request.RequestURI = "/other?two=2"
			if err := request.Body.Close(); err != nil {
				return err
			}
			request.Body = http.NoBody
			request.Header.Set("Authorization", `Signature keyId="legacy"`)
			request.Header.Set("Signature-Input", `legacy=("@method")`)
			request.Header["signature"] = []string{"legacy=:c2ln:"}
			request.Header.Set("Accept-Signature", `legacy=("@method")`)
			request.Trailer.Set("X-Legacy-Proof", "signed")
			request.Trailer.Set("Signature", "legacy=:dHJhaWxlcg==:")
			return nil
		},
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.RoundTrip(original)
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("RoundTrip() = %#v, %v", response, err)
	}
	if delegated.Method != http.MethodPost || delegated.URL.String() != "https://origin.example/resource?one=1" || delegated.Host != "origin.example" || delegated.RequestURI != "/resource?one=1" {
		t.Fatalf("delegated request identity = %s %s, host %q, request URI %q", delegated.Method, delegated.URL, delegated.Host, delegated.RequestURI)
	}
	if delegated.Body != body || body.closed {
		t.Fatalf("delegated body = %#v, original closed = %v", delegated.Body, body.closed)
	}
	if delegated.Header.Get("Authorization") == "" {
		t.Fatal("vendor authorization mutation was discarded")
	}
	if got := delegated.Header.Values("Signature-Input"); len(got) != 1 || got[0] != `rfc=("@method");keyid="rfc"` {
		t.Fatalf("Signature-Input = %q", got)
	}
	if got := delegated.Header.Values("Signature"); len(got) != 1 || got[0] != "rfc=:AQID:" {
		t.Fatalf("Signature = %q", got)
	}
	if got := delegated.Header.Values("Accept-Signature"); len(got) != 0 {
		t.Fatalf("added Accept-Signature = %q", got)
	}
	if got := delegated.Trailer.Values("Signature"); len(got) != 1 || got[0] != "rfc=:BAUG:" {
		t.Fatalf("Signature trailer = %q", got)
	}
	if got := delegated.Trailer.Get("X-Legacy-Proof"); got != "signed" {
		t.Fatalf("X-Legacy-Proof trailer = %q", got)
	}
}

func TestCompatibilityCallbacksCannotReachCallerBodyThroughGetBody(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*testing.T, *http.Request)
	}{
		{
			name: "signing",
			run: func(t *testing.T, request *http.Request) {
				t.Helper()
				adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
					Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
					}),
					Sign: func(_ context.Context, request *http.Request) error {
						return accessBodyThroughGetBody(request)
					},
					ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := adapter.RoundTrip(request); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "verification",
			run: func(t *testing.T, request *http.Request) {
				t.Helper()
				middleware, err := compatibility.NewCavageVerificationMiddleware(compatibility.VerificationMiddlewareConfig{
					Verify: func(_ context.Context, request *http.Request) error {
						return accessBodyThroughGetBody(request)
					},
					ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
					Reject:      func(http.ResponseWriter, *http.Request, error) { t.Fatal("request rejected") },
				})
				if err != nil {
					t.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(http.StatusNoContent)
				})).ServeHTTP(recorder, request)
				if recorder.Code != http.StatusNoContent {
					t.Fatalf("status = %d", recorder.Code)
				}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := &observedBody{reader: strings.NewReader("body")}
			request, err := http.NewRequest(http.MethodPost, "https://example.com", body)
			if err != nil {
				t.Fatal(err)
			}
			request.GetBody = func() (io.ReadCloser, error) {
				return body, nil
			}
			test.run(t, request)
			if body.closed {
				t.Fatal("callback closed the caller body through GetBody")
			}
			content, err := io.ReadAll(body)
			if err != nil || string(content) != "body" {
				t.Fatalf("caller body after callback = %q, %v", content, err)
			}
		})
	}
}

func TestCompatibilityCallbacksCannotReachSharedRequestGraphs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*testing.T, *http.Request, func(context.Context, *http.Request) error)
	}{
		{
			name: "signing",
			run: func(t *testing.T, request *http.Request, callback func(context.Context, *http.Request) error) {
				t.Helper()
				adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
					Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
					}),
					Sign:        callback,
					ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := adapter.RoundTrip(request); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "verification",
			run: func(t *testing.T, request *http.Request, callback func(context.Context, *http.Request) error) {
				t.Helper()
				middleware, err := compatibility.NewCavageVerificationMiddleware(compatibility.VerificationMiddlewareConfig{
					Verify:      callback,
					ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
					Reject:      func(http.ResponseWriter, *http.Request, error) { t.Fatal("request rejected") },
				})
				if err != nil {
					t.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(http.StatusNoContent)
				})).ServeHTTP(recorder, request)
				if recorder.Code != http.StatusNoContent {
					t.Fatalf("status = %d", recorder.Code)
				}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := parsedMultipartRequest(t)
			request.TLS = &tls.ConnectionState{ServerName: "origin.example", OCSPResponse: []byte("ocsp")}
			request.Response = &http.Response{Request: request, Header: make(http.Header), Body: http.NoBody}
			sharedStateVisible := false
			test.run(t, request, func(_ context.Context, callbackRequest *http.Request) error {
				if callbackRequest.Form != nil || callbackRequest.PostForm != nil || callbackRequest.MultipartForm != nil {
					sharedStateVisible = true
				}
				if callbackRequest.MultipartForm != nil {
					_ = callbackRequest.MultipartForm.RemoveAll()
				}
				if callbackRequest.TLS != nil {
					sharedStateVisible = true
					callbackRequest.TLS.ServerName = "attacker.example"
					callbackRequest.TLS.OCSPResponse[0] = 'X'
				}
				if callbackRequest.Response != nil && callbackRequest.Response.Request != nil {
					sharedStateVisible = true
					callbackRequest.Response.Request.Method = http.MethodDelete
				}
				return nil
			})

			if sharedStateVisible {
				t.Fatal("callback received body-derived, TLS, or response request state")
			}
			if request.Method != http.MethodPost || request.TLS.ServerName != "origin.example" || string(request.TLS.OCSPResponse) != "ocsp" {
				t.Fatalf("original request graph = method %q, TLS server %q, OCSP %q", request.Method, request.TLS.ServerName, request.TLS.OCSPResponse)
			}
			file, _, err := request.FormFile("upload")
			if err != nil {
				t.Fatalf("original multipart file unavailable: %v", err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func parsedMultipartRequest(t *testing.T) *http.Request {
	t.Helper()

	var encoded strings.Builder
	writer := multipart.NewWriter(&encoded)
	file, err := writer.CreateFormFile("upload", "upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, strings.Repeat("x", 1024)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://origin.example/upload", strings.NewReader(encoded.String()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = request.MultipartForm.RemoveAll()
	})
	return request
}

func accessBodyThroughGetBody(request *http.Request) error {
	if request.GetBody == nil {
		return nil
	}
	body, err := request.GetBody()
	if err != nil {
		return err
	}
	if _, err := body.Read(make([]byte, 1)); err != nil {
		return err
	}
	return body.Close()
}

func TestSigningCallbackCanClearVendorFieldsWithoutClearingRFCFields(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	request.Header.Set("Authorization", "stale vendor value")
	request.Header["Signature"] = nil
	request.Trailer = http.Header{
		"Signature":      nil,
		"X-Legacy-Stale": []string{"stale vendor value"},
	}
	var delegated *http.Request
	adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			delegated = request
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
		}),
		Sign: func(_ context.Context, request *http.Request) error {
			request.Header = nil
			request.Trailer = nil
			return nil
		},
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := adapter.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if delegated.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization = %q", delegated.Header.Get("Authorization"))
	}
	values, present := delegated.Header["Signature"]
	if !present || values != nil {
		t.Fatalf("Signature entry = %#v, present = %v", values, present)
	}
	if delegated.Trailer.Get("X-Legacy-Stale") != "" {
		t.Fatalf("X-Legacy-Stale trailer = %q", delegated.Trailer.Get("X-Legacy-Stale"))
	}
	values, present = delegated.Trailer["Signature"]
	if !present || values != nil {
		t.Fatalf("Signature trailer entry = %#v, present = %v", values, present)
	}
}

func TestVendorAdapterRequiresAnExplicitSafeName(t *testing.T) {
	t.Parallel()

	config := compatibility.SigningRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Sign:        func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	}
	for _, name := range []string{"", strings.Repeat("a", 65), "AWS", "contains space", "contains/slash", "`", "{", "/", ":"} {
		if _, err := compatibility.NewVendorSigningRoundTripper(name, config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("NewVendorSigningRoundTripper(%q) error = %v", name, err)
		}
	}
	for _, name := range []string{"a", "z", "0", "9", "-", "_", ".", "acme-v2", strings.Repeat("a", 64)} {
		adapter, err := compatibility.NewVendorSigningRoundTripper(name, config)
		if err != nil || adapter.Protocol() != compatibility.Protocol("vendor:"+name) {
			t.Fatalf("vendor adapter %q = %#v, %v", name, adapter, err)
		}
	}
}

func TestCompatibilityAdaptersSanitizeCallbackFailures(t *testing.T) {
	t.Parallel()

	secretFailure := errors.New("credential=secret")
	var reported error
	adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }),
		Sign:      func(context.Context, *http.Request) error { return secretFailure },
		ReportError: func(_ context.Context, protocol compatibility.Protocol, operation compatibility.Operation, err error) {
			if protocol != compatibility.CavageDraft || operation != compatibility.OperationSign {
				t.Fatalf("reported boundary = %q, %q", protocol, operation)
			}
			reported = err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if response != nil || !errors.Is(err, compatibility.ErrSigning) || strings.Contains(err.Error(), "secret") || !errors.Is(reported, secretFailure) {
		t.Fatalf("sanitized result = %#v, %v; reported = %v", response, err, reported)
	}
}

func TestSigningCallbackFailureClosesCallerBody(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: strings.NewReader("body")}
	request, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport called")
			return nil, nil
		}),
		Sign:        func(context.Context, *http.Request) error { return errors.New("signer unavailable") },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.RoundTrip(request)
	if response != nil || !errors.Is(err, compatibility.ErrSigning) {
		t.Fatalf("RoundTrip() = %#v, %v", response, err)
	}
	if !body.closed {
		t.Fatal("request body remained open after signing failure")
	}
}

func TestExplicitLegacyVerificationMiddlewareRejectsBeforeApplication(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		new  func(compatibility.VerificationMiddlewareConfig) (compatibility.VerificationMiddleware, error)
		want compatibility.Protocol
	}{
		{"cavage", compatibility.NewCavageVerificationMiddleware, compatibility.CavageDraft},
		{"aws", compatibility.NewAWSSigV4VerificationMiddleware, compatibility.AWSSigV4},
		{"oauth", compatibility.NewOAuth1VerificationMiddleware, compatibility.OAuth1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			called := false
			var reported error
			middleware, err := test.new(compatibility.VerificationMiddlewareConfig{
				Verify: func(context.Context, *http.Request) error { return errors.New("key=secret") },
				ReportError: func(_ context.Context, protocol compatibility.Protocol, operation compatibility.Operation, err error) {
					if protocol != test.want || operation != compatibility.OperationVerify {
						t.Fatalf("reported boundary = %q, %q", protocol, operation)
					}
					reported = err
				},
				Reject: func(writer http.ResponseWriter, _ *http.Request, err error) {
					if !errors.Is(err, compatibility.ErrVerification) || strings.Contains(err.Error(), "secret") {
						t.Fatalf("unsafe rejection error = %v", err)
					}
					http.Error(writer, "unauthorized", http.StatusUnauthorized)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, "https://example.com", nil),
			)
			if called || recorder.Code != http.StatusUnauthorized || reported == nil {
				t.Fatalf("called = %v, status = %d, reported = %v", called, recorder.Code, reported)
			}
		})
	}
}

func TestVerificationCallbackCannotContaminateRFCRequestIdentity(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: strings.NewReader("body")}
	request, err := http.NewRequest(http.MethodPost, "https://origin.example/resource?one=1", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "origin.example"
	request.RequestURI = "/resource?one=1"
	request.Header.Set("Signature-Input", `rfc=("@method");keyid="rfc"`)
	request.Header.Set("Signature", "rfc=:AQID:")
	request.Header.Set("Accept-Signature", `rfc=("@method")`)

	middleware, err := compatibility.NewCavageVerificationMiddleware(compatibility.VerificationMiddlewareConfig{
		Verify: func(_ context.Context, request *http.Request) error {
			request.Method = http.MethodDelete
			request.URL.Scheme = "http"
			request.URL.Host = "attacker.example"
			request.URL.Path = "/other"
			request.URL.RawQuery = "two=2"
			request.Host = "attacker.example"
			request.RequestURI = "/other?two=2"
			if err := request.Body.Close(); err != nil {
				return err
			}
			request.Body = http.NoBody
			request.Header.Set("Signature-Input", `legacy=("@method")`)
			request.Header.Set("Signature", "legacy=:c2ln:")
			request.Header.Del("Accept-Signature")
			return nil
		},
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
		Reject:      func(http.ResponseWriter, *http.Request, error) { t.Fatal("request rejected") },
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, downstream *http.Request) {
		if downstream != request {
			t.Fatal("middleware replaced the application request")
		}
		if downstream.Method != http.MethodPost || downstream.URL.String() != "https://origin.example/resource?one=1" || downstream.Host != "origin.example" || downstream.RequestURI != "/resource?one=1" {
			t.Fatalf("downstream request identity = %s %s, host %q, request URI %q", downstream.Method, downstream.URL, downstream.Host, downstream.RequestURI)
		}
		if downstream.Body != body || body.closed {
			t.Fatalf("downstream body = %#v, original closed = %v", downstream.Body, body.closed)
		}
		if downstream.Header.Get("Signature-Input") != `rfc=("@method");keyid="rfc"` || downstream.Header.Get("Signature") != "rfc=:AQID:" || downstream.Header.Get("Accept-Signature") != `rfc=("@method")` {
			t.Fatalf("downstream RFC fields = %v", downstream.Header)
		}
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestVerificationCallbackCannotNormalizeArbitraryCoveredFields(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	request.Header.Set("Signature-Input", `rfc=("@method")`)
	request.Header.Set("X-Covered", "raw")
	request.Header.Set("Content-Digest", "sha-256=:cmF3:")
	request.Header.Set("Authorization", "raw")
	request.Trailer = http.Header{
		"Signature": []string{"rfc=:AQID:"},
		"X-Covered": []string{"raw"},
	}
	middleware, err := compatibility.NewCavageVerificationMiddleware(compatibility.VerificationMiddlewareConfig{
		Verify: func(_ context.Context, request *http.Request) error {
			request.Header.Set("X-Covered", "normalized")
			request.Header.Set("Content-Digest", "sha-256=:bm9ybWFsaXplZA==:")
			request.Header.Set("Authorization", "normalized")
			request.Header.Set("Signature-Input", `legacy=("@method")`)
			request.Trailer.Set("X-Covered", "normalized")
			request.Trailer.Set("Signature", "legacy=:c2ln:")
			return nil
		},
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
		Reject:      func(http.ResponseWriter, *http.Request, error) { t.Fatal("request rejected") },
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, downstream *http.Request) {
		if downstream.Header.Get("X-Covered") != "raw" || downstream.Header.Get("Content-Digest") != "sha-256=:cmF3:" || downstream.Header.Get("Authorization") != "raw" || downstream.Trailer.Get("X-Covered") != "raw" {
			t.Fatalf("covered fields = %q, %q, %q, trailer %q", downstream.Header.Get("X-Covered"), downstream.Header.Get("Content-Digest"), downstream.Header.Get("Authorization"), downstream.Trailer.Get("X-Covered"))
		}
		if downstream.Header.Get("Signature-Input") != `rfc=("@method")` || downstream.Trailer.Get("Signature") != "rfc=:AQID:" {
			t.Fatalf("RFC fields = header %q, trailer %q", downstream.Header.Get("Signature-Input"), downstream.Trailer.Get("Signature"))
		}
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestCompatibilityAdapterConfigurationAndNilBoundaries(t *testing.T) {
	t.Parallel()
	var nilAdapter *compatibility.SigningRoundTripper
	if nilAdapter.Protocol() != "" {
		t.Fatal("nil adapter reported a protocol")
	}

	validSigning := compatibility.SigningRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Sign:        func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	}
	for _, config := range []compatibility.SigningRoundTripperConfig{
		{},
		{Sign: validSigning.Sign, ReportError: validSigning.ReportError},
		{Transport: validSigning.Transport, ReportError: validSigning.ReportError},
		{Transport: validSigning.Transport, Sign: validSigning.Sign},
	} {
		if _, err := compatibility.NewCavageSigningRoundTripper(config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("invalid signing config error = %v", err)
		}
	}
	adapter, err := compatibility.NewCavageSigningRoundTripper(validSigning)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := adapter.RoundTrip(nil); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("nil request result = %#v, %v", response, err)
	}
	var absent *compatibility.SigningRoundTripper
	if response, err := absent.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("nil adapter result = %#v, %v", response, err)
	}
	if response, err := new(compatibility.SigningRoundTripper).RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("zero adapter result = %#v, %v", response, err)
	}

	validVerification := compatibility.VerificationMiddlewareConfig{
		Verify:      func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
		Reject:      func(http.ResponseWriter, *http.Request, error) {},
	}
	if _, err := compatibility.NewVendorVerificationMiddleware("", validVerification); !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("invalid vendor verification error = %v", err)
	}
	for _, config := range []compatibility.VerificationMiddlewareConfig{
		{},
		{ReportError: validVerification.ReportError, Reject: validVerification.Reject},
		{Verify: validVerification.Verify, Reject: validVerification.Reject},
		{Verify: validVerification.Verify, ReportError: validVerification.ReportError},
	} {
		if _, err := compatibility.NewCavageVerificationMiddleware(config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("invalid verification config error = %v", err)
		}
	}
	middleware, err := compatibility.NewCavageVerificationMiddleware(validVerification)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("nil next status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("nil request delegated") })).ServeHTTP(recorder, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("nil request status = %d", recorder.Code)
	}

	vendorMiddleware, err := compatibility.NewVendorVerificationMiddleware("acme-v2", validVerification)
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	vendorMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "https://example.com", io.NopCloser(strings.NewReader("body"))),
	)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("vendor middleware status = %d", recorder.Code)
	}
}
