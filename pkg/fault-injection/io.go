package faultinject

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var (
	// ErrDropped reports bytes intentionally consumed or discarded by a rule.
	ErrDropped = errors.New("faultinject: bytes dropped")
	// ErrStreamInterrupted reports a bounded partial stream operation.
	ErrStreamInterrupted = errors.New("faultinject: stream interrupted")
	// ErrConnectionReset reports an injected connection reset.
	ErrConnectionReset = errors.New("faultinject: connection reset")
	// ErrHalfClosed reports an injected half-close.
	ErrHalfClosed = errors.New("faultinject: connection half-closed")
	// ErrTemporaryNetwork reports an injected retryable network failure.
	ErrTemporaryNetwork = errors.New("faultinject: temporary network failure")
	// ErrPermanentNetwork reports an injected permanent network failure.
	ErrPermanentNetwork = errors.New("faultinject: permanent network failure")
)

// NetworkError classifies an injected network failure without changing the
// underlying adapter's organic error values.
type NetworkError struct {
	cause     error
	temporary bool
}

func (e *NetworkError) Error() string   { return e.cause.Error() }
func (e *NetworkError) Unwrap() error   { return e.cause }
func (e *NetworkError) Timeout() bool   { return false }
func (e *NetworkError) Temporary() bool { return e.temporary }

// WrapReader wraps reader only when injector is active. Byte faults are
// bounded by their configured limit, and duplicated data is retained in a
// bounded private buffer.
func WrapReader(reader io.Reader, injector *Injector, operation uint32) io.Reader {
	if injector == nil || !injector.enabled {
		return reader
	}
	return &injectedReader{reader: reader, injector: injector, operation: operation, boundary: BoundaryReader}
}

type injectedReader struct {
	reader      io.Reader
	injector    *Injector
	operation   uint32
	boundary    Boundary
	attempt     atomic.Uint64
	duplicateMu sync.Mutex
	duplicate   []byte
}

func (reader *injectedReader) Read(buffer []byte) (int, error) {
	attempt := reader.attempt.Add(1)
	reader.duplicateMu.Lock()
	if len(reader.duplicate) != 0 {
		n := copy(buffer, reader.duplicate)
		reader.duplicate = reader.duplicate[n:]
		reader.duplicateMu.Unlock()
		return n, nil
	}
	reader.duplicateMu.Unlock()

	decision := reader.injector.Decide(Metadata{Boundary: reader.boundary, Operation: reader.operation, Attempt: attempt})
	if err := ioPhaseError(decision.faults, PhaseBefore, reader.injector.sleeper); err != nil {
		return 0, err
	}
	readBuffer := boundedBuffer(buffer, decision.faults, true)
	if err := ioPhaseError(decision.faults, PhaseDuring, reader.injector.sleeper); err != nil {
		return 0, err
	}
	n, organicError := reader.reader.Read(readBuffer)
	n, injectedError := reader.transform(readBuffer[:n], decision.faults)
	if injectedError != nil {
		return n, injectedError
	}
	if err := ioPhaseError(decision.faults, PhaseAfter, reader.injector.sleeper); err != nil {
		return n, err
	}
	return n, organicError
}

func (reader *injectedReader) transform(data []byte, faults []Fault) (int, error) {
	n := len(data)
	for _, fault := range faults {
		switch fault.Kind {
		case KindCorrupt:
			if fault.phase == PhaseAfter && n != 0 {
				data[0] ^= fault.mask
			}
		case KindReorder:
			if fault.phase == PhaseAfter && n != 0 {
				reverse(data[:min(n, fault.limit)])
			}
		case KindDrop:
			if fault.phase == PhaseAfter && n != 0 {
				return 0, ErrDropped
			}
		case KindDuplicate:
			if fault.phase == PhaseAfter && n != 0 {
				reader.duplicateMu.Lock()
				reader.duplicate = append(reader.duplicate[:0], data[:min(n, fault.limit)]...)
				reader.duplicateMu.Unlock()
			}
		case KindInterrupt:
			if fault.phase == PhaseDuring && n != 0 {
				return n, ErrStreamInterrupted
			}
		}
	}
	return n, nil
}

// WrapWriter wraps writer only when injector is active. Transforming faults
// operate on at most their declared limit and report short writes for larger
// caller buffers.
func WrapWriter(writer io.Writer, injector *Injector, operation uint32) io.Writer {
	if injector == nil || !injector.enabled {
		return writer
	}
	return &injectedWriter{writer: writer, injector: injector, operation: operation, boundary: BoundaryWriter}
}

type injectedWriter struct {
	writer    io.Writer
	injector  *Injector
	operation uint32
	boundary  Boundary
	attempt   atomic.Uint64
}

func (writer *injectedWriter) Write(buffer []byte) (int, error) {
	attempt := writer.attempt.Add(1)
	decision := writer.injector.Decide(Metadata{Boundary: writer.boundary, Operation: writer.operation, Attempt: attempt})
	if err := ioPhaseError(decision.faults, PhaseBefore, writer.injector.sleeper); err != nil {
		return 0, err
	}
	for _, fault := range decision.faults {
		if fault.Kind == KindDrop && fault.phase == PhaseDuring {
			return len(buffer), nil
		}
	}
	writeBuffer, transformed := transformWriteBuffer(buffer, decision.faults)
	if err := ioPhaseError(decision.faults, PhaseDuring, writer.injector.sleeper); err != nil {
		return 0, err
	}
	n, organicError := writer.writer.Write(writeBuffer)
	for _, fault := range decision.faults {
		switch fault.Kind {
		case KindInterrupt:
			if fault.phase == PhaseDuring {
				return n, ErrStreamInterrupted
			}
		case KindDuplicate:
			if fault.phase == PhaseAfter && n >= len(writeBuffer) {
				if _, err := writer.writer.Write(writeBuffer[:min(len(writeBuffer), fault.limit)]); err != nil {
					return n, err
				}
			}
		}
	}
	if err := ioPhaseError(decision.faults, PhaseAfter, writer.injector.sleeper); err != nil {
		return n, err
	}
	if organicError != nil {
		return n, organicError
	}
	if transformed && len(writeBuffer) < len(buffer) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func boundedBuffer(buffer []byte, faults []Fault, reader bool) []byte {
	limit := len(buffer)
	for _, fault := range faults {
		if fault.phase != PhaseDuring {
			continue
		}
		if (reader && (fault.Kind == KindShortRead || fault.Kind == KindTruncate || fault.Kind == KindInterrupt)) ||
			(!reader && (fault.Kind == KindShortWrite || fault.Kind == KindTruncate || fault.Kind == KindInterrupt)) {
			limit = min(limit, fault.limit)
		}
	}
	if reader {
		for _, fault := range faults {
			if fault.Kind == KindDuplicate && fault.phase == PhaseAfter {
				limit = min(limit, fault.limit)
			}
		}
	}
	return buffer[:limit]
}

func transformWriteBuffer(buffer []byte, faults []Fault) ([]byte, bool) {
	originalLength := len(buffer)
	buffer = boundedBuffer(buffer, faults, false)
	transformed := len(buffer) != originalLength
	copied := false
	for _, fault := range faults {
		if fault.phase != PhaseDuring || (fault.Kind != KindCorrupt && fault.Kind != KindReorder) {
			continue
		}
		if !copied {
			buffer = append([]byte(nil), buffer...)
			copied = true
		}
		prefix := buffer[:min(len(buffer), fault.limit)]
		if fault.Kind == KindCorrupt && len(prefix) != 0 {
			prefix[0] ^= fault.mask
		} else if fault.Kind == KindReorder {
			reverse(prefix)
		}
		transformed = true
	}
	return buffer, transformed
}

func ioPhaseError(faults []Fault, phase Phase, sleeper Sleeper) error {
	return faultPhaseError(context.Background(), faults, phase, sleeper)
}

func faultPhaseError(ctx context.Context, faults []Fault, phase Phase, sleeper Sleeper) error {
	if err := applyImmediate(ctx, sleeper, faults, phase); err != nil {
		return err
	}
	for _, fault := range faults {
		if fault.phase != phase {
			continue
		}
		switch fault.Kind {
		case KindTemporary:
			return &NetworkError{cause: ErrTemporaryNetwork, temporary: true}
		case KindPermanent:
			return &NetworkError{cause: ErrPermanentNetwork}
		case KindReset:
			return ErrConnectionReset
		case KindHalfClose:
			return ErrHalfClosed
		}
	}
	return nil
}

func reverse(buffer []byte) {
	for left := range len(buffer) / 2 {
		right := len(buffer) - 1 - left
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
}
