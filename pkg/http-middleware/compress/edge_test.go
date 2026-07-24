package compress

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestConfigurationAndNegotiationBoundaries(t *testing.T) {
	t.Parallel()
	if _, err := New(Policy{}); err != nil {
		t.Fatalf("default policy error = %v", err)
	}
	if _, err := New(Policy{ExcludedTypes: []string{"TEXT/PLAIN; charset=utf-8"}}); err != nil {
		t.Fatalf("excluded media type error = %v", err)
	}
	for _, policy := range []Policy{
		{MinimumBytes: -1}, {MinimumBytes: 2, MaxBuffer: 1}, {MinimumBytes: 1, MaxBuffer: 16<<20 + 1},
		{MinimumBytes: 1, MaxBuffer: 1, Level: gzip.HuffmanOnly - 1}, {MinimumBytes: 1, MaxBuffer: 1, Level: gzip.BestCompression + 1},
		{MinimumBytes: 1, MaxBuffer: 1, MaxHeaderBytes: -1}, {MinimumBytes: 1, MaxBuffer: 1, MaxHeaderBytes: 1<<20 + 1},
		{MinimumBytes: 1, MaxBuffer: 1, ExcludedTypes: make([]string, 65)},
		{MinimumBytes: 1, MaxBuffer: 1, ExcludedTypes: []string{"invalid"}},
		{MinimumBytes: 1, MaxBuffer: 1, ExcludedTypes: []string{strings.Repeat("x", 257)}},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	for _, lines := range [][]string{{"gzip;"}, {"gzip;level=1"}, {"gzip;q=1;q=1"}, {"gzip;q=no"}, {strings.Repeat("x", 9)}} {
		if _, _, ok := negotiate(lines, 8); ok {
			t.Fatalf("negotiate(%q) succeeded", lines)
		}
	}
	for _, tc := range []struct {
		lines          []string
		gzip, identity float64
		ok             bool
	}{
		{nil, 0, 1, true}, {[]string{""}, 0, 1, true}, {[]string{"*;q=0.5"}, .5, 1, true},
		{[]string{"*;q=0"}, 0, 0, false}, {[]string{"gzip;q=0.5, gzip;q=0.8, identity;q=0"}, .8, 0, true},
		{[]string{"br, identity;q=0"}, 0, 0, false},
	} {
		gzipQ, identityQ, ok := negotiate(tc.lines, 128)
		if gzipQ != tc.gzip || identityQ != tc.identity || ok != tc.ok {
			t.Fatalf("negotiate(%q) = %v, %v, %v", tc.lines, gzipQ, identityQ, ok)
		}
	}
}

func TestResponseBufferAndCompressionDecisionMatrix(t *testing.T) {
	t.Parallel()
	destination := httptest.NewRecorder()
	buffer := newBuffer(destination, 2)
	buffer.Header().Set("X-Test", "yes")
	buffer.WriteHeader(http.StatusEarlyHints)
	if destination.Code != http.StatusEarlyHints {
		t.Fatalf("informational status = %d", destination.Code)
	}
	destination = httptest.NewRecorder()
	buffer = newBuffer(destination, 2)
	buffer.Header().Set("X-Test", "yes")
	buffer.WriteHeader(http.StatusCreated)
	buffer.WriteHeader(http.StatusAccepted)
	if _, err := buffer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("d")); err != nil {
		t.Fatal(err)
	}
	buffer.commitIdentity()
	if !buffer.spilled || destination.Code != http.StatusCreated || destination.Body.String() != "abcd" {
		t.Fatalf("buffer = %+v, response = %d %q", buffer, destination.Code, destination.Body.String())
	}

	baseRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	makeBuffer := func(status int, header http.Header, body string) *responseBuffer {
		value := newBuffer(httptest.NewRecorder(), 100)
		value.status = status
		value.header = header
		_, _ = value.buffer.WriteString(body)
		return value
	}
	for _, tc := range []struct {
		name             string
		request          *http.Request
		buffer           *responseBuffer
		gzipQ, identityQ float64
		excluded         []string
	}{
		{"gzip disabled", baseRequest, makeBuffer(200, http.Header{}, "body"), 0, 1, nil},
		{"identity preferred", baseRequest, makeBuffer(200, http.Header{}, "body"), .5, 1, nil},
		{"head", httptest.NewRequest(http.MethodHead, "/", nil), makeBuffer(200, http.Header{}, "body"), 1, 1, nil},
		{"informational", baseRequest, makeBuffer(101, http.Header{}, "body"), 1, 1, nil},
		{"no content", baseRequest, makeBuffer(204, http.Header{}, "body"), 1, 1, nil},
		{"not modified", baseRequest, makeBuffer(304, http.Header{}, "body"), 1, 1, nil},
		{"request range", requestWithHeader("Range", "bytes=0-1"), makeBuffer(200, http.Header{}, "body"), 1, 1, nil},
		{"response range", baseRequest, makeBuffer(200, http.Header{"Content-Range": {"bytes 0-1/4"}}, "body"), 1, 1, nil},
		{"encoded", baseRequest, makeBuffer(200, http.Header{"Content-Encoding": {"br"}}, "body"), 1, 1, nil},
		{"no transform", baseRequest, makeBuffer(200, http.Header{"Cache-Control": {"private, NO-TRANSFORM"}}, "body"), 1, 1, nil},
		{"small", baseRequest, makeBuffer(200, http.Header{}, "x"), 1, 1, nil},
		{"excluded", baseRequest, makeBuffer(200, http.Header{"Content-Type": {"image/png"}}, "body"), 1, 1, []string{"IMAGE/PNG"}},
	} {
		if shouldCompress(tc.request, tc.buffer, tc.gzipQ, tc.identityQ, 2, tc.excluded) {
			t.Fatalf("%s compressed", tc.name)
		}
	}
	if !shouldCompress(baseRequest, makeBuffer(0, http.Header{"Content-Type": {"text/plain"}}, "body"), 1, 1, 2, nil) {
		t.Fatal("eligible response not compressed")
	}
	if statusOrOK(0) != http.StatusOK || statusOrOK(201) != 201 {
		t.Fatal("status default failed")
	}
}

func TestResponseBufferRejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []int{0, 99, 1000} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("WriteHeader did not panic")
				}
			}()
			newBuffer(httptest.NewRecorder(), 1).WriteHeader(status)
		})
	}
}

func TestResponseBufferCommitsProtocolSwitchImmediately(t *testing.T) {
	t.Parallel()
	destination := httptest.NewRecorder()
	buffer := newBuffer(destination, 64)
	buffer.WriteHeader(http.StatusSwitchingProtocols)
	if !buffer.spilled || destination.Code != http.StatusSwitchingProtocols {
		t.Fatalf("spilled = %v, status = %d", buffer.spilled, destination.Code)
	}
}

func TestMiddlewareRejectsNoAcceptableCodingAndCompressesPastBuffer(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{MinimumBytes: 1, MaxBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "identity;q=0, gzip;q=0")
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "identity;q=0, gzip")
	recorder = httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("lar"))
		_ = w.Header().Get("Content-Encoding")
		_, _ = w.Write([]byte("ge"))
	})).ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("headers = %v", recorder.Header())
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "large" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestStreamingCompressionReportsDestinationFailure(t *testing.T) {
	t.Parallel()
	destination := &failingWriter{header: make(http.Header)}
	buffer := newBuffer(destination, 2)
	buffer.compression = &streamPolicy{
		request:     httptest.NewRequest(http.MethodGet, "/", nil),
		gzipQuality: 1, identityQuality: 0, minimum: 1, level: gzip.DefaultCompression,
	}
	if _, err := buffer.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("large")); err == nil {
		t.Fatal("streaming compression write succeeded")
	}
}

func TestStreamingCompressionClosesEncoderOnPanic(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{MinimumBytes: 1, MaxBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("panic = %v", recovered)
			}
		}()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("large"))
			panic("boom")
		})).ServeHTTP(recorder, request)
	}()
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "large" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestStreamingCompressionClosesEncoderAfterCancellation(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{MinimumBytes: 1, MaxBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	written := make(chan struct{})
	done := make(chan struct{})
	go func() {
		middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("large"))
			close(written)
			<-r.Context().Done()
		})).ServeHTTP(recorder, request)
		close(done)
	}()
	<-written
	cancel()
	<-done
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "large" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestLogicalHeadersAndTrailersRemainSeparated(t *testing.T) {
	t.Parallel()
	destination := httptest.NewRecorder()
	buffer := newBuffer(destination, 64)
	buffer.Header().Set("Trailer", "X-Checksum, Digest")
	buffer.Header().Set("X-Checksum", "early")
	buffer.Header().Set("Digest", "early")
	buffer.Header().Set(http.TrailerPrefix+"X-Late", "early")
	if _, err := buffer.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	buffer.commitHeader()
	if buffer.committed.Get("X-Checksum") != "" || buffer.committed.Get("Digest") != "" || buffer.committed.Get(http.TrailerPrefix+"X-Late") != "" {
		t.Fatalf("committed headers = %v", buffer.committed)
	}
	buffer.header.Set("X-Checksum", "done")
	buffer.header.Set("Digest", "sha-256=secret")
	buffer.header.Set(http.TrailerPrefix+"X-Late", "done")
	buffer.header.Set(http.TrailerPrefix+"Digest", "sha-256=secret")
	buffer.spilled = true
	buffer.compressed = true
	buffer.finish()
	if destination.Header().Get("X-Checksum") != "done" || destination.Header().Get(http.TrailerPrefix+"X-Late") != "done" {
		t.Fatalf("trailers = %v", destination.Header())
	}
	if destination.Header().Get("Digest") != "" || destination.Header().Get(http.TrailerPrefix+"Digest") != "" {
		t.Fatalf("representation digest retained: %v", destination.Header())
	}

	identity := newBuffer(httptest.NewRecorder(), 64)
	identity.header.Set("Trailer", "Digest")
	identity.commitHeader()
	identity.header.Set("Digest", "sha-256=identity")
	identity.header.Set(http.TrailerPrefix+"Digest", "sha-256=identity")
	identity.spilled = true
	identity.finish()
	if identity.destination.Header().Get("Digest") == "" || identity.destination.Header().Get(http.TrailerPrefix+"Digest") == "" {
		t.Fatalf("identity trailers = %v", identity.destination.Header())
	}
}

func TestTrailerDeclarationAndRepresentationFiltering(t *testing.T) {
	t.Parallel()
	header := http.Header{"Trailer": {", Digest, X-Checksum"}}
	if got := declaredTrailers(header); len(got) != 2 || got[1] != "X-Checksum" {
		t.Fatalf("declared trailers = %v", got)
	}
	removeRepresentationTrailers(header)
	if header.Get("Trailer") != "X-Checksum" {
		t.Fatalf("filtered trailers = %v", header)
	}
	header.Set("Trailer", "Digest")
	removeRepresentationTrailers(header)
	if header.Get("Trailer") != "" {
		t.Fatalf("representation trailer retained: %v", header)
	}
	for _, name := range []string{"Content-Length", "ETag", "Content-MD5", "Digest"} {
		if !representationHeader(name) {
			t.Fatalf("representationHeader(%q) = false", name)
		}
	}
	if representationHeader("X-Checksum") {
		t.Fatal("custom trailer classified as representation metadata")
	}
}

type failingWriter struct{ header http.Header }

func (w *failingWriter) Header() http.Header       { return w.header }
func (w *failingWriter) WriteHeader(int)           {}
func (w *failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func requestWithHeader(name, value string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(name, value)
	return request
}
