package faultinject

import (
	"context"
	"errors"
	"net"
	"time"
)

// WrapListener injects bounded accept faults while delegating address and
// close ownership to base. Connections rejected after acceptance are closed.
func WrapListener(base net.Listener, injector *Injector, operation uint32) (net.Listener, error) {
	if base == nil {
		return nil, invalid("Listener", "must be non-nil")
	}
	if injector == nil || !injector.enabled {
		return base, nil
	}
	return &injectedListener{Listener: base, injector: injector, operation: operation}, nil
}

type injectedListener struct {
	net.Listener
	injector  *Injector
	operation uint32
}

func (listener *injectedListener) Accept() (net.Conn, error) {
	decision := listener.injector.Decide(Metadata{Boundary: BoundaryListen, Operation: listener.operation})
	if !decision.Injected() {
		return listener.Listener.Accept()
	}
	if err := faultPhaseError(context.Background(), decision.faults, PhaseBefore, listener.injector.sleeper); err != nil {
		return nil, err
	}
	if err := faultPhaseError(context.Background(), decision.faults, PhaseDuring, listener.injector.sleeper); err != nil {
		return nil, err
	}
	connection, organicError := listener.Listener.Accept()
	if err := faultPhaseError(context.Background(), decision.faults, PhaseAfter, listener.injector.sleeper); err != nil {
		closeConnection(connection)
		return nil, err
	}
	return connection, organicError
}

// DialContextFunc matches net.Dialer's DialContext method.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// WrapDialer injects bounded establishment faults. A connection rejected by a
// during or after fault is closed before the injected error is returned.
func WrapDialer(base DialContextFunc, injector *Injector, operation uint32) DialContextFunc {
	if injector == nil || !injector.enabled {
		return base
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		decision := injector.Decide(Metadata{Boundary: BoundaryDial, Operation: operation})
		if !decision.Injected() {
			return base(ctx, network, address)
		}
		if err := faultPhaseError(ctx, decision.faults, PhaseBefore, injector.sleeper); err != nil {
			return nil, err
		}
		operationContext, cleanup, duringError := prepareDuring(ctx, injector.sleeper, decision.faults)
		defer cleanup()
		connection, organicError := base(operationContext, network, address)
		if duringError != nil {
			closeConnection(connection)
			return nil, duringError
		}
		if err := faultPhaseError(ctx, decision.faults, PhaseAfter, injector.sleeper); err != nil {
			closeConnection(connection)
			return nil, err
		}
		return connection, organicError
	}
}

// WrapConn preserves the net.Conn deadline, address, close, and concurrent
// read/write surface while applying independent operation identifiers.
func WrapConn(connection net.Conn, injector *Injector, readOperation, writeOperation uint32) net.Conn {
	if injector == nil || !injector.enabled {
		return connection
	}
	return &injectedConn{
		Conn: connection,
		reader: &injectedReader{
			reader: connection, injector: injector,
			operation: readOperation, boundary: BoundaryConn,
		},
		writer: &injectedWriter{
			writer: connection, injector: injector,
			operation: writeOperation, boundary: BoundaryConn,
		},
	}
}

type injectedConn struct {
	net.Conn
	reader *injectedReader
	writer *injectedWriter
}

func (connection *injectedConn) Read(buffer []byte) (int, error) {
	n, err := connection.reader.Read(buffer)
	connection.applyClose(err, true)
	return n, err
}

func (connection *injectedConn) Write(buffer []byte) (int, error) {
	n, err := connection.writer.Write(buffer)
	connection.applyClose(err, false)
	return n, err
}

func (connection *injectedConn) applyClose(err error, read bool) {
	if errors.Is(err, ErrConnectionReset) {
		_ = connection.Close()
		return
	}
	if !errors.Is(err, ErrHalfClosed) {
		return
	}
	if read {
		if closer, ok := connection.Conn.(interface{ CloseRead() error }); ok {
			_ = closer.CloseRead()
			return
		}
	} else if closer, ok := connection.Conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
		return
	}
	_ = connection.Close()
}

func (connection *injectedConn) SetDeadline(deadline time.Time) error {
	return connection.Conn.SetDeadline(deadline)
}

func closeConnection(connection net.Conn) {
	if connection != nil {
		_ = connection.Close()
	}
}
