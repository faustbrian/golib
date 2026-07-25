package httpx

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestTrackRecordsEveryWriterPathAndPreservesInterfaces(t *testing.T) {
	t.Parallel()
	underlying := &capableWriter{ResponseRecorder: httptest.NewRecorder()}
	wrapped, metrics := Track(underlying)
	wrapped.WriteHeader(http.StatusEarlyHints)
	wrapped.WriteHeader(http.StatusCreated)
	wrapped.WriteHeader(http.StatusAccepted)
	if metrics.Status != http.StatusCreated || !metrics.Committed {
		t.Fatalf("metrics = %#v", metrics)
	}
	if _, ok := wrapped.(http.Hijacker); !ok {
		t.Fatal("Hijacker missing")
	}
	if err := wrapped.(http.Pusher).Push("/asset", nil); err != nil || !underlying.pushed {
		t.Fatalf("Push() error = %v, pushed = %v", err, underlying.pushed)
	}
	dataWrapped, dataMetrics := Track(&capableWriter{ResponseRecorder: httptest.NewRecorder()})
	_, _ = dataWrapped.Write([]byte("ab"))
	_, _ = dataWrapped.(io.ReaderFrom).ReadFrom(strings.NewReader("cd"))
	dataWrapped.(http.Flusher).Flush()
	if dataMetrics.Bytes != 4 || dataMetrics.Status != http.StatusOK {
		t.Fatalf("data metrics = %#v", dataMetrics)
	}
	partialWrapped, partialMetrics := Track(&partialWriter{header: make(http.Header)})
	written, writeErr := partialWrapped.Write([]byte("partial"))
	if written != 2 || writeErr == nil || partialMetrics.Bytes != 2 || partialMetrics.Status != http.StatusOK {
		t.Fatalf("partial write = %d, %v; metrics = %#v", written, writeErr, partialMetrics)
	}

	hijackUnderlying := &capableWriter{ResponseRecorder: httptest.NewRecorder(), hijack: true}
	hijacked, hijackMetrics := Track(hijackUnderlying)
	connection, _, err := hijacked.(http.Hijacker).Hijack()
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if !hijackMetrics.Committed {
		t.Fatal("hijack not recorded")
	}
}

func TestWithPolicyAppliesAcrossCommitPaths(t *testing.T) {
	t.Parallel()
	paths := []func(http.ResponseWriter){
		func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
		func(w http.ResponseWriter) { _, _ = w.Write([]byte("x")) },
		func(w http.ResponseWriter) { _, _ = w.(io.ReaderFrom).ReadFrom(strings.NewReader("x")) },
		func(w http.ResponseWriter) { w.(http.Flusher).Flush() },
		func(w http.ResponseWriter) {
			connection, _, _ := w.(http.Hijacker).Hijack()
			if connection != nil {
				_ = connection.Close()
			}
		},
	}
	for index, path := range paths {
		underlying := &capableWriter{ResponseRecorder: httptest.NewRecorder(), hijack: true}
		calls := 0
		wrapped := WithPolicy(underlying, func(header http.Header) { calls++; header.Set("X-Policy", "yes") })
		path(wrapped)
		if calls == 0 || underlying.Header().Get("X-Policy") != "yes" {
			t.Fatalf("path %d calls = %d, headers = %v", index, calls, underlying.Header())
		}
	}
}

func TestAddVaryAndSafeErrorAreDeterministic(t *testing.T) {
	t.Parallel()
	header := http.Header{"Vary": []string{"Origin, accept-encoding", "Origin"}}
	AddVary(header, "Accept-Encoding", "Access-Control-Request-Method")
	if got := header.Values("Vary"); !reflect.DeepEqual(got, []string{"Origin, accept-encoding, Access-Control-Request-Method"}) {
		t.Fatalf("Vary = %v", got)
	}
	recorder := httptest.NewRecorder()
	SafeError(recorder, http.StatusBadRequest, "bad\n")
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "bad\n" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

func TestCheckWriteHeaderCodeMatchesNetHTTPRange(t *testing.T) {
	t.Parallel()
	for _, status := range []int{100, 200, 999} {
		CheckWriteHeaderCode(status)
	}
	for _, status := range []int{0, 99, 1000} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("status did not panic")
				}
			}()
			CheckWriteHeaderCode(status)
		})
	}
}

type capableWriter struct {
	*httptest.ResponseRecorder
	hijack, pushed bool
}

func (w *capableWriter) ReadFrom(reader io.Reader) (int64, error) {
	return io.Copy(w.ResponseRecorder, reader)
}
func (w *capableWriter) Push(string, *http.PushOptions) error {
	w.pushed = true
	return nil
}
func (w *capableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.hijack {
		return nil, nil, http.ErrNotSupported
	}
	server, client := net.Pipe()
	_ = client.Close()
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}

type partialWriter struct{ header http.Header }

func (w *partialWriter) Header() http.Header     { return w.header }
func (*partialWriter) WriteHeader(int)           {}
func (*partialWriter) Write([]byte) (int, error) { return 2, io.ErrShortWrite }
