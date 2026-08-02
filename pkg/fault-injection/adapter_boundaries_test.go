package faultinject_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestFilesystemValidationPassThroughAndFailurePhases(t *testing.T) {
	t.Parallel()

	if _, err := faultinject.WrapFS(nil, nil, 1, 2); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("nil FS error = %v", err)
	}
	base := &trackingFS{file: &trackingFile{data: []byte("x")}}
	disabled, err := faultinject.WrapFS(base, nil, 1, 2)
	if err != nil || disabled != base {
		t.Fatalf("disabled FS = %T, %v", disabled, err)
	}
	for _, phase := range []faultinject.Phase{faultinject.PhaseBefore, faultinject.PhaseDuring} {
		wrapped, err := faultinject.WrapFS(base, scopedInjector(t, faultinject.BoundaryFilesystemOpen,
			faultinject.ErrorFault(phase, errInjected)), 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if file, err := wrapped.Open("safe"); file != nil || !errors.Is(err, errInjected) {
			t.Fatalf("phase %v Open() = %v, %v", phase, file, err)
		}
	}
	for _, result := range []struct {
		file fs.File
		err  error
	}{{err: errInjected}, {}} {
		wrapped, err := faultinject.WrapFS(fsFunc(func(string) (fs.File, error) { return result.file, result.err }),
			injectorWithConfig(t, faultinject.Config{}), 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		file, openError := wrapped.Open("safe")
		if file != result.file || !errors.Is(openError, result.err) {
			t.Fatalf("organic Open() = %v, %v", file, openError)
		}
	}
}

func TestHTTPNoMatchDuringAndOrganicPaths(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
	organic := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errInjected })
	transport, err := faultinject.NewRoundTripper(organic, injectorWithConfig(t, faultinject.Config{}), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := transport.RoundTrip(request); response != nil || !errors.Is(err, errInjected) {
		t.Fatalf("organic RoundTrip() = %v, %v", response, err)
	}

	transport, err = faultinject.NewRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}), injectorWithConfig(t, faultinject.Config{}), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := transport.RoundTrip(request); err != nil || response.Body != nil {
		t.Fatalf("bodyless RoundTrip() = %v, %v", response, err)
	}

	body := &trackingReadCloser{Reader: bytes.NewReader(nil)}
	transport, err = faultinject.NewRoundTripper(roundTripFunc(func(cloned *http.Request) (*http.Response, error) {
		if !errors.Is(cloned.Context().Err(), context.Canceled) {
			t.Fatalf("during context error = %v", cloned.Context().Err())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}), scopedInjector(t, faultinject.BoundaryHTTP, faultinject.CancelFault(faultinject.PhaseDuring)), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := transport.RoundTrip(request); response != nil || !errors.Is(err, context.Canceled) || !body.closed {
		t.Fatalf("during RoundTrip() = %v, %v; closed=%t", response, err, body.closed)
	}

	body = &trackingReadCloser{Reader: bytes.NewReader(nil)}
	transport, err = faultinject.NewRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, errInjected
	}), scopedInjector(t, faultinject.BoundaryHTTP, faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond)), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if response == nil || !errors.Is(err, errInjected) {
		t.Fatalf("organic response/error = %v, %v", response, err)
	}
	_ = response.Body.Close()
}

func TestIOErrorPhasesAndWriterFailures(t *testing.T) {
	t.Parallel()

	for _, phase := range []faultinject.Phase{faultinject.PhaseDuring, faultinject.PhaseAfter} {
		reader := faultinject.WrapReader(bytes.NewBufferString("x"), scopedInjector(t, faultinject.BoundaryReader,
			faultinject.ErrorFault(phase, errInjected)), 1)
		n, err := reader.Read(make([]byte, 1))
		wantN := 0
		if phase == faultinject.PhaseAfter {
			wantN = 1
		}
		if n != wantN || !errors.Is(err, errInjected) {
			t.Fatalf("reader phase %v = %d, %v", phase, n, err)
		}
	}
	writer := faultinject.WrapWriter(io.Discard, scopedInjector(t, faultinject.BoundaryWriter,
		faultinject.ErrorFault(faultinject.PhaseDuring, errInjected)), 1)
	if n, err := writer.Write([]byte("x")); n != 0 || !errors.Is(err, errInjected) {
		t.Fatalf("during writer = %d, %v", n, err)
	}

	duplicateFailure := &sequenceWriter{errors: []error{nil, errInjected}}
	writer = faultinject.WrapWriter(duplicateFailure, scopedInjector(t, faultinject.BoundaryWriter,
		faultinject.ByteFault(faultinject.KindDuplicate, faultinject.PhaseAfter, 1, 0)), 1)
	if n, err := writer.Write([]byte("x")); n != 1 || !errors.Is(err, errInjected) {
		t.Fatalf("duplicate writer = %d, %v", n, err)
	}

	organicFailure := &sequenceWriter{errors: []error{errInjected}}
	writer = faultinject.WrapWriter(organicFailure, scopedInjector(t, faultinject.BoundaryWriter,
		faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseDuring, 1, 1)), 1)
	if n, err := writer.Write([]byte("x")); n != 1 || !errors.Is(err, errInjected) {
		t.Fatalf("organic writer = %d, %v", n, err)
	}

	networkReader := faultinject.WrapReader(bytes.NewReader(nil), scopedInjector(t, faultinject.BoundaryReader,
		faultinject.ByteFault(faultinject.KindTemporary, faultinject.PhaseBefore, 0, 0)), 1)
	_, networkError := networkReader.Read(make([]byte, 1))
	if networkError.Error() == "" || !errors.Is(networkError, faultinject.ErrTemporaryNetwork) {
		t.Fatalf("network error = %v", networkError)
	}
}

func TestListenerAndDialerAllOwnershipPaths(t *testing.T) {
	t.Parallel()

	if _, err := faultinject.WrapListener(nil, nil, 1); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("nil listener error = %v", err)
	}
	baseListener := &stubListener{connection: &trackingConn{}}
	disabled, err := faultinject.WrapListener(baseListener, nil, 1)
	if err != nil || disabled != baseListener {
		t.Fatalf("disabled listener = %T, %v", disabled, err)
	}
	for _, phase := range []faultinject.Phase{faultinject.PhaseBefore, faultinject.PhaseDuring} {
		wrapped, err := faultinject.WrapListener(baseListener, scopedInjector(t, faultinject.BoundaryListen,
			faultinject.ErrorFault(phase, errInjected)), 1)
		if err != nil {
			t.Fatal(err)
		}
		if connection, err := wrapped.Accept(); connection != nil || !errors.Is(err, errInjected) {
			t.Fatalf("listener phase %v = %v, %v", phase, connection, err)
		}
	}
	listenerError := &errorListener{err: errInjected}
	wrapped, err := faultinject.WrapListener(listenerError, injectorWithConfig(t, faultinject.Config{}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if connection, err := wrapped.Accept(); connection != nil || !errors.Is(err, errInjected) {
		t.Fatalf("organic Accept() = %v, %v", connection, err)
	}
	successListener := &stubListener{connection: &trackingConn{}}
	latencyRule := ruleWithFault("listener-latency", faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond))
	latencyRule.Scope = faultinject.BoundaryListen
	wrapped, err = faultinject.WrapListener(successListener, injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{latencyRule}}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if connection, err := wrapped.Accept(); connection == nil || err != nil {
		t.Fatalf("successful Accept() = %v, %v", connection, err)
	}

	baseDial := faultinject.DialContextFunc(func(context.Context, string, string) (net.Conn, error) { return &trackingConn{}, errInjected })
	disabledDial := faultinject.WrapDialer(baseDial, nil, 1)
	if connection, err := disabledDial(context.Background(), "tcp", "x"); connection == nil || !errors.Is(err, errInjected) {
		t.Fatalf("disabled dial = %v, %v", connection, err)
	}
	activeNoMatch := faultinject.WrapDialer(baseDial, injectorWithConfig(t, faultinject.Config{}), 1)
	if connection, err := activeNoMatch(context.Background(), "tcp", "x"); connection == nil || !errors.Is(err, errInjected) {
		t.Fatalf("organic dial = %v, %v", connection, err)
	}
	for _, phase := range []faultinject.Phase{faultinject.PhaseBefore, faultinject.PhaseDuring} {
		dial := faultinject.WrapDialer(baseDial, scopedInjector(t, faultinject.BoundaryDial,
			faultinject.ErrorFault(phase, errInjected)), 1)
		if connection, err := dial(context.Background(), "tcp", "x"); connection != nil || !errors.Is(err, errInjected) {
			t.Fatalf("dial phase %v = %v, %v", phase, connection, err)
		}
	}
	successDialRule := ruleWithFault("dial-latency", faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond))
	successDialRule.Scope = faultinject.BoundaryDial
	successDial := faultinject.WrapDialer(faultinject.DialContextFunc(func(context.Context, string, string) (net.Conn, error) {
		return &trackingConn{}, nil
	}), injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{successDialRule}}), 1)
	if connection, err := successDial(context.Background(), "tcp", "x"); connection == nil || err != nil {
		t.Fatalf("successful dial = %v, %v", connection, err)
	}
}

func TestConnDelegationAndHalfCloseFallbacks(t *testing.T) {
	t.Parallel()

	base := &trackingConn{}
	conn := faultinject.WrapConn(base, scopedInjector(t, faultinject.BoundaryConn,
		faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)), 1, 2)
	_, _ = conn.Read(make([]byte, 1))
	if base.closed {
		t.Fatal("ordinary injected error closed connection")
	}
	deadline := time.Now()
	if err := conn.SetDeadline(deadline); err != nil {
		t.Fatalf("SetDeadline() = %v", err)
	}

	readBase := &trackingConn{}
	conn = faultinject.WrapConn(readBase, scopedInjector(t, faultinject.BoundaryConn,
		faultinject.ByteFault(faultinject.KindHalfClose, faultinject.PhaseBefore, 0, 0)), 1, 2)
	_, _ = conn.Read(make([]byte, 1))
	if !readBase.readClosed {
		t.Fatal("read half-close did not use CloseRead")
	}

	fallback := &basicConn{}
	conn = faultinject.WrapConn(fallback, scopedInjector(t, faultinject.BoundaryConn,
		faultinject.ByteFault(faultinject.KindHalfClose, faultinject.PhaseBefore, 0, 0)), 1, 2)
	_, _ = conn.Write([]byte("x"))
	if !fallback.closed {
		t.Fatal("unsupported half-close did not close connection")
	}
}

func TestTimeAdaptersValidationPassThroughAndSuccess(t *testing.T) {
	t.Parallel()

	if _, err := faultinject.WrapSleeper(nil, nil, 1); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("nil sleeper error = %v", err)
	}
	baseSleeper := &recordingSleeper{}
	disabledSleeper, err := faultinject.WrapSleeper(baseSleeper, nil, 1)
	if err != nil || !reflect.DeepEqual(disabledSleeper, baseSleeper) {
		t.Fatalf("disabled sleeper = %T, %v", disabledSleeper, err)
	}
	wrappedSleeper, err := faultinject.WrapSleeper(baseSleeper, injectorWithConfig(t, faultinject.Config{}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrappedSleeper.Sleep(context.Background(), time.Millisecond); err != nil || len(baseSleeper.delays) != 1 {
		t.Fatalf("Sleep() = %v, delays=%v", err, baseSleeper.delays)
	}

	if _, err := faultinject.WrapTimerFactory(nil, nil, 1); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("nil timer factory error = %v", err)
	}
	baseFactory := &stubTimerFactory{timer: &stubTimer{channel: make(chan time.Time)}}
	disabledFactory, err := faultinject.WrapTimerFactory(baseFactory, nil, 1)
	if err != nil || !reflect.DeepEqual(disabledFactory, baseFactory) {
		t.Fatalf("disabled timer factory = %T, %v", disabledFactory, err)
	}
	for _, phase := range []faultinject.Phase{faultinject.PhaseBefore, faultinject.PhaseDuring} {
		wrapped, err := faultinject.WrapTimerFactory(baseFactory, scopedInjector(t, faultinject.BoundaryClock,
			faultinject.ErrorFault(phase, errInjected)), 1)
		if err != nil {
			t.Fatal(err)
		}
		if timer, err := wrapped.NewTimer(context.Background(), time.Second); timer != nil || !errors.Is(err, errInjected) {
			t.Fatalf("timer phase %v = %v, %v", phase, timer, err)
		}
	}
	organicFactory := &errorTimerFactory{err: errInjected}
	wrappedFactory, err := faultinject.WrapTimerFactory(organicFactory, injectorWithConfig(t, faultinject.Config{}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if timer, err := wrappedFactory.NewTimer(context.Background(), time.Second); timer != nil || !errors.Is(err, errInjected) {
		t.Fatalf("organic timer = %v, %v", timer, err)
	}
}

type fsFunc func(string) (fs.File, error)

func (function fsFunc) Open(name string) (fs.File, error) { return function(name) }

type sequenceWriter struct {
	calls  int
	errors []error
}

func (writer *sequenceWriter) Write(buffer []byte) (int, error) {
	err := writer.errors[writer.calls]
	writer.calls++
	return len(buffer), err
}

type errorListener struct{ err error }

func (listener *errorListener) Accept() (net.Conn, error) { return nil, listener.err }
func (*errorListener) Close() error                       { return nil }
func (*errorListener) Addr() net.Addr                     { return stubAddr("error") }

type basicConn struct{ closed bool }

func (*basicConn) Read([]byte) (int, error)         { return 0, nil }
func (*basicConn) Write(buffer []byte) (int, error) { return len(buffer), nil }
func (conn *basicConn) Close() error                { conn.closed = true; return nil }
func (*basicConn) LocalAddr() net.Addr              { return stubAddr("local") }
func (*basicConn) RemoteAddr() net.Addr             { return stubAddr("remote") }
func (*basicConn) SetDeadline(time.Time) error      { return nil }
func (*basicConn) SetReadDeadline(time.Time) error  { return nil }
func (*basicConn) SetWriteDeadline(time.Time) error { return nil }

type errorTimerFactory struct{ err error }

func (factory *errorTimerFactory) NewTimer(context.Context, time.Duration) (faultinject.Timer, error) {
	return nil, factory.err
}
