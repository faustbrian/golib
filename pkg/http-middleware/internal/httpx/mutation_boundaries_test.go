package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseQualityAcceptsExactDigitBounds(t *testing.T) {
	t.Parallel()

	if quality, ok := ParseQuality("0.09"); !ok || quality != 0.09 {
		t.Fatalf("ParseQuality(0.09) = %v, %v", quality, ok)
	}
}

func TestValidFieldValueAcceptsExactLengthBound(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("a", 8)
	if !ValidFieldValue(value, len(value)) {
		t.Fatal("exact-length field value was rejected")
	}
}

func TestTrackDistinguishesExactInformationalAndFinalStatusBounds(t *testing.T) {
	t.Parallel()

	wrapped, recorder := Track(httptest.NewRecorder())
	wrapped.WriteHeader(http.StatusContinue)
	if recorder.Committed || recorder.Status != 0 {
		t.Fatalf("100 response metrics = %#v", recorder)
	}
	wrapped.WriteHeader(http.StatusOK)
	if !recorder.Committed || recorder.Status != http.StatusOK {
		t.Fatalf("200 response metrics = %#v", recorder)
	}
}

func TestTrackAccumulatesRepeatedWrites(t *testing.T) {
	t.Parallel()

	wrapped, recorder := Track(httptest.NewRecorder())
	_, _ = wrapped.Write([]byte("ab"))
	_, _ = wrapped.Write([]byte("cd"))
	if recorder.Bytes != 4 {
		t.Fatalf("recorded bytes = %d", recorder.Bytes)
	}
}

func TestAddVaryLeavesEmptyHeaderAbsent(t *testing.T) {
	t.Parallel()

	header := make(http.Header)
	AddVary(header)
	if _, exists := header["Vary"]; exists {
		t.Fatalf("empty Vary header exists: %v", header)
	}
}
