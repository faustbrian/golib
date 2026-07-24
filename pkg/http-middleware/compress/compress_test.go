package compress_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/compress"
)

func TestGzipNegotiationHonorsQualityAndMergesVary(t *testing.T) {
	t.Parallel()

	middleware, err := compress.New(compress.Policy{MinimumBytes: 4})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br;q=1, gzip;q=0.5, identity;q=0")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Vary", "Origin")
		_, _ = io.WriteString(w, "compressible payload")
	})).ServeHTTP(recorder, req)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("headers = %v", recorder.Header())
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip read error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	if string(payload) != "compressible payload" {
		t.Fatalf("payload = %q", payload)
	}
	if !strings.Contains(recorder.Header().Get("Vary"), "Accept-Encoding") || !strings.Contains(recorder.Header().Get("Vary"), "Origin") {
		t.Fatalf("Vary = %q", recorder.Header().Get("Vary"))
	}
}

func TestCompressionSkipsNoBodyHeadRangeAndAlreadyEncodedResponses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, method string
		status       int
		setup        func(*http.Request)
		header       string
	}{
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "no-body", method: http.MethodGet, status: http.StatusNoContent},
		{name: "range", method: http.MethodGet, status: http.StatusOK, setup: func(r *http.Request) { r.Header.Set("Range", "bytes=0-3") }},
		{name: "encoded", method: http.MethodGet, status: http.StatusOK, header: "br"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware, _ := compress.New(compress.Policy{MinimumBytes: 1})
			req := httptest.NewRequest(tc.method, "/", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			if tc.setup != nil {
				tc.setup(req)
			}
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set("Content-Encoding", tc.header)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, "payload")
			})).ServeHTTP(recorder, req)
			if recorder.Header().Get("Content-Encoding") == "gzip" {
				t.Fatalf("headers = %v", recorder.Header())
			}
		})
	}
}

func TestExplicitCodingQualityOverridesWildcardAndEmptyMeansIdentity(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"gzip;q=0, *;q=1", ""} {
		t.Run(value, func(t *testing.T) {
			middleware, _ := compress.New(compress.Policy{MinimumBytes: 1})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header["Accept-Encoding"] = []string{value}
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Vary", "Origin")
				_, _ = io.WriteString(w, "payload")
			})).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Encoding") != "" {
				t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
			}
			if !strings.Contains(recorder.Header().Get("Vary"), "Accept-Encoding") {
				t.Fatalf("Vary = %q", recorder.Header().Get("Vary"))
			}
		})
	}
}

func TestMalformedOrOversizedAcceptEncodingFailsBounded(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"gzip;q=1.0000", "gzip;q=0.5;q=0.4", strings.Repeat("x", 65)} {
		middleware, _ := compress.New(compress.Policy{MinimumBytes: 1, MaxHeaderBytes: 64})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", value)
		recorder := httptest.NewRecorder()
		middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotAcceptable {
			t.Fatalf("value %q status = %d", value, recorder.Code)
		}
	}
}

func TestVaryAppliesToIdentityAndCodingRejection(t *testing.T) {
	t.Parallel()
	middleware, err := compress.New(compress.Policy{MinimumBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, accept string
		status       int
	}{
		{name: "identity", accept: "identity", status: http.StatusOK},
		{name: "rejected", accept: "identity;q=0, gzip;q=0", status: http.StatusNotAcceptable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Encoding", tc.accept)
			middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("body"))
			})).ServeHTTP(recorder, request)
			if recorder.Code != tc.status || !strings.Contains(strings.ToLower(recorder.Header().Get("Vary")), "accept-encoding") {
				t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
			}
		})
	}
}
