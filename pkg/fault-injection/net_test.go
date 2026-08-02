package faultinject_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestConnPreservesContractAndAppliesResetAndHalfClose(t *testing.T) {
	t.Parallel()

	t.Run("read reset closes connection", func(t *testing.T) {
		t.Parallel()
		base := &trackingConn{}
		conn := faultinject.WrapConn(base, scopedInjector(t, faultinject.BoundaryConn,
			faultinject.ByteFault(faultinject.KindReset, faultinject.PhaseBefore, 0, 0)), 1, 2)
		n, err := conn.Read(make([]byte, 1))
		if n != 0 || !errors.Is(err, faultinject.ErrConnectionReset) || !base.closed {
			t.Fatalf("Read() = %d, %v; closed=%t", n, err, base.closed)
		}
	})

	t.Run("write half-close uses CloseWrite", func(t *testing.T) {
		t.Parallel()
		base := &trackingConn{}
		conn := faultinject.WrapConn(base, scopedInjector(t, faultinject.BoundaryConn,
			faultinject.ByteFault(faultinject.KindHalfClose, faultinject.PhaseBefore, 0, 0)), 1, 2)
		n, err := conn.Write([]byte("x"))
		if n != 0 || !errors.Is(err, faultinject.ErrHalfClosed) || !base.writeClosed {
			t.Fatalf("Write() = %d, %v; writeClosed=%t", n, err, base.writeClosed)
		}
	})

	t.Run("disabled returns original", func(t *testing.T) {
		t.Parallel()
		base := &trackingConn{}
		if got := faultinject.WrapConn(base, nil, 1, 2); got != base {
			t.Fatal("disabled conn wrapper changed boundary")
		}
	})
}

func TestDialerClosesConnectionsRejectedAfterEstablishment(t *testing.T) {
	t.Parallel()

	baseConn := &trackingConn{}
	base := faultinject.DialContextFunc(func(context.Context, string, string) (net.Conn, error) {
		return baseConn, nil
	})
	dial := faultinject.WrapDialer(base, scopedInjector(t, faultinject.BoundaryDial,
		faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)), 1)
	conn, err := dial(context.Background(), "tcp", "example.test:80")
	if conn != nil || !errors.Is(err, errInjected) || !baseConn.closed {
		t.Fatalf("Dial() = %v, %v; closed=%t", conn, err, baseConn.closed)
	}
}

type trackingConn struct {
	closed      bool
	readClosed  bool
	writeClosed bool
}

func (*trackingConn) Read([]byte) (int, error)         { return 0, nil }
func (*trackingConn) Write(buffer []byte) (int, error) { return len(buffer), nil }
func (conn *trackingConn) Close() error                { conn.closed = true; return nil }
func (*trackingConn) LocalAddr() net.Addr              { return stubAddr("local") }
func (*trackingConn) RemoteAddr() net.Addr             { return stubAddr("remote") }
func (*trackingConn) SetDeadline(time.Time) error      { return nil }
func (*trackingConn) SetReadDeadline(time.Time) error  { return nil }
func (*trackingConn) SetWriteDeadline(time.Time) error { return nil }
func (conn *trackingConn) CloseRead() error            { conn.readClosed = true; return nil }
func (conn *trackingConn) CloseWrite() error           { conn.writeClosed = true; return nil }

type stubAddr string

func (address stubAddr) Network() string { return string(address) }
func (address stubAddr) String() string  { return string(address) }
