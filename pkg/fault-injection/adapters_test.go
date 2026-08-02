package faultinject_test

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestListenerClosesConnectionsRejectedAfterAccept(t *testing.T) {
	t.Parallel()

	connection := &trackingConn{}
	listener := &stubListener{connection: connection}
	wrapped, err := faultinject.WrapListener(listener, scopedInjector(t, faultinject.BoundaryListen,
		faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)), 1)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := wrapped.Accept()
	if accepted != nil || !errors.Is(err, errInjected) || !connection.closed {
		t.Fatalf("Accept() = %v, %v; closed=%t", accepted, err, connection.closed)
	}
	if wrapped.Addr().String() != "listener" {
		t.Fatalf("Addr() = %v", wrapped.Addr())
	}
	if err := wrapped.Close(); err != nil || !listener.closed {
		t.Fatalf("Close() = %v, closed=%t", err, listener.closed)
	}
}

func TestFilesystemPreservesOpenReadAndCloseOwnership(t *testing.T) {
	t.Parallel()

	base := &trackingFS{file: &trackingFile{data: []byte("data")}}
	injector := scopedInjector(t, faultinject.BoundaryFilesystemRead,
		faultinject.ByteFault(faultinject.KindShortRead, faultinject.PhaseDuring, 2, 0))
	wrapped, err := faultinject.WrapFS(base, injector, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	file, err := wrapped.Open("safe.txt")
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	n, err := file.Read(buffer)
	if err != nil || string(buffer[:n]) != "da" {
		t.Fatalf("Read() = %q, %v", buffer[:n], err)
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if err := file.Close(); err != nil || !base.file.closed {
		t.Fatalf("Close() = %v, closed=%t", err, base.file.closed)
	}
}

func TestFilesystemClosesFileRejectedAfterOpen(t *testing.T) {
	t.Parallel()

	base := &trackingFS{file: &trackingFile{data: []byte("data")}}
	wrapped, err := faultinject.WrapFS(base, scopedInjector(t, faultinject.BoundaryFilesystemOpen,
		faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	file, err := wrapped.Open("safe.txt")
	if file != nil || !errors.Is(err, errInjected) || !base.file.closed {
		t.Fatalf("Open() = %v, %v; closed=%t", file, err, base.file.closed)
	}
}

func TestSleeperAndTimerFactoryUseExplicitClockBoundary(t *testing.T) {
	t.Parallel()

	baseSleeper := &recordingSleeper{}
	wrappedSleeper, err := faultinject.WrapSleeper(baseSleeper, scopedInjector(t, faultinject.BoundaryClock,
		faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrappedSleeper.Sleep(context.Background(), time.Second); !errors.Is(err, errInjected) || len(baseSleeper.delays) != 0 {
		t.Fatalf("Sleep() = %v, base calls %v", err, baseSleeper.delays)
	}

	baseFactory := &stubTimerFactory{timer: &stubTimer{channel: make(chan time.Time)}}
	wrappedFactory, err := faultinject.WrapTimerFactory(baseFactory, scopedInjector(t, faultinject.BoundaryClock,
		faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)), 2)
	if err != nil {
		t.Fatal(err)
	}
	timer, err := wrappedFactory.NewTimer(context.Background(), time.Second)
	if timer != nil || !errors.Is(err, errInjected) || !baseFactory.timer.stopped {
		t.Fatalf("NewTimer() = %v, %v; stopped=%t", timer, err, baseFactory.timer.stopped)
	}
}

func TestTimerFactoryDuringCancellationDelegatesWithEndedContextAndCleansUp(t *testing.T) {
	t.Parallel()

	baseTimer := &stubTimer{channel: make(chan time.Time)}
	called := false
	base := timerFactoryFunc(func(ctx context.Context, _ time.Duration) (faultinject.Timer, error) {
		called = true
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("factory context error = %v", ctx.Err())
		}
		return baseTimer, nil
	})
	wrapped, err := faultinject.WrapTimerFactory(base, scopedInjector(t, faultinject.BoundaryClock,
		faultinject.CancelFault(faultinject.PhaseDuring)), 1)
	if err != nil {
		t.Fatal(err)
	}

	timer, err := wrapped.NewTimer(context.Background(), time.Second)
	if timer != nil || !errors.Is(err, context.Canceled) || !called || !baseTimer.stopped {
		t.Fatalf("NewTimer() = %v, %v; called=%t, stopped=%t", timer, err, called, baseTimer.stopped)
	}
}

type stubListener struct {
	connection net.Conn
	closed     bool
}

func (listener *stubListener) Accept() (net.Conn, error) { return listener.connection, nil }
func (listener *stubListener) Close() error              { listener.closed = true; return nil }
func (*stubListener) Addr() net.Addr                     { return stubAddr("listener") }

type trackingFS struct{ file *trackingFile }

func (filesystem *trackingFS) Open(string) (fs.File, error) { return filesystem.file, nil }

type trackingFile struct {
	data   []byte
	offset int
	closed bool
}

func (file *trackingFile) Read(buffer []byte) (int, error) {
	if file.offset >= len(file.data) {
		return 0, nil
	}
	n := copy(buffer, file.data[file.offset:])
	file.offset += n
	return n, nil
}
func (file *trackingFile) Close() error          { file.closed = true; return nil }
func (*trackingFile) Stat() (fs.FileInfo, error) { return stubFileInfo{}, nil }

type stubFileInfo struct{}

func (stubFileInfo) Name() string       { return "safe.txt" }
func (stubFileInfo) Size() int64        { return 4 }
func (stubFileInfo) Mode() fs.FileMode  { return 0 }
func (stubFileInfo) ModTime() time.Time { return time.Time{} }
func (stubFileInfo) IsDir() bool        { return false }
func (stubFileInfo) Sys() any           { return nil }

type stubTimerFactory struct{ timer *stubTimer }

func (factory *stubTimerFactory) NewTimer(context.Context, time.Duration) (faultinject.Timer, error) {
	return factory.timer, nil
}

type timerFactoryFunc func(context.Context, time.Duration) (faultinject.Timer, error)

func (function timerFactoryFunc) NewTimer(ctx context.Context, delay time.Duration) (faultinject.Timer, error) {
	return function(ctx, delay)
}

type stubTimer struct {
	channel chan time.Time
	stopped bool
}

func (timer *stubTimer) C() <-chan time.Time { return timer.channel }
func (timer *stubTimer) Stop() bool          { timer.stopped = true; return true }
func (*stubTimer) Reset(time.Duration) bool  { return true }
