package compress

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactCompressionPolicyBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	excluded := make([]string, 64)
	for index := range excluded {
		excluded[index] = "text/plain"
	}
	for name, policy := range map[string]Policy{
		"minimum bytes":        {MinimumBytes: 1, MaxBuffer: 1},
		"equal buffer":         {MinimumBytes: 2, MaxBuffer: 2},
		"maximum buffer":       {MinimumBytes: 1, MaxBuffer: 16 << 20},
		"Huffman-only level":   {MinimumBytes: 1, MaxBuffer: 1, Level: gzip.HuffmanOnly},
		"best level":           {MinimumBytes: 1, MaxBuffer: 1, Level: gzip.BestCompression},
		"minimum header":       {MinimumBytes: 1, MaxBuffer: 1, MaxHeaderBytes: 1},
		"maximum header":       {MinimumBytes: 1, MaxBuffer: 1, MaxHeaderBytes: 1 << 20},
		"maximum exclusions":   {MinimumBytes: 1, MaxBuffer: 1, ExcludedTypes: excluded},
		"maximum media length": {MinimumBytes: 1, MaxBuffer: 1, ExcludedTypes: []string{"a/" + strings.Repeat("b", 254)}},
	} {
		if _, err := New(policy); err != nil {
			t.Fatalf("New(%s exact bound) error = %v", name, err)
		}
	}
}

func TestExactInformationalAndFinalStatusBounds(t *testing.T) {
	t.Parallel()

	informationalDestination := httptest.NewRecorder()
	informational := newBuffer(informationalDestination, 8)
	informational.WriteHeader(http.StatusContinue)
	if informationalDestination.Code != http.StatusContinue || informational.status != 0 {
		t.Fatalf("100 response = %d, buffered status = %d", informationalDestination.Code, informational.status)
	}

	final := newBuffer(httptest.NewRecorder(), 8)
	final.WriteHeader(http.StatusOK)
	if final.status != http.StatusOK || final.committed == nil || final.spilled {
		t.Fatalf("200 response = status %d, committed %v, spilled %v", final.status, final.committed != nil, final.spilled)
	}
}

func TestExactBufferAndCompressionThresholds(t *testing.T) {
	t.Parallel()

	destination := httptest.NewRecorder()
	buffer := newBuffer(destination, 4)
	if written, err := buffer.Write([]byte("1234")); err != nil || written != 4 {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if buffer.spilled || buffer.buffer.String() != "1234" {
		t.Fatalf("exact buffer = spilled %v, payload %q", buffer.spilled, buffer.buffer.String())
	}
	buffer.commitIdentity()
	if destination.Body.String() != "1234" {
		t.Fatalf("identity payload = %q", destination.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	eligible := newBuffer(httptest.NewRecorder(), 4)
	eligible.status = http.StatusOK
	_, _ = eligible.buffer.WriteString("12")
	if !shouldCompress(request, eligible, 1, 1, 2, nil) {
		t.Fatal("response at minimum size was not compressible")
	}
	if shouldCompress(request, eligible, 0, 0, 1, nil) {
		t.Fatal("zero-quality gzip was compressible")
	}
}

func TestIdentityCommitWritesOnlyBufferedBytes(t *testing.T) {
	t.Parallel()

	emptyDestination := &countingWriter{header: make(http.Header)}
	empty := newBuffer(emptyDestination, 4)
	empty.commitIdentity()
	if emptyDestination.writes != 0 {
		t.Fatalf("empty commit writes = %d", emptyDestination.writes)
	}

	payloadDestination := &countingWriter{header: make(http.Header)}
	payload := newBuffer(payloadDestination, 4)
	_, _ = payload.buffer.WriteString("x")
	payload.commitIdentity()
	if payloadDestination.writes != 1 || payloadDestination.payload.String() != "x" {
		t.Fatalf("payload commit = %d writes, %q", payloadDestination.writes, payloadDestination.payload.String())
	}
}

func TestContentSniffingPreservesExplicitRepresentationMetadata(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		payload         []byte
		header          http.Header
		wantContentType bool
		wantExact       string
	}{
		"empty":             {header: make(http.Header)},
		"detected":          {payload: []byte("plain text"), header: make(http.Header), wantContentType: true},
		"explicit type":     {payload: []byte("plain text"), header: http.Header{"Content-Type": {"application/custom"}}, wantContentType: true, wantExact: "application/custom"},
		"explicit encoding": {payload: []byte("plain text"), header: http.Header{"Content-Encoding": {"br"}}},
	} {
		buffer := &responseBuffer{committed: test.header.Clone()}
		buffer.sniffContentType(test.payload)
		contentType := buffer.committed.Get("Content-Type")
		if (contentType != "") != test.wantContentType {
			t.Fatalf("%s Content-Type = %q", name, contentType)
		}
		if test.wantExact != "" && contentType != test.wantExact {
			t.Fatalf("%s Content-Type = %q, want %q", name, contentType, test.wantExact)
		}
	}
}

func TestCompressedTrailerFilteringContinuesAfterRepresentationMetadata(t *testing.T) {
	t.Parallel()

	destination := httptest.NewRecorder()
	buffer := newBuffer(destination, 8)
	buffer.header.Set("Trailer", "Digest, X-Checksum")
	buffer.commitHeader()
	buffer.header.Set("Digest", "sha-256=representation")
	buffer.header.Set("X-Checksum", "retained")
	buffer.spilled = true
	buffer.compressed = true
	buffer.finish()
	if destination.Header().Get("Digest") != "" || destination.Header().Get("X-Checksum") != "retained" {
		t.Fatalf("trailers = %v", destination.Header())
	}
}

func TestRepresentationOnlyTrailerDeclarationIsRemoved(t *testing.T) {
	t.Parallel()

	header := http.Header{"Trailer": {"Digest"}}
	removeRepresentationTrailers(header)
	if _, exists := header["Trailer"]; exists {
		t.Fatalf("Trailer key remains: %v", header)
	}
}

func TestNegotiationBudgetsAccumulateAcrossHeaderLines(t *testing.T) {
	t.Parallel()

	if gzipQuality, identityQuality, ok := negotiate([]string{"gzip", "identity"}, 12); !ok || gzipQuality != 1 || identityQuality != 1 {
		t.Fatalf("exact byte budget = %v, %v, %v", gzipQuality, identityQuality, ok)
	}
	if _, _, ok := negotiate([]string{"gzip", "identity"}, 11); ok {
		t.Fatal("cumulative byte budget was not enforced")
	}

	first := strings.Repeat("br,", 31) + "br"
	secondExact := strings.Repeat("br,", 30) + "br,gzip"
	if gzipQuality, _, ok := negotiate([]string{first, secondExact}, 1024); !ok || gzipQuality != 1 {
		t.Fatalf("exact item budget = %v, %v", gzipQuality, ok)
	}
	secondExcessive := strings.Repeat("br,", 31) + "br,gzip"
	if _, _, ok := negotiate([]string{first, secondExcessive}, 1024); ok {
		t.Fatal("cumulative item budget was not enforced")
	}
	tenItems := strings.Repeat("br,", 9) + "br"
	twentyItems := strings.Repeat("br,", 19) + "br"
	thirtyFiveItems := strings.Repeat("br,", 33) + "br,gzip"
	if _, _, ok := negotiate([]string{tenItems, twentyItems, thirtyFiveItems}, 1024); ok {
		t.Fatal("three-line cumulative item budget was not enforced")
	}
	if _, _, ok := negotiate([]string{"gzip; q"}, 128); ok {
		t.Fatal("quality parameter without equals was accepted")
	}
}

func TestAbsentWildcardLeavesIdentityAcceptable(t *testing.T) {
	t.Parallel()

	gzipQuality, identityQuality, ok := negotiate([]string{"gzip;q=0.5"}, 128)
	if !ok || gzipQuality != 0.5 || identityQuality != 1 {
		t.Fatalf("negotiate() = %v, %v, %v", gzipQuality, identityQuality, ok)
	}
}

type countingWriter struct {
	header  http.Header
	payload strings.Builder
	writes  int
}

func (w *countingWriter) Header() http.Header { return w.header }
func (*countingWriter) WriteHeader(int)       {}
func (w *countingWriter) Write(payload []byte) (int, error) {
	w.writes++
	return w.payload.Write(payload)
}
