package httpsignature

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type responseSigningCoverageWriter struct {
	header     http.Header
	body       bytes.Buffer
	status     int
	writeCalls int
}

func newResponseSigningCoverageWriter() *responseSigningCoverageWriter {
	return &responseSigningCoverageWriter{header: make(http.Header)}
}

func (writer *responseSigningCoverageWriter) Header() http.Header {
	return writer.header
}

func (writer *responseSigningCoverageWriter) WriteHeader(status int) {
	writer.status = status
}

func (writer *responseSigningCoverageWriter) Write(content []byte) (int, error) {
	writer.writeCalls++
	return writer.body.Write(content)
}

type responseSigningCoverageState struct {
	mapped              error
	optionCalls         int
	signedStatus        int
	signedContentLength int64
}

func newResponseSigningCoverageMiddleware(t *testing.T, now time.Time, key HMACKey) (ResponseSigningMiddleware, *responseSigningCoverageState) {
	t.Helper()

	state := &responseSigningCoverageState{}
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer:           NewSigner(testResponseSigningProfile(t, now, key)),
		Label:            "response",
		Existing:         ExistingSignaturesReject,
		MaxBufferedBytes: 32,
		Options: func(_ context.Context, _ *http.Request, response *http.Response) (SigningOptions, error) {
			state.optionCalls++
			state.signedStatus = response.StatusCode
			state.signedContentLength = response.ContentLength
			return SigningOptions{}, nil
		},
		MapError: func(_ http.ResponseWriter, _ *http.Request, err error) {
			state.mapped = err
		},
		ReportError: func(*http.Request, error) {},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	return middleware, state
}

func TestResponseSigningRejectsCaseCollidingOuterFramingBeforeHandler(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	middleware, state := newResponseSigningCoverageMiddleware(t, now, key)
	writer := newResponseSigningCoverageWriter()
	writer.header["Content-Length"] = []string{"0"}
	writer.header["content-length"] = []string{"0"}
	handlerCalls := 0

	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerCalls++
	})).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))

	if !errors.Is(state.mapped, ErrAmbiguousProtectedField) {
		t.Fatalf("mapped error = %v, want ErrAmbiguousProtectedField", state.mapped)
	}
	if handlerCalls != 0 || state.optionCalls != 0 {
		t.Fatalf("handler calls = %d, option calls = %d, want both zero", handlerCalls, state.optionCalls)
	}
	if writer.status != 0 || writer.writeCalls != 0 || writer.body.Len() != 0 {
		t.Fatalf("emitted response status = %d, write calls = %d, body = %q", writer.status, writer.writeCalls, writer.body.Bytes())
	}
}

func TestResponseSigningSignsBodylessResponseWithoutContentLength(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	middleware, state := newResponseSigningCoverageMiddleware(t, now, key)
	writer := newResponseSigningCoverageWriter()
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)

	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(writer, request)

	if state.mapped != nil {
		t.Fatalf("mapped error = %v", state.mapped)
	}
	if state.optionCalls != 1 || state.signedStatus != http.StatusNoContent || state.signedContentLength != 0 {
		t.Fatalf("option calls = %d, signed status = %d, signed content length = %d", state.optionCalls, state.signedStatus, state.signedContentLength)
	}
	if writer.status != http.StatusNoContent || writer.body.Len() != 0 || writer.header.Get("Content-Length") != "" {
		t.Fatalf("emitted status = %d, body = %q, Content-Length = %q", writer.status, writer.body.Bytes(), writer.header.Get("Content-Length"))
	}
	inputs, err := ParseSignatureInputs(writer.header.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(writer.header.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	response := &http.Response{
		StatusCode:    writer.status,
		Header:        writer.header.Clone(),
		Body:          http.NoBody,
		ContentLength: 0,
		Request:       request,
	}
	if _, err := NewVerifier(testResponseVerificationProfile(t, now, key)).Verify(
		context.Background(),
		MessageContext{Response: response, RelatedRequest: request},
		"response",
		inputs,
		signatures,
	); err != nil {
		t.Fatalf("Verify(bodyless response) error = %v", err)
	}
}

func TestResponseSigningRejectsContentLengthOnBodylessResponse(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		header  http.Header
		wantErr error
	}{
		{
			name:    "present",
			header:  http.Header{"Content-Length": []string{"0"}},
			wantErr: ErrInvalidHTTPIntegration,
		},
		{
			name: "case collision",
			header: http.Header{
				"Content-Length": []string{"0"},
				"content-length": []string{"0"},
			},
			wantErr: ErrAmbiguousProtectedField,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			middleware, state := newResponseSigningCoverageMiddleware(t, now, key)
			writer := newResponseSigningCoverageWriter()
			handlerCalls := 0
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				handlerCalls++
				for name, values := range test.header {
					writer.Header()[name] = append([]string(nil), values...)
				}
				writer.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))

			if !errors.Is(state.mapped, test.wantErr) {
				t.Fatalf("mapped error = %v, want %v", state.mapped, test.wantErr)
			}
			if handlerCalls != 1 || state.optionCalls != 0 {
				t.Fatalf("handler calls = %d, option calls = %d", handlerCalls, state.optionCalls)
			}
			if writer.status != 0 || writer.writeCalls != 0 || writer.body.Len() != 0 {
				t.Fatalf("emitted response status = %d, write calls = %d, body = %q", writer.status, writer.writeCalls, writer.body.Bytes())
			}
		})
	}
}

func TestResponseSigningRejectsInvalidRepresentationContentLength(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		header  http.Header
		wantErr error
	}{
		{name: "empty field value", header: http.Header{"Content-Length": []string{""}}, wantErr: ErrInvalidHTTPIntegration},
		{name: "multiple field values", header: http.Header{"Content-Length": []string{"1", "2"}}, wantErr: ErrInvalidHTTPIntegration},
		{name: "non decimal", header: http.Header{"Content-Length": []string{"12x"}}, wantErr: ErrInvalidHTTPIntegration},
		{name: "integer overflow", header: http.Header{"Content-Length": []string{"9223372036854775808"}}, wantErr: ErrInvalidHTTPIntegration},
		{
			name: "case collision",
			header: http.Header{
				"Content-Length": []string{"1"},
				"content-length": []string{"1"},
			},
			wantErr: ErrAmbiguousProtectedField,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			middleware, state := newResponseSigningCoverageMiddleware(t, now, key)
			writer := newResponseSigningCoverageWriter()
			handlerCalls := 0
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				handlerCalls++
				for name, values := range test.header {
					writer.Header()[name] = append([]string(nil), values...)
				}
				writer.WriteHeader(http.StatusOK)
			})).ServeHTTP(writer, httptest.NewRequest(http.MethodHead, "https://example.com/data", nil))

			if !errors.Is(state.mapped, test.wantErr) {
				t.Fatalf("mapped error = %v, want %v", state.mapped, test.wantErr)
			}
			if handlerCalls != 1 || state.optionCalls != 0 {
				t.Fatalf("handler calls = %d, option calls = %d", handlerCalls, state.optionCalls)
			}
			if writer.status != 0 || writer.writeCalls != 0 || writer.body.Len() != 0 {
				t.Fatalf("emitted response status = %d, write calls = %d, body = %q", writer.status, writer.writeCalls, writer.body.Bytes())
			}
		})
	}
}
