package httpsignature

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dunglas/httpsfv"
)

func TestSignatureBaseContentLengthMatchesBodylessRequestWire(t *testing.T) {
	t.Parallel()

	input := SignatureInput{Components: []ComponentIdentifier{{Name: "content-length"}}}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			request, err := http.NewRequest(method, "https://example.test/", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			var wire bytes.Buffer
			if err := request.Write(&wire); err != nil {
				t.Fatalf("Request.Write() error = %v", err)
			}
			if !strings.Contains(wire.String(), "Content-Length: 0\r\n") {
				t.Fatalf("wire request = %q, want Content-Length: 0", wire.String())
			}
			base, err := CreateSignatureBase(MessageContext{Request: request}, input)
			if err != nil || !strings.HasPrefix(base, "\"content-length\": 0\n") {
				t.Fatalf("CreateSignatureBase() = %q, %v, want wire Content-Length 0", base, err)
			}
		})
	}

	for _, method := range []string{http.MethodDelete, http.MethodOptions, "CUSTOM"} {
		method := method
		t.Run(method+" absent", func(t *testing.T) {
			t.Parallel()
			request, err := http.NewRequest(method, "https://example.test/", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			var wire bytes.Buffer
			if err := request.Write(&wire); err != nil {
				t.Fatalf("Request.Write() error = %v", err)
			}
			if strings.Contains(wire.String(), "Content-Length:") {
				t.Fatalf("wire request = %q, want no Content-Length", wire.String())
			}
			if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
				t.Fatalf("CreateSignatureBase() error = %v, want absent covered field", err)
			}
		})

		t.Run(method+" identity", func(t *testing.T) {
			t.Parallel()
			request, err := http.NewRequest(method, "https://example.test/", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			request.TransferEncoding = []string{"identity"}
			var wire bytes.Buffer
			if err := request.Write(&wire); err != nil {
				t.Fatalf("Request.Write() error = %v", err)
			}
			if !strings.Contains(wire.String(), "Content-Length: 0\r\n") {
				t.Fatalf("wire request = %q, want identity Content-Length: 0", wire.String())
			}
			base, err := CreateSignatureBase(MessageContext{Request: request}, input)
			if err != nil || !strings.HasPrefix(base, "\"content-length\": 0\n") {
				t.Fatalf("CreateSignatureBase() = %q, %v, want identity Content-Length 0", base, err)
			}
		})
	}
}

func TestSignatureBaseMatchesRequestWriteForCallerTransferEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		body             io.ReadCloser
		contentLength    int64
		transferEncoding []string
		wireLength       string
		wireChunked      bool
	}{
		{name: "lowercase chunked", method: http.MethodPost, body: http.NoBody, transferEncoding: []string{"chunked"}, wireChunked: true},
		{name: "mixed case chunked", method: http.MethodPost, body: http.NoBody, transferEncoding: []string{"Chunked"}, wireLength: "0"},
		{name: "nil body clears chunked", method: http.MethodPost, transferEncoding: []string{"chunked"}, wireLength: "0"},
		{name: "exact identity", method: http.MethodDelete, body: http.NoBody, transferEncoding: []string{"identity"}, wireLength: "0"},
		{name: "mixed case identity", method: http.MethodDelete, body: http.NoBody, transferEncoding: []string{"Identity"}},
		{name: "unsupported", method: http.MethodPost, body: http.NoBody, transferEncoding: []string{"gzip"}, wireLength: "0"},
		{name: "chunked not first", method: http.MethodPost, body: http.NoBody, transferEncoding: []string{"gzip", "chunked"}, wireLength: "0"},
		{name: "chunked first with suffix", method: http.MethodPost, body: http.NoBody, transferEncoding: []string{"chunked", "gzip"}, wireChunked: true},
		{
			name: "known body with mixed case chunked", method: http.MethodPost,
			body: io.NopCloser(strings.NewReader("abc")), contentLength: 3,
			transferEncoding: []string{"Chunked"}, wireLength: "3",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{
				Method: test.method,
				URL:    &url.URL{Scheme: "https", Host: "example.test", Path: "/"},
				Host:   "example.test", Header: make(http.Header), Body: test.body,
				ContentLength: test.contentLength, TransferEncoding: append([]string(nil), test.transferEncoding...),
			}
			var wire bytes.Buffer
			if err := request.Write(&wire); err != nil {
				t.Fatalf("Request.Write() error = %v", err)
			}
			wireValue := wire.String()
			lengthLine := "Content-Length: " + test.wireLength + "\r\n"
			if got := test.wireLength != "" && strings.Contains(wireValue, lengthLine); got != (test.wireLength != "") {
				t.Fatalf("wire request = %q, Content-Length presence = %t", wireValue, got)
			}
			if test.wireLength == "" && strings.Contains(wireValue, "Content-Length:") {
				t.Fatalf("wire request = %q, want no Content-Length", wireValue)
			}
			if got := strings.Contains(wireValue, "Transfer-Encoding: chunked\r\n"); got != test.wireChunked {
				t.Fatalf("wire request = %q, chunked = %t, want %t", wireValue, got, test.wireChunked)
			}

			lengthBase, lengthErr := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "content-length"}},
			})
			if test.wireLength == "" {
				if !errors.Is(lengthErr, ErrSignatureBase) {
					t.Fatalf("content-length base = %q, %v, want absent component", lengthBase, lengthErr)
				}
			} else if lengthErr != nil || !strings.HasPrefix(lengthBase, "\"content-length\": "+test.wireLength+"\n") {
				t.Fatalf("content-length base = %q, %v, want wire value %s", lengthBase, lengthErr, test.wireLength)
			}

			transferBase, transferErr := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "transfer-encoding"}},
			})
			if !test.wireChunked {
				if !errors.Is(transferErr, ErrSignatureBase) {
					t.Fatalf("transfer-encoding base = %q, %v, want absent component", transferBase, transferErr)
				}
			} else if transferErr != nil || !strings.HasPrefix(transferBase, "\"transfer-encoding\": chunked\n") {
				t.Fatalf("transfer-encoding base = %q, %v, want wire chunked", transferBase, transferErr)
			}
		})
	}
}

func TestSignatureBaseTrailerMatchesRequestWriteTransferSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		body             io.ReadCloser
		transferEncoding []string
		wireTrailer      bool
	}{
		{name: "lowercase chunked", body: http.NoBody, transferEncoding: []string{"chunked"}, wireTrailer: true},
		{name: "mixed case chunked", body: http.NoBody, transferEncoding: []string{"Chunked"}},
		{name: "nil body clears chunked", transferEncoding: []string{"chunked"}},
		{name: "chunked not first", body: http.NoBody, transferEncoding: []string{"gzip", "chunked"}},
		{name: "chunked first with suffix", body: http.NoBody, transferEncoding: []string{"chunked", "gzip"}, wireTrailer: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{
				Method: http.MethodPost,
				URL:    &url.URL{Scheme: "https", Host: "example.test", Path: "/"},
				Host:   "example.test", Header: make(http.Header), Body: test.body,
				TransferEncoding: append([]string(nil), test.transferEncoding...),
				Trailer:          http.Header{"X-Final": []string{"done"}},
			}
			var wire bytes.Buffer
			if err := request.Write(&wire); err != nil {
				t.Fatalf("Request.Write() error = %v", err)
			}
			if got := strings.Contains(wire.String(), "Trailer: X-Final\r\n"); got != test.wireTrailer {
				t.Fatalf("wire request = %q, Trailer = %t, want %t", wire.String(), got, test.wireTrailer)
			}
			base, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "trailer"}},
			})
			if !test.wireTrailer {
				if !errors.Is(err, ErrSignatureBase) {
					t.Fatalf("trailer base = %q, %v, want absent component", base, err)
				}
			} else if err != nil || !strings.HasPrefix(base, "\"trailer\": X-Final\n") {
				t.Fatalf("trailer base = %q, %v, want wire declaration", base, err)
			}
		})
	}
}

func TestSignatureBaseMatchesResponseWriteFraming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		status           int
		body             io.ReadCloser
		contentLength    int64
		transferEncoding []string
		wireLength       string
		wireChunked      bool
		wireBody         string
		headerLength     string
	}{
		{name: "known length", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, wireLength: "3"},
		{name: "exact chunked", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, transferEncoding: []string{"chunked"}, wireChunked: true},
		{name: "chunked suffix", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, transferEncoding: []string{"chunked", "gzip"}, wireChunked: true},
		{name: "mixed case chunked", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, transferEncoding: []string{"Chunked"}, wireLength: "3"},
		{name: "unsupported", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, transferEncoding: []string{"gzip"}, wireLength: "3"},
		{name: "chunked not first", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, transferEncoding: []string{"gzip", "chunked"}, wireLength: "3"},
		{name: "unknown body length", method: http.MethodGet, status: http.StatusOK, body: io.NopCloser(strings.NewReader("abc")), contentLength: -1},
		{name: "nil body clears known length", method: http.MethodGet, status: http.StatusOK, contentLength: 3},
		{name: "nil body clears chunked", method: http.MethodGet, status: http.StatusOK, contentLength: 3, transferEncoding: []string{"chunked"}},
		{name: "head preserves known length", method: http.MethodHead, status: http.StatusOK, contentLength: 3, wireLength: "3"},
		{name: "head mixed chunked preserves length", method: http.MethodHead, status: http.StatusOK, contentLength: 3, transferEncoding: []string{"Chunked"}, wireLength: "3"},
		{name: "head exact chunked", method: http.MethodHead, status: http.StatusOK, contentLength: 3, transferEncoding: []string{"chunked"}, wireChunked: true},
		{name: "zero GET", method: http.MethodGet, status: http.StatusOK, wireLength: "0", headerLength: "999"},
		{name: "zero POST", method: http.MethodPost, status: http.StatusOK, wireLength: "0"},
		{name: "zero HEAD", method: http.MethodHead, status: http.StatusOK, wireLength: "0"},
		{name: "zero no-content", method: http.MethodGet, status: http.StatusNoContent, headerLength: "999"},
		{name: "reset content Go wire divergence", method: http.MethodGet, status: http.StatusResetContent, body: io.NopCloser(strings.NewReader("abc")), contentLength: 3, wireLength: "3", wireBody: "abc"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			header := make(http.Header)
			if test.headerLength != "" {
				header["Content-Length"] = []string{test.headerLength}
			}
			response := &http.Response{
				StatusCode: test.status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
				Header: header, Body: test.body, ContentLength: test.contentLength,
				TransferEncoding: append([]string(nil), test.transferEncoding...),
				Request:          &http.Request{Method: test.method, RequestURI: "/"},
			}
			var wire bytes.Buffer
			if err := response.Write(&wire); err != nil {
				t.Fatalf("Response.Write() error = %v", err)
			}
			wireValue := wire.String()
			if test.wireLength == "" {
				if strings.Contains(wireValue, "Content-Length:") {
					t.Fatalf("wire response = %q, want no Content-Length", wireValue)
				}
			} else if !strings.Contains(wireValue, "Content-Length: "+test.wireLength+"\r\n") {
				t.Fatalf("wire response = %q, want Content-Length %s", wireValue, test.wireLength)
			}
			if got := strings.Contains(wireValue, "Transfer-Encoding: chunked\r\n"); got != test.wireChunked {
				t.Fatalf("wire response = %q, chunked = %t, want %t", wireValue, got, test.wireChunked)
			}
			if test.wireBody != "" && !strings.HasSuffix(wireValue, "\r\n\r\n"+test.wireBody) {
				t.Fatalf("wire response = %q, want body %q", wireValue, test.wireBody)
			}

			lengthBase, lengthErr := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "content-length"}},
			})
			if test.wireLength == "" {
				if !errors.Is(lengthErr, ErrSignatureBase) {
					t.Fatalf("content-length base = %q, %v, want absent component", lengthBase, lengthErr)
				}
			} else if lengthErr != nil || !strings.HasPrefix(lengthBase, "\"content-length\": "+test.wireLength+"\n") {
				t.Fatalf("content-length base = %q, %v, want wire value %s", lengthBase, lengthErr, test.wireLength)
			}

			transferBase, transferErr := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "transfer-encoding"}},
			})
			if !test.wireChunked {
				if !errors.Is(transferErr, ErrSignatureBase) {
					t.Fatalf("transfer-encoding base = %q, %v, want absent component", transferBase, transferErr)
				}
			} else if transferErr != nil || !strings.HasPrefix(transferBase, "\"transfer-encoding\": chunked\n") {
				t.Fatalf("transfer-encoding base = %q, %v, want wire chunked", transferBase, transferErr)
			}
		})
	}
}

func TestSignatureBaseMatchesResponseWriteWithExplicitTransportMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		withRequest      bool
		body             string
		noBody           bool
		contentLength    int64
		transferEncoding []string
		wireLength       string
		wireChunked      bool
	}{
		{name: "requestless nil body positive length", contentLength: 3},
		{name: "requestless nil body zero length", wireLength: "0"},
		{name: "requestless NoBody zero length", noBody: true, wireLength: "0"},
		{name: "requestless known body", body: "abc", contentLength: 3, wireLength: "3"},
		{name: "client request nil body positive length", withRequest: true, contentLength: 3},
		{name: "client request nil body zero length", withRequest: true, wireLength: "0"},
		{name: "client request known body", withRequest: true, body: "abc", contentLength: 3, wireLength: "3"},
		{name: "client request nil body chunked", withRequest: true, contentLength: 3, transferEncoding: []string{"chunked"}},
		{name: "client request chunked body", withRequest: true, body: "abc", contentLength: 3, transferEncoding: []string{"chunked"}, wireChunked: true},
		{name: "requestless chunked body", body: "abc", contentLength: 3, transferEncoding: []string{"chunked"}, wireChunked: true},
		{name: "client request mixed-case chunked", withRequest: true, body: "abc", contentLength: 3, transferEncoding: []string{"Chunked"}, wireLength: "3"},
		{name: "client request identity DELETE", method: http.MethodDelete, withRequest: true, noBody: true, transferEncoding: []string{"identity"}, wireLength: "0"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			newResponse := func() *http.Response {
				var body io.ReadCloser
				switch {
				case test.noBody:
					body = http.NoBody
				case test.body != "":
					body = io.NopCloser(strings.NewReader(test.body))
				}
				var request *http.Request
				if test.withRequest {
					method := test.method
					if method == "" {
						method = http.MethodGet
					}
					request, _ = http.NewRequest(method, "https://example.test/", nil)
				}
				return &http.Response{
					StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
					Header: http.Header{"Content-Length": []string{"999"}}, Body: body,
					ContentLength: test.contentLength, TransferEncoding: append([]string(nil), test.transferEncoding...),
					Request: request,
				}
			}
			var wire bytes.Buffer
			if err := newResponse().Write(&wire); err != nil {
				t.Fatalf("Response.Write() error = %v", err)
			}
			wireValue := wire.String()
			if test.wireLength == "" {
				if strings.Contains(wireValue, "Content-Length:") {
					t.Fatalf("wire response = %q, want no Content-Length", wireValue)
				}
			} else if !strings.Contains(wireValue, "Content-Length: "+test.wireLength+"\r\n") {
				t.Fatalf("wire response = %q, want Content-Length %s", wireValue, test.wireLength)
			}
			if got := strings.Contains(wireValue, "Transfer-Encoding: chunked\r\n"); got != test.wireChunked {
				t.Fatalf("wire response = %q, chunked = %t, want %t", wireValue, got, test.wireChunked)
			}

			lengthBase, lengthErr := CreateSignatureBase(MessageContext{Response: newResponse(), ResponseTransport: ResponseTransportWrite}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "content-length"}},
			})
			if test.wireLength == "" {
				if !errors.Is(lengthErr, ErrSignatureBase) {
					t.Fatalf("content-length base = %q, %v, want absent component", lengthBase, lengthErr)
				}
			} else if lengthErr != nil || !strings.HasPrefix(lengthBase, "\"content-length\": "+test.wireLength+"\n") {
				t.Fatalf("content-length base = %q, %v, want wire value %s", lengthBase, lengthErr, test.wireLength)
			}

			transferBase, transferErr := CreateSignatureBase(MessageContext{Response: newResponse(), ResponseTransport: ResponseTransportWrite}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "transfer-encoding"}},
			})
			if !test.wireChunked {
				if !errors.Is(transferErr, ErrSignatureBase) {
					t.Fatalf("transfer-encoding base = %q, %v, want absent component", transferBase, transferErr)
				}
			} else if transferErr != nil || !strings.HasPrefix(transferBase, "\"transfer-encoding\": chunked\n") {
				t.Fatalf("transfer-encoding base = %q, %v, want wire chunked", transferBase, transferErr)
			}
		})
	}
}

func TestSignatureBaseMatchesResponseWriteImplicitConnectionClose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		protoMajor       int
		protoMinor       int
		contentLength    int64
		transferEncoding []string
		uncompressed     bool
		wireClose        bool
	}{
		{name: "unknown HTTP/1.1 length", protoMajor: 1, protoMinor: 1, contentLength: -1, wireClose: true},
		{name: "uncompressed", protoMajor: 1, protoMinor: 1, contentLength: -1, uncompressed: true},
		{name: "chunked", protoMajor: 1, protoMinor: 1, contentLength: -1, transferEncoding: []string{"chunked"}},
		{name: "HTTP/1.0", protoMajor: 1, contentLength: -1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			newResponse := func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK, ProtoMajor: test.protoMajor, ProtoMinor: test.protoMinor,
					Header: make(http.Header), Body: io.NopCloser(strings.NewReader("body")),
					ContentLength: test.contentLength, TransferEncoding: append([]string(nil), test.transferEncoding...),
					Uncompressed: test.uncompressed,
				}
			}

			var wire bytes.Buffer
			if err := newResponse().Write(&wire); err != nil {
				t.Fatalf("Response.Write() error = %v", err)
			}
			if got := strings.Contains(wire.String(), "Connection: close\r\n"); got != test.wireClose {
				t.Fatalf("wire response = %q, connection close = %t, want %t", wire.String(), got, test.wireClose)
			}

			base, err := CreateSignatureBase(MessageContext{
				Response: newResponse(), ResponseTransport: ResponseTransportWrite,
			}, SignatureInput{Components: []ComponentIdentifier{{Name: "connection"}}})
			if !test.wireClose {
				if !errors.Is(err, ErrSignatureBase) {
					t.Fatalf("connection base = %q, %v, want absent component", base, err)
				}
				return
			}
			if err != nil || !strings.HasPrefix(base, "\"connection\": close\n") {
				t.Fatalf("connection base = %q, %v, want wire close", base, err)
			}
		})
	}
}

func TestSignatureBaseRejectsResponseWriteProbeDependentContentLength(t *testing.T) {
	t.Parallel()

	response := &http.Response{
		StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), ContentLength: 0,
		Request: &http.Request{Method: http.MethodGet, RequestURI: "/"},
	}
	if _, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "content-length"}},
	}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("probe-dependent content-length error = %v, want ErrSignatureBase", err)
	}
}

func TestSignatureBaseRejectsResponseWriteKnownEmptyBodyLength(t *testing.T) {
	t.Parallel()

	response := &http.Response{
		StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: http.NoBody, ContentLength: 1,
		Request: &http.Request{Method: http.MethodGet},
	}
	var wire bytes.Buffer
	if err := response.Write(&wire); err == nil {
		t.Fatal("Response.Write() succeeded with a known-empty body and positive content length")
	}
	if _, err := CreateSignatureBase(MessageContext{
		Response: response, ResponseTransport: ResponseTransportWrite,
	}, SignatureInput{Components: []ComponentIdentifier{{Name: "content-length"}}}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("known-empty body content-length error = %v, want ErrSignatureBase", err)
	}
}

func TestSignatureBaseDistinguishesReceivedZeroContentLengthFromAbsence(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "content-length"}}}
	for _, test := range []struct {
		name string
		wire string
		want bool
	}{
		{name: "explicit zero", wire: "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n", want: true},
		{name: "absent", wire: "HTTP/1.1 204 No Content\r\n\r\n"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response, readErr := http.ReadResponse(bufio.NewReader(strings.NewReader(test.wire)), request)
			if readErr != nil {
				t.Fatal(readErr)
			}
			defer func() { _ = response.Body.Close() }()
			if response.ContentLength != 0 {
				t.Fatalf("received ContentLength = %d, want the shared net/http zero representation", response.ContentLength)
			}

			base, baseErr := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportReceived}, input)
			if !test.want {
				if !errors.Is(baseErr, ErrSignatureBase) {
					t.Fatalf("absent content-length base = %q, %v, want absent component", base, baseErr)
				}
				return
			}
			if baseErr != nil || !strings.HasPrefix(base, "\"content-length\": 0\n") {
				t.Fatalf("explicit zero content-length base = %q, %v", base, baseErr)
			}
		})
	}
}

func TestSignatureBaseRequiresResponseTransportModeForAmbiguousFields(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	received, err := http.ReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n",
	)), request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = received.Body.Close() }()
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "content-length"}}}
	if _, err := CreateSignatureBase(MessageContext{Response: received}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("unspecified received mode error = %v, want ErrSignatureBase", err)
	}
	if base, err := CreateSignatureBase(MessageContext{
		Response: received, ResponseTransport: ResponseTransportReceived,
	}, input); err != nil || !strings.HasPrefix(base, "\"content-length\": 0\n") {
		t.Fatalf("received mode base = %q, %v", base, err)
	}
	if _, err := CreateSignatureBase(MessageContext{
		Response: received, ResponseTransport: ResponseTransportWrite,
	}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("write mode for received response error = %v, want ErrSignatureBase", err)
	}

	outgoing := &http.Response{
		StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Length": []string{"999"}}, Body: io.NopCloser(strings.NewReader("abc")),
		ContentLength: 3, Request: request,
	}
	if _, err := CreateSignatureBase(MessageContext{
		Response: outgoing, ResponseTransport: ResponseTransportReceived,
	}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("received mode for inconsistent response error = %v, want ErrSignatureBase", err)
	}
	if base, err := CreateSignatureBase(MessageContext{
		Response: outgoing, ResponseTransport: ResponseTransportWrite,
	}, input); err != nil || !strings.HasPrefix(base, "\"content-length\": 3\n") {
		t.Fatalf("write mode base = %q, %v", base, err)
	}
	if _, err := CreateSignatureBase(MessageContext{
		Response: outgoing, ResponseTransport: ResponseTransportMode(255),
	}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("invalid response mode error = %v, want ErrSignatureBase", err)
	}
	if _, err := CreateSignatureBase(MessageContext{
		Request: request, ResponseTransport: ResponseTransportReceived,
	}, SignatureInput{}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("request response mode error = %v, want ErrSignatureBase", err)
	}
}

func TestSignatureBasePreservesReceivedHTTP10ConnectionFields(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"close", "keep-alive"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			wire := "HTTP/1.0 200 OK\r\nConnection: " + value + "\r\nContent-Length: 0\r\n\r\n"
			response, readErr := http.ReadResponse(bufio.NewReader(strings.NewReader(wire)), request)
			if readErr != nil {
				t.Fatal(readErr)
			}
			defer func() { _ = response.Body.Close() }()
			base, baseErr := CreateSignatureBase(MessageContext{
				Response: response, ResponseTransport: ResponseTransportReceived,
			}, SignatureInput{Components: []ComponentIdentifier{{Name: "connection"}}})
			if baseErr != nil || !strings.HasPrefix(base, "\"connection\": "+value+"\n") {
				t.Fatalf("received HTTP/1.0 connection base = %q, %v", base, baseErr)
			}
		})
	}
}

func TestSignatureBaseRejectsImpossibleReceivedResponseTransferEncoding(t *testing.T) {
	t.Parallel()

	for _, encodings := range [][]string{{"identity"}, {"chunked", "gzip"}, {"Chunked"}} {
		response := &http.Response{StatusCode: http.StatusOK, TransferEncoding: encodings}
		if _, err := CreateSignatureBase(MessageContext{
			Response: response, ResponseTransport: ResponseTransportReceived,
		}, SignatureInput{Components: []ComponentIdentifier{{Name: "transfer-encoding"}}}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("received transfer encoding %#v error = %v, want ErrSignatureBase", encodings, err)
		}
	}
}

func TestSignatureBaseTrailerMatchesResponseWriteFraming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		body             io.ReadCloser
		transferEncoding []string
		wireTrailer      bool
	}{
		{name: "exact chunked", method: http.MethodGet, body: http.NoBody, transferEncoding: []string{"chunked"}, wireTrailer: true},
		{name: "chunked suffix", method: http.MethodGet, body: http.NoBody, transferEncoding: []string{"chunked", "gzip"}, wireTrailer: true},
		{name: "mixed case chunked", method: http.MethodGet, body: http.NoBody, transferEncoding: []string{"Chunked"}},
		{name: "chunked not first", method: http.MethodGet, body: http.NoBody, transferEncoding: []string{"gzip", "chunked"}},
		{name: "nil body clears chunked", method: http.MethodGet, transferEncoding: []string{"chunked"}},
		{name: "head retains chunked declaration", method: http.MethodHead, transferEncoding: []string{"chunked"}, wireTrailer: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := &http.Response{
				StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
				Header: make(http.Header), Body: test.body, ContentLength: -1,
				TransferEncoding: append([]string(nil), test.transferEncoding...),
				Trailer:          http.Header{"X-Final": []string{"done"}},
				Request:          &http.Request{Method: test.method, RequestURI: "/"},
			}
			var wire bytes.Buffer
			if err := response.Write(&wire); err != nil {
				t.Fatalf("Response.Write() error = %v", err)
			}
			if got := strings.Contains(wire.String(), "Trailer: X-Final\r\n"); got != test.wireTrailer {
				t.Fatalf("wire response = %q, Trailer = %t, want %t", wire.String(), got, test.wireTrailer)
			}
			base, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, SignatureInput{
				Components: []ComponentIdentifier{{Name: "trailer"}},
			})
			if !test.wireTrailer {
				if !errors.Is(err, ErrSignatureBase) {
					t.Fatalf("trailer base = %q, %v, want absent component", base, err)
				}
			} else if err != nil || !strings.HasPrefix(base, "\"trailer\": X-Final\n") {
				t.Fatalf("trailer base = %q, %v, want wire declaration", base, err)
			}
		})
	}
}

func TestSignatureBaseRejectsCallerBuiltBoundaryValues(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	for _, message := range []MessageContext{{}, {Request: request, Response: &http.Response{StatusCode: 200}}} {
		if _, err := CreateSignatureBase(message, SignatureInput{}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("target cardinality error = %v", err)
		}
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}, {Name: "@method"}}}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("duplicate component error = %v", err)
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{Parameters: []Parameter{{Name: "UPPER", Value: true}}}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("invalid signature parameters error = %v", err)
	}
}

func TestSignatureBaseLimitAndFailClosedBoundaries(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		limit   int
		input   SignatureInput
		context MessageContext
	}{
		{
			name: "component count", limit: 4,
			input: SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}, {Name: "@path"}}},
		},
		{
			name: "identifier", limit: 4,
			input: SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}}},
		},
		{
			name: "identifier framing", limit: len(`"@method"`),
			input: SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}}},
		},
	} {
		test.context.Request = request
		test.context.MaxSignatureBaseBytes = test.limit
		if _, err := CreateSignatureBase(test.context, test.input); !errors.Is(err, ErrSignatureBaseLimit) {
			t.Fatalf("%s error = %v, want ErrSignatureBaseLimit", test.name, err)
		}
	}

	if _, err := resolveDerived(MessageContext{Request: request}, "@method", componentParameters{}, 2); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("oversized method error = %v, want ErrSignatureBaseLimit", err)
	}
	unsafeTarget := &http.Request{
		Method: http.MethodGet, URL: &url.URL{Path: "/é"}, RequestURI: "/é", Host: "example.test",
	}
	if _, err := CreateSignatureBase(MessageContext{Request: unsafeTarget}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "@request-target"}},
	}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("non-ASCII request-target error = %v, want ErrSignatureBase", err)
	}
}

func TestMessageLimitAccountingBoundaries(t *testing.T) {
	t.Parallel()

	for _, parameter := range []Parameter{
		{Name: "flag", Value: true},
		{Name: "flag", Value: false},
		{Name: "text", Value: `a"\\`},
		{Name: "integer", Value: int64(math.MinInt64)},
		{Name: "decimal", Value: float64(1)},
		{Name: "invalid-decimal", Value: math.Inf(1)},
		{Name: "binary", Value: []byte("x")},
		{Name: "token", Value: SFToken("value")},
		{Name: "invalid", Value: struct{}{}},
	} {
		if size := parameterUpperBound(parameter); size <= len(parameter.Name) {
			t.Fatalf("parameterUpperBound(%#v) = %d", parameter, size)
		}
	}

	component := ComponentIdentifier{Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}}
	componentSize := structuredItemUpperBound(component.Name, component.Parameters)
	if componentSize != len(`"@method";req`) {
		t.Fatalf("structuredItemUpperBound() = %d", componentSize)
	}
	if signatureParametersFit(SignatureInput{}, 1) {
		t.Fatal("signature parameters fit below the inner-list framing minimum")
	}
	if signatureParametersFit(SignatureInput{Components: make([]ComponentIdentifier, 3)}, 3) {
		t.Fatal("component count fit an impossible allocation budget")
	}
	if signatureParametersFit(SignatureInput{Components: []ComponentIdentifier{component}}, 2) {
		t.Fatal("component fit below its serialized size")
	}
	validInput := SignatureInput{
		Components: []ComponentIdentifier{{Name: "@method"}, {Name: "@path"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	serialized, err := serializeSignatureParameters(validInput)
	if err != nil {
		t.Fatal(err)
	}
	if !signatureParametersFit(validInput, len(serialized)) || signatureParametersFit(validInput, len(serialized)-1) {
		t.Fatalf("signatureParametersFit() did not enforce exact size %d", len(serialized))
	}

	for _, test := range []struct {
		values []string
		binary bool
		budget int
		want   bool
	}{
		{values: []string{"a"}, budget: -1},
		{values: []string{"", ""}, budget: 1},
		{values: []string{"a", ""}, budget: 2},
		{values: []string{"a"}, binary: true, budget: 3},
		{values: []string{"a"}, binary: true, budget: 6, want: true},
		{values: []string{"a", "b"}, budget: 4, want: true},
	} {
		if got := fieldValuesFit(test.values, test.binary, test.budget); got != test.want {
			t.Fatalf("fieldValuesFit(%#v, %t, %d) = %t, want %t", test.values, test.binary, test.budget, got, test.want)
		}
	}

	if got := escapedStringLength(`a"\\b`); got != 8 {
		t.Fatalf("escapedStringLength() = %d, want 8", got)
	}
	for _, test := range []struct {
		value int64
		want  int
	}{{0, 1}, {7, 1}, {10, 2}, {-7, 2}, {math.MinInt64, 20}} {
		if got := decimalIntegerLength(test.value); got != test.want {
			t.Fatalf("decimalIntegerLength(%d) = %d, want %d", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		left, right int
		wantOK      bool
	}{
		{-1, 0, false}, {0, -1, false}, {math.MaxInt, 1, false}, {1, 2, true},
	} {
		if _, ok := safeSizeAdd(test.left, test.right); ok != test.wantOK {
			t.Fatalf("safeSizeAdd(%d, %d) ok = %t, want %t", test.left, test.right, ok, test.wantOK)
		}
	}
	for _, test := range []struct {
		value, multiplier int
		wantOK            bool
	}{
		{-1, 1, false}, {1, -1, false}, {math.MaxInt, 2, false}, {2, 3, true},
	} {
		if _, ok := safeSizeMultiply(test.value, test.multiplier); ok != test.wantOK {
			t.Fatalf("safeSizeMultiply(%d, %d) ok = %t, want %t", test.value, test.multiplier, ok, test.wantOK)
		}
	}
	if got := saturatingSizeAdd(math.MaxInt, 1); got != math.MaxInt {
		t.Fatalf("saturatingSizeAdd() = %d", got)
	}
	if got := saturatingSizeMultiply(math.MaxInt, 2); got != math.MaxInt {
		t.Fatalf("saturatingSizeMultiply() = %d", got)
	}
	if got := base64EncodedLength(1); got != 4 {
		t.Fatalf("base64EncodedLength(1) = %d, want 4", got)
	}
	if got := base64EncodedLength(math.MaxInt); got != math.MaxInt {
		t.Fatalf("base64EncodedLength(MaxInt) = %d, want MaxInt", got)
	}
}

func TestDerivedSourceLimitAccountingBoundaries(t *testing.T) {
	t.Parallel()

	if !derivedSourceFits(nil, nil, "@path", -1) {
		t.Fatal("unavailable source should defer to semantic validation")
	}
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/path", RawQuery: "x=1"}, Host: "example.test"}
	if derivedSourceFits(request, nil, "@target-uri", 4) {
		t.Fatal("target URI source fit an undersized budget")
	}
	request.TLS = &tls.ConnectionState{}
	for _, name := range []string{"@scheme", "@authority", "@request-target", "@path", "@query", "@query-param", "@target-uri", "@unknown"} {
		if !derivedSourceFits(request, nil, name, DefaultMaxSignatureBaseBytes) {
			t.Fatalf("derivedSourceFits(%s) rejected a bounded request", name)
		}
	}
	request.URL.Opaque = "opaque"
	if !derivedSourceFits(request, nil, "@target-uri", DefaultMaxSignatureBaseBytes) {
		t.Fatal("opaque request target exceeded a bounded source budget")
	}
	external := &ExternalRequestContext{Scheme: "https", Authority: "public.example", RequestTarget: "/external?x=1"}
	if !derivedSourceFits(request, external, "@target-uri", DefaultMaxSignatureBaseBytes) {
		t.Fatal("external request context exceeded a bounded source budget")
	}
}

func TestComponentParameterAndResolutionBoundaries(t *testing.T) {
	t.Parallel()

	for _, parameters := range [][]Parameter{
		{{Name: "sf", Value: false}},
		{{Name: "key", Value: true}},
		{{Name: "name", Value: true}},
		{{Name: "unknown", Value: true}},
		{{Name: "bs", Value: true}, {Name: "sf", Value: true}},
		{{Name: "bs", Value: true}, {Name: "key", Value: "x"}},
	} {
		if _, err := componentParameterSet(parameters); err == nil {
			t.Fatalf("componentParameterSet(%#v) succeeded", parameters)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	for _, test := range []struct {
		name       string
		component  string
		parameters componentParameters
		context    MessageContext
	}{
		{name: "derived field parameter", component: "@method", parameters: componentParameters{sf: true}, context: MessageContext{Request: request}},
		{name: "name outside query", component: "@method", parameters: componentParameters{hasName: true}, context: MessageContext{Request: request}},
		{name: "missing request", component: "@method", context: MessageContext{Response: &http.Response{StatusCode: 200}}},
		{name: "query name required", component: "@query-param", context: MessageContext{Request: request}},
		{name: "status req", component: "@status", parameters: componentParameters{req: true}, context: MessageContext{Response: &http.Response{StatusCode: 200}, RelatedRequest: request}},
		{name: "status invalid", component: "@status", context: MessageContext{Response: &http.Response{StatusCode: 99}}},
		{name: "unknown", component: "@unknown", context: MessageContext{Request: request}},
	} {
		if _, err := resolveDerived(test.context, test.component, test.parameters, DefaultMaxSignatureBaseBytes); err == nil {
			t.Fatalf("resolveDerived(%s) succeeded", test.name)
		}
	}
	if _, err := resolveComponent(MessageContext{Request: request}, ComponentIdentifier{Name: "@signature-params"}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("explicit @signature-params succeeded")
	}
}

func TestFieldResolutionAndHeaderSelectionBoundaries(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	request.Header = http.Header{"X-Test": []string{"value"}}
	request.Trailer = http.Header{"X-Trailer": []string{"trailer"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"X-Test": []string{"response"}}, Trailer: http.Header{"X-Trailer": []string{"response trailer"}}}
	context := MessageContext{Response: response, RelatedRequest: request}
	for _, test := range []struct {
		related bool
		trailer bool
		want    string
	}{
		{related: true, want: "value"},
		{related: true, trailer: true, want: "trailer"},
		{want: "response"},
		{trailer: true, want: "response trailer"},
	} {
		header, err := headerForComponent(context, test.related, test.trailer)
		if err != nil || (header.Get("X-Test") != test.want && header.Get("X-Trailer") != test.want) {
			t.Fatalf("headerForComponent(%t,%t) = %#v, %v", test.related, test.trailer, header, err)
		}
	}
	if _, err := headerForComponent(MessageContext{Response: response}, true, false); err == nil {
		t.Fatal("related header without request succeeded")
	}
	if _, err := resolveField(MessageContext{Response: response}, "x", componentParameters{req: true}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("related field without request succeeded")
	}
	hostRequest := &http.Request{Host: "example.com", Header: make(http.Header)}
	if value, err := resolveField(MessageContext{Request: hostRequest}, "host", componentParameters{}, DefaultMaxSignatureBaseBytes); err != nil || value != "example.com" {
		t.Fatalf("Host fallback = %q, %v", value, err)
	}
	for _, test := range []struct {
		name       string
		parameters componentParameters
		context    MessageContext
	}{
		{name: "name", parameters: componentParameters{hasName: true}, context: MessageContext{Request: request}},
		{name: "missing", context: MessageContext{Request: request}},
		{name: "binary newline", parameters: componentParameters{bs: true}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"bad\nvalue"}}}}},
		{name: "non ascii", context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"\u0080"}}}}},
		{name: "dictionary malformed", parameters: componentParameters{hasKey: true, key: "x"}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"Bad"}}}}},
		{name: "dictionary absent", parameters: componentParameters{hasKey: true, key: "missing"}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"x=1"}}}}},
		{name: "structured unknown", parameters: componentParameters{sf: true}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"1"}}}}},
	} {
		if _, err := resolveField(test.context, "x", test.parameters, DefaultMaxSignatureBaseBytes); err == nil {
			t.Fatalf("resolveField(%s) succeeded", test.name)
		}
	}
}

func TestFieldResolutionFailsClosedAtUnavailableManagedBoundaries(t *testing.T) {
	t.Parallel()

	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	for _, name := range []string{"host", "content-length", "x-test"} {
		if _, err := resolveField(MessageContext{Response: response}, name, componentParameters{req: true}, DefaultMaxSignatureBaseBytes); err == nil {
			t.Fatalf("related %s without a request succeeded", name)
		}
	}
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, Header: make(http.Header)}
	request.TransferEncoding = []string{"gzip"}
	if _, err := resolveField(MessageContext{Request: request}, "transfer-encoding", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("unsupported request transfer coding succeeded")
	}
	response.TransferEncoding = []string{"gzip"}
	if _, err := resolveField(MessageContext{Response: response}, "transfer-encoding", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("unsupported response transfer coding succeeded")
	}
	cookieRequest := &http.Request{
		Method: http.MethodGet, ProtoMajor: 1, Header: http.Header{"Cookie": []string{"a=1\nb=2"}},
	}
	if _, err := resolveField(MessageContext{Request: cookieRequest}, "cookie", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("Cookie with a bare newline succeeded")
	}
	binaryRequest := &http.Request{
		Method: http.MethodGet, ProtoMajor: 1, Header: http.Header{"X-Test": []string{"a\nb"}},
	}
	if _, err := resolveField(MessageContext{Request: binaryRequest}, "x-test", componentParameters{bs: true}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("binary-wrapped field with a bare newline succeeded")
	}
}

func TestTransportManagedFieldHelperBoundaries(t *testing.T) {
	t.Parallel()

	inboundRequest := &http.Request{RequestURI: "/", Header: make(http.Header)}
	if _, handled, err := requestTransportFieldValues(inboundRequest, "trailer"); !handled || err == nil {
		t.Fatalf("inbound request Trailer = handled %t, error %v", handled, err)
	}
	outboundRequest := &http.Request{Header: make(http.Header)}
	if values, handled, err := requestTransportFieldValues(outboundRequest, "trailer"); !handled || err != nil || values != nil {
		t.Fatalf("absent outbound Trailer = %#v, %t, %v", values, handled, err)
	}
	if values, handled, err := requestTransportFieldValues(outboundRequest, "user-agent"); !handled || err != nil || values != nil {
		t.Fatalf("absent User-Agent = %#v, %t, %v", values, handled, err)
	}
	outboundRequest.Header = http.Header{"User-Agent": []string{"one"}, "user-agent": []string{"two"}}
	if _, handled, err := requestTransportFieldValues(outboundRequest, "user-agent"); !handled || err == nil {
		t.Fatalf("case-colliding User-Agent = handled %t, error %v", handled, err)
	}
	outboundRequest.Close = true
	outboundRequest.Header = http.Header{"Connection": []string{"keep-alive"}, "connection": []string{"upgrade"}}
	if _, handled, err := requestTransportFieldValues(outboundRequest, "connection"); !handled || err == nil {
		t.Fatalf("case-colliding request Connection = handled %t, error %v", handled, err)
	}
	outboundRequest.Header = http.Header{"Connection": []string{"keep-alive, CLOSE"}}
	if values, handled, err := requestTransportFieldValues(outboundRequest, "connection"); !handled || err != nil || len(values) != 1 {
		t.Fatalf("explicit request close = %#v, %t, %v", values, handled, err)
	}
	outboundRequest.Header = http.Header{"Connection": []string{"keep-alive"}}
	if values, handled, err := requestTransportFieldValues(outboundRequest, "connection"); !handled || err != nil || len(values) != 2 || values[0] != "close" {
		t.Fatalf("synthesized request close = %#v, %t, %v", values, handled, err)
	}

	inboundResponse := &http.Response{Request: inboundRequest, Header: make(http.Header)}
	if values, handled, err := responseTransportFieldValues(inboundResponse, ResponseTransportWrite, "trailer"); !handled || err != nil || values != nil {
		t.Fatalf("absent response Trailer = %#v, %t, %v", values, handled, err)
	}
	inboundResponse.Close = true
	inboundResponse.Header = http.Header{"Connection": []string{"keep-alive"}, "connection": []string{"upgrade"}}
	if _, handled, err := responseTransportFieldValues(inboundResponse, ResponseTransportWrite, "connection"); !handled || err == nil {
		t.Fatalf("case-colliding response Connection = handled %t, error %v", handled, err)
	}
	inboundResponse.Header = http.Header{"Connection": []string{"keep-alive, CLOSE"}}
	if values, handled, err := responseTransportFieldValues(inboundResponse, ResponseTransportWrite, "connection"); !handled || err != nil || len(values) != 1 {
		t.Fatalf("explicit response close = %#v, %t, %v", values, handled, err)
	}
	inboundResponse.Header = http.Header{"Connection": []string{"keep-alive"}}
	if values, handled, err := responseTransportFieldValues(inboundResponse, ResponseTransportWrite, "connection"); !handled || err != nil || len(values) != 2 || values[0] != "close" {
		t.Fatalf("synthesized response close = %#v, %t, %v", values, handled, err)
	}

	if receivedResponseCloseFieldIsExplicit(&http.Response{ProtoMajor: 2, ProtoMinor: 0}) {
		t.Fatal("HTTP/2 close was classified as an explicit HTTP/1 field")
	}
	if !receivedResponseCloseFieldIsExplicit(&http.Response{
		StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1,
		ContentLength: -1, Request: &http.Request{Method: http.MethodHead},
	}) {
		t.Fatal("HEAD response close was not classified as explicit")
	}

	for _, trailer := range []http.Header{{"Bad Key": nil}, {"Content-Length": nil}} {
		if _, err := trailerDeclarationFieldValues(trailer); err == nil {
			t.Fatalf("trailerDeclarationFieldValues(%#v) succeeded", trailer)
		}
	}
	for _, encodings := range [][]string{nil, {"identity"}} {
		if values, err := transferEncodingFieldValues(encodings); err != nil || values != nil {
			t.Fatalf("transferEncodingFieldValues(%#v) = %#v, %v", encodings, values, err)
		}
	}
	if _, err := transferEncodingFieldValues([]string{"gzip"}); err == nil {
		t.Fatal("unsupported transfer coding succeeded")
	}
	if !fieldValueHasToken("keep-alive, CLOSE", "close") || fieldValueHasToken("keep-alive, upgrade", "close") {
		t.Fatal("Connection token matching did not preserve list boundaries")
	}

	if _, err := responseTransferEncodingFieldValues(nil, ResponseTransportWrite); err == nil {
		t.Fatal("nil response transfer encoding succeeded")
	}
	receivedChunked := &http.Response{TransferEncoding: []string{"chunked"}}
	if values, err := responseTransferEncodingFieldValues(receivedChunked, ResponseTransportReceived); err != nil || len(values) != 1 || values[0] != "chunked" {
		t.Fatalf("received response transfer encoding = %#v, %v", values, err)
	}
	if values, err := responseContentLengthFieldValues(nil, ResponseTransportWrite); err != nil || values != nil {
		t.Fatalf("nil response content length = %#v, %v", values, err)
	}
	for _, header := range []http.Header{
		{"Content-Length": []string{"1", "1"}},
		{"Content-Length": []string{"invalid"}},
	} {
		if _, err := preservedResponseContentLengthFieldValues(header); err == nil {
			t.Fatalf("preservedResponseContentLengthFieldValues(%#v) succeeded", header)
		}
	}
	if values, err := responseTrailerFieldValues(&http.Response{}, ResponseTransportUnspecified); err != nil || values != nil {
		t.Fatalf("unambiguous absent response Trailer = %#v, %v", values, err)
	}
	if sameFieldValues([]string{"a"}, []string{"b"}) || !sameFieldValues([]string{"a"}, []string{"a"}) {
		t.Fatal("field value equality did not compare member values")
	}
}

func TestMessageRequestMetadataHelperBoundaries(t *testing.T) {
	t.Parallel()

	if componentHTTPMajor(MessageContext{Response: &http.Response{}}, true) != 0 || componentHTTPMajor(MessageContext{}, false) != 0 {
		t.Fatal("unavailable HTTP version did not fail closed")
	}
	for _, test := range []struct {
		name    string
		request *http.Request
		length  int64
		ok      bool
	}{
		{name: "nil"},
		{name: "unsupported transfer coding", request: &http.Request{TransferEncoding: []string{"gzip"}}},
		{name: "nil body with declared length", request: &http.Request{ContentLength: 1}},
		{name: "unknown body length", request: &http.Request{Body: io.NopCloser(strings.NewReader("x"))}},
		{name: "empty default method", request: &http.Request{Body: http.NoBody}},
		{name: "POST", request: &http.Request{Method: http.MethodPost, Body: http.NoBody}, ok: true},
		{name: "identity DELETE", request: &http.Request{Method: http.MethodDelete, Body: http.NoBody, TransferEncoding: []string{"identity"}}, ok: true},
		{name: "DELETE", request: &http.Request{Method: http.MethodDelete, Body: http.NoBody}},
		{name: "known body length", request: &http.Request{Body: io.NopCloser(strings.NewReader("x")), ContentLength: 1}, length: 1, ok: true},
	} {
		length, ok := requestContentLength(test.request)
		if length != test.length || ok != test.ok {
			t.Fatalf("requestContentLength(%s) = %d, %t; want %d, %t", test.name, length, ok, test.length, test.ok)
		}
	}
	if _, err := requestParts(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/"}, nil); err == nil {
		t.Fatal("request without an authority succeeded")
	}
	if got := removeIPv6Zone("[unterminated"); got != "[unterminated" {
		t.Fatalf("removeIPv6Zone() = %q", got)
	}
}

func TestRequestPartsAndAuthorityBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := requestParts(nil, nil); err == nil {
		t.Fatal("requestParts(nil) succeeded")
	}
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/", Host: "example.com"}
	if _, err := requestParts(request, &ExternalRequestContext{}); err == nil {
		t.Fatal("partial external context succeeded")
	}
	for _, scheme := range []string{"ftp", "HTTP+unix"} {
		copy := request.Clone(request.Context())
		copy.URL.Scheme = scheme
		if _, err := requestParts(copy, nil); err == nil {
			t.Fatalf("scheme %q succeeded", scheme)
		}
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{}, RequestURI: "example.com:443", Host: "example.com:443"}
	if _, err := requestParts(connect, &ExternalRequestContext{Scheme: "https", Authority: "other.example:443", RequestTarget: "example.com:443"}); err == nil {
		t.Fatal("contradictory CONNECT external context succeeded")
	}
	relative := &http.Request{Method: http.MethodGet, URL: &url.URL{}, RequestURI: "relative", Host: "example.com"}
	if _, err := requestParts(relative, nil); err == nil {
		t.Fatal("relative request target succeeded")
	}
	tlsRequest := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/", Host: "example.com", TLS: &tls.ConnectionState{}}
	if parts, err := requestParts(tlsRequest, nil); err != nil || parts.scheme != "https" {
		t.Fatalf("TLS request scheme = %#v, %v", parts, err)
	}
	if externalForRequest(MessageContext{}, request) != nil {
		t.Fatal("externalForRequest returned unrelated context")
	}
	for _, authority := range []string{"", "user@example.com", "example.com:bad", "example.com:99999", "\u0080.example"} {
		if _, err := normalizeAuthority(authority, "https"); err == nil {
			t.Fatalf("normalizeAuthority(%q) succeeded", authority)
		}
	}
	if authority, err := normalizeAuthority("[2001:db8::1]:8443", "https"); err != nil || authority != "[2001:db8::1]:8443" {
		t.Fatalf("IPv6 authority = %q, %v", authority, err)
	}
}

func TestFieldAndStructuredSerializationBoundaries(t *testing.T) {
	t.Parallel()

	if value, err := normalizeFieldBytes(" a\r\n\t  b "); err != nil || value != "a b" {
		t.Fatalf("obs-fold normalization = %q, %v", value, err)
	}
	for _, value := range []string{"bad\rvalue", "bad\nvalue"} {
		if _, err := normalizeFieldBytes(value); err == nil {
			t.Fatalf("normalizeFieldBytes(%q) succeeded", value)
		}
	}
	if _, err := normalizeFieldValue("\u0080"); err == nil {
		t.Fatal("non-ASCII field value succeeded")
	}
	if _, err := normalizeFieldValue("bad\nvalue"); err == nil {
		t.Fatal("field normalization error was ignored")
	}
	for _, test := range []struct {
		values    []string
		fieldType StructuredFieldType
	}{
		{values: []string{"1", "2"}, fieldType: StructuredFieldItem},
		{values: []string{"Bad"}, fieldType: StructuredFieldDictionary},
		{values: []string{"1"}, fieldType: StructuredFieldType(255)},
	} {
		if _, err := strictStructuredField(test.values, test.fieldType); err == nil {
			t.Fatalf("strictStructuredField(%#v,%d) succeeded", test.values, test.fieldType)
		}
	}
	member, _ := httpsfv.UnmarshalItem([]string{"?1;a"})
	if value := marshalMember(member); value != "?1;a" {
		t.Fatalf("marshalMember(bare true) = %q", value)
	}
	if _, err := serializeComponentIdentifier(ComponentIdentifier{Name: "@method", Parameters: []Parameter{{Name: "UPPER", Value: true}}}); err == nil {
		t.Fatal("invalid component serialization succeeded")
	}
	if _, err := serializeSignatureParameters(SignatureInput{Components: []ComponentIdentifier{{Name: "@"}}}); err == nil {
		t.Fatal("invalid signature component serialization succeeded")
	}
}

func TestRFC8941TypeGuardsRejectUnsupportedMemberGraphs(t *testing.T) {
	t.Parallel()

	validParams := httpsfv.NewParams()
	validInner := httpsfv.InnerList{Items: []httpsfv.Item{httpsfv.NewItem("token")}, Params: validParams}
	if !isRFC8941StructuredField(validInner) {
		t.Fatal("valid RFC 8941 inner list was rejected")
	}
	if isRFC8941StructuredField(httpsfv.List{httpsfv.InnerList{}}) {
		t.Fatal("list member with missing parameters was accepted")
	}
	dictionary := httpsfv.NewDictionary()
	dictionary.Add("x", httpsfv.InnerList{})
	if isRFC8941StructuredField(dictionary) {
		t.Fatal("dictionary member with missing parameters was accepted")
	}
	var structured httpsfv.StructuredFieldValue
	if isRFC8941StructuredField(structured) {
		t.Fatal("nil structured field was accepted")
	}
	var member httpsfv.Member
	if isRFC8941Member(member) {
		t.Fatal("nil member was accepted")
	}
	invalidItem := httpsfv.NewItem("token")
	invalidItem.Params.Add("unsupported", time.Unix(1, 0))
	if isRFC8941Member(httpsfv.InnerList{Items: []httpsfv.Item{invalidItem}, Params: validParams}) {
		t.Fatal("inner-list item with an unsupported parameter was accepted")
	}
	if isRFC8941Parameters(nil) {
		t.Fatal("nil parameters were accepted")
	}
	invalidParams := httpsfv.NewParams()
	invalidParams.Add("unsupported", time.Unix(1, 0))
	if isRFC8941Parameters(invalidParams) {
		t.Fatal("unsupported bare parameter value was accepted")
	}
}

func TestQueryAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		name string
	}{
		{raw: "", name: "x"},
		{raw: "%ZZ=1", name: "x"},
		{raw: "x=1&x=2", name: "x"},
	} {
		if _, err := queryParameter(test.raw, test.name, DefaultMaxSignatureBaseBytes); err == nil {
			t.Fatalf("queryParameter(%q,%q) succeeded", test.raw, test.name)
		}
	}
	if value, err := queryParameter("x", "x", DefaultMaxSignatureBaseBytes); err != nil || value != "" {
		t.Fatalf("valueless query = %q, %v", value, err)
	}
	if value, err := queryParameter("other=1&x=2", "x", DefaultMaxSignatureBaseBytes); err != nil || value != "2" {
		t.Fatalf("query scan = %q, %v", value, err)
	}
	if value, err := queryParameter("x=%ZZ", "x", DefaultMaxSignatureBaseBytes); err != nil || value != "%25ZZ" {
		t.Fatalf("malformed percent form value = %q, %v", value, err)
	}
	if got := formPercentEncode("a b+"); got != "a%20b%2B" {
		t.Fatalf("formPercentEncode() = %q", got)
	}
	if validFieldBaseValue("bad\x00") || asciiString("\u0080") {
		t.Fatal("prohibited field bytes accepted")
	}
	for _, parameters := range [][]Parameter{
		{{Name: "", Value: true}},
		{{Name: "UPPER", Value: true}},
		{{Name: "bad!", Value: true}},
		{{Name: "ok", Value: struct{}{}}},
	} {
		if validParametersForSerialization(parameters) {
			t.Fatalf("validParametersForSerialization(%#v) = true", parameters)
		}
	}
	if !strings.Contains("keep", "keep") {
		t.Fatal("unreachable")
	}
}

func TestFormDecodingAndQueryLimitBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := queryParameter("x=1", "x", 8); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("raw query limit error = %v, want ErrSignatureBaseLimit", err)
	}
	if _, err := queryParameter("x="+string([]byte{0xff, 0xff}), "x", 12); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("decoded query limit error = %v, want ErrSignatureBaseLimit", err)
	}
	if value, err := queryParameter("&x=1&", "x", DefaultMaxSignatureBaseBytes); err != nil || value != "1" {
		t.Fatalf("query with empty pairs = %q, %v", value, err)
	}
	if got := decodeFormComponent("a+b%af%"); got != "a b�%" {
		t.Fatalf("decodeFormComponent() = %q", got)
	}

	for _, test := range []struct {
		first byte
		width int
	}{
		{0xc2, 2}, {0xe0, 3}, {0xe1, 3}, {0xed, 3}, {0xf0, 4}, {0xf1, 4}, {0xf4, 4}, {0x80, 0},
	} {
		width, _, _ := utf8Sequence(test.first)
		if width != test.width {
			t.Fatalf("utf8Sequence(%x) width = %d, want %d", test.first, width, test.width)
		}
	}
	for _, test := range []struct {
		input []byte
		want  string
	}{
		{[]byte{0xc2, 0xa2}, "¢"},
		{[]byte{0xe0, 0xa0, 0x80}, "ࠀ"},
		{[]byte{0xe1, 0x80, 0x80}, "က"},
		{[]byte{0xed, 0x9f, 0xbf}, "퟿"},
		{[]byte{0xf0, 0x90, 0x80, 0x80}, "𐀀"},
		{[]byte{0xf1, 0x80, 0x80, 0x80}, "\U00040000"},
		{[]byte{0xf4, 0x8f, 0xbf, 0xbf}, "\U0010ffff"},
		{[]byte{0x80}, "�"},
		{[]byte{0xe1}, "�"},
		{[]byte{0xe1, 0x80}, "�"},
		{[]byte{0xe1, 'x'}, "�x"},
		{[]byte{0xe1, 0x80, 'x'}, "�x"},
	} {
		if got := decodeUTF8WithReplacement(test.input); got != test.want {
			t.Fatalf("decodeUTF8WithReplacement(%x) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestMessageByteAndStatusExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, status := range []int{100, 599} {
		value, err := resolveDerived(MessageContext{Response: &http.Response{StatusCode: status}}, "@status", componentParameters{}, DefaultMaxSignatureBaseBytes)
		if err != nil || value != strconv.Itoa(status) {
			t.Fatalf("status %d = %q, %v", status, value, err)
		}
	}
	for _, status := range []int{99, 600, 1000} {
		if _, err := resolveDerived(MessageContext{Response: &http.Response{StatusCode: status}}, "@status", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
			t.Fatalf("status %d accepted", status)
		}
	}

	const allowed = "azAZ09*-._"
	if got := formPercentEncode(allowed); got != allowed {
		t.Fatalf("allowed form bytes = %q", got)
	}
	if got := formPercentEncode("`{: @"); got != "%60%7B%3A%20%40" {
		t.Fatalf("escaped form bytes = %q", got)
	}
	for _, value := range []string{"!", "~", "a b"} {
		if !validDerivedValue(value) {
			t.Fatalf("validDerivedValue(%q) = false", value)
		}
	}
	for _, value := range []string{"", " x", "x ", "\tx", "x\t", string([]byte{0x1f}), string([]byte{0x7f})} {
		if validDerivedValue(value) {
			t.Fatalf("validDerivedValue(%q) = true", value)
		}
	}
	for _, value := range []string{"\t", " ", "~"} {
		if !validFieldBaseValue(value) {
			t.Fatalf("validFieldBaseValue(%q) = false", value)
		}
	}
	for _, value := range []string{string([]byte{0x00}), string([]byte{0x1f}), string([]byte{0x7f}), string([]byte{0x80})} {
		if validFieldBaseValue(value) {
			t.Fatalf("validFieldBaseValue(%q) = true", value)
		}
	}
	if !asciiString(string([]byte{0x7f})) || asciiString(string([]byte{0x80})) {
		t.Fatal("ASCII boundary classified incorrectly")
	}
	for _, test := range []struct{ input, want string }{
		{"a\r\n b", "a b"}, {"a\r\n\t  b", "a b"}, {"a\r\n b\r\n\tc", "a b c"},
	} {
		if got, err := normalizeFieldBytes(test.input); err != nil || got != test.want {
			t.Fatalf("normalizeFieldBytes(%q) = %q, %v", test.input, got, err)
		}
	}
}

func TestExternalRequestSelectionBoundaries(t *testing.T) {
	t.Parallel()

	request := &http.Request{}
	related := &http.Request{}
	unrelated := &http.Request{}
	external := &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/"}
	context := MessageContext{Request: request, RelatedRequest: related, ExternalRequest: external}
	if externalForRequest(context, request) != external || externalForRequest(context, related) != external {
		t.Fatal("target request did not receive external context")
	}
	if externalForRequest(context, unrelated) != nil {
		t.Fatal("unrelated request received external context")
	}
}

func TestMatchingExternalAbsoluteAndAuthorityTargets(t *testing.T) {
	t.Parallel()

	absolute := &http.Request{
		Method: http.MethodGet, URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/a", RawQuery: "b=1"},
		RequestURI: "https://example.com/a?b=1", Host: "example.com",
	}
	parts, err := requestParts(absolute, &ExternalRequestContext{Scheme: "HTTPS", Authority: "EXAMPLE.COM:443", RequestTarget: absolute.RequestURI})
	if err != nil || parts.authority != "example.com" || parts.path != "/a" || parts.rawQuery != "b=1" {
		t.Fatalf("matching absolute target = %#v, %v", parts, err)
	}

	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{}, RequestURI: "example.com:443", Host: "example.com:443"}
	parts, err = requestParts(connect, &ExternalRequestContext{Scheme: "https", Authority: "EXAMPLE.COM:443", RequestTarget: connect.RequestURI})
	if err != nil || parts.authority != "example.com" || parts.requestTarget != "example.com:443" {
		t.Fatalf("matching authority target = %#v, %v", parts, err)
	}
}

func TestRequestPartsRejectsEachMalformedTargetBoundary(t *testing.T) {
	t.Parallel()

	base := func(method, target string) *http.Request {
		return &http.Request{Method: method, URL: &url.URL{Path: "/"}, RequestURI: target, Host: "example.com"}
	}
	tests := []struct {
		request  *http.Request
		external *ExternalRequestContext
	}{
		{base(http.MethodGet, "/"), &ExternalRequestContext{Scheme: "https", Authority: "example.com"}},
		{base(http.MethodGet, "http://[::1"), nil},
		{base(http.MethodGet, "http:///path"), nil},
		{base(http.MethodGet, "http://münich.example/"), &ExternalRequestContext{Scheme: "http", Authority: "münich.example", RequestTarget: "http://münich.example/"}},
		{base(http.MethodGet, "http://example.com:bad/"), &ExternalRequestContext{Scheme: "http", Authority: "example.com:bad", RequestTarget: "http://example.com:bad/"}},
		{base(http.MethodGet, "http://example.com/"), &ExternalRequestContext{Scheme: "http", Authority: "example.com:bad", RequestTarget: "http://example.com/"}},
		{base(http.MethodGet, "http://example.com/"), &ExternalRequestContext{Scheme: "http", Authority: "other.example", RequestTarget: "http://example.com/"}},
		{base(http.MethodGet, "/%zz"), nil},
		{base(http.MethodConnect, "[::1"), nil},
		{base(http.MethodConnect, "?"), nil},
		{base(http.MethodConnect, "münich.example:443"), &ExternalRequestContext{Scheme: "https", Authority: "münich.example:443", RequestTarget: "münich.example:443"}},
		{base(http.MethodConnect, "example.com:bad"), &ExternalRequestContext{Scheme: "https", Authority: "example.com:bad", RequestTarget: "example.com:bad"}},
		{base(http.MethodConnect, "example.com:443"), &ExternalRequestContext{Scheme: "https", Authority: "example.com:bad", RequestTarget: "example.com:443"}},
	}
	for _, test := range tests {
		if _, err := requestParts(test.request, test.external); err == nil {
			t.Fatalf("requestParts(%q, %#v) succeeded", test.request.RequestURI, test.external)
		}
	}
}
