package faultinject

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestInternalByteTransformPhaseAndEmptyGuards(t *testing.T) {
	t.Parallel()

	reader := &injectedReader{}
	data := []byte("ab")
	for _, fault := range []Fault{
		{Kind: KindCorrupt, phase: PhaseBefore, limit: 2, mask: 1},
		{Kind: KindReorder, phase: PhaseBefore, limit: 2},
		{Kind: KindDrop, phase: PhaseBefore},
		{Kind: KindDuplicate, phase: PhaseBefore, limit: 2},
		{Kind: KindInterrupt, phase: PhaseAfter, limit: 1},
	} {
		copyData := append([]byte(nil), data...)
		n, err := reader.transform(copyData, []Fault{fault})
		if n != 2 || err != nil || !bytes.Equal(copyData, data) || len(reader.duplicate) != 0 {
			t.Fatalf("wrong-phase transform = %q, %d, %v, duplicate=%q", copyData, n, err, reader.duplicate)
		}
	}
	for _, fault := range []Fault{
		{Kind: KindCorrupt, phase: PhaseAfter, limit: 1, mask: 1},
		{Kind: KindDrop, phase: PhaseAfter},
		{Kind: KindDuplicate, phase: PhaseAfter, limit: 1},
		{Kind: KindInterrupt, phase: PhaseDuring, limit: 1},
	} {
		if n, err := reader.transform([]byte{}, []Fault{fault}); n != 0 || err != nil || len(reader.duplicate) != 0 {
			t.Fatalf("empty transform = %d, %v, duplicate=%q", n, err, reader.duplicate)
		}
	}
}

func TestInternalBufferSelectionAndPhaseIteration(t *testing.T) {
	t.Parallel()

	buffer := []byte("abcd")
	if got := boundedBuffer(buffer, []Fault{{Kind: KindShortRead, phase: PhaseBefore, limit: 1}}, true); len(got) != 4 {
		t.Fatalf("before-phase reader length = %d", len(got))
	}
	if got := boundedBuffer(buffer, []Fault{{Kind: KindShortWrite, phase: PhaseDuring, limit: 1}}, true); len(got) != 4 {
		t.Fatalf("writer kind changed reader length = %d", len(got))
	}
	if got := boundedBuffer(buffer, []Fault{{Kind: KindShortRead, phase: PhaseDuring, limit: 1}}, false); len(got) != 4 {
		t.Fatalf("reader kind changed writer length = %d", len(got))
	}
	if got := boundedBuffer(buffer, []Fault{{Kind: KindDrop, phase: PhaseDuring}, {Kind: KindShortRead, phase: PhaseDuring, limit: 2}}, true); len(got) != 2 {
		t.Fatalf("later reader bound length = %d", len(got))
	}
	if got := boundedBuffer(buffer, []Fault{{Kind: KindDrop, phase: PhaseAfter}, {Kind: KindShortRead, phase: PhaseDuring, limit: 2}}, true); len(got) != 2 {
		t.Fatalf("wrong-phase fault stopped later bound: %d", len(got))
	}
	if got := boundedBuffer(buffer, []Fault{{Kind: KindDrop, phase: PhaseAfter}, {Kind: KindDuplicate, phase: PhaseAfter, limit: 3}}, true); len(got) != 3 {
		t.Fatalf("duplicate reader bound length = %d", len(got))
	}

	transformed, changed := transformWriteBuffer(buffer, []Fault{
		{Kind: KindCorrupt, phase: PhaseAfter, limit: 1, mask: 1},
		{Kind: KindDrop, phase: PhaseDuring},
	})
	if changed || !bytes.Equal(transformed, buffer) {
		t.Fatalf("wrong-phase write transform = %q, %t", transformed, changed)
	}
	transformed, changed = transformWriteBuffer(buffer, []Fault{
		{Kind: KindDrop, phase: PhaseDuring},
		{Kind: KindReorder, phase: PhaseDuring, limit: 2},
	})
	if !changed || string(transformed) != "bacd" {
		t.Fatalf("later write transform = %q, %t", transformed, changed)
	}
}

func TestInternalFaultPhaseIterationAndHTTPNilCleanup(t *testing.T) {
	t.Parallel()

	err := faultPhaseError(context.Background(), []Fault{
		{Kind: KindTemporary, phase: PhaseAfter},
		{Kind: KindPermanent, phase: PhaseBefore},
	}, PhaseBefore, systemSleeper{})
	if !errors.Is(err, ErrPermanentNetwork) {
		t.Fatalf("phase error = %v", err)
	}
	closeRequestBody(nil)
	closeRequestBody(&http.Request{})
	closeResponseBody(nil)
	closeResponseBody(&http.Response{})
}

func TestInternalDuplicateRequiresCompleteFirstWrite(t *testing.T) {
	t.Parallel()

	base := &shortNilWriter{}
	writer := &injectedWriter{
		writer:   base,
		injector: mustInternalInjector(t, BoundaryWriter, Fault{Kind: KindDuplicate, phase: PhaseAfter, limit: 2}),
		boundary: BoundaryWriter,
	}
	n, err := writer.Write([]byte("ab"))
	if n != 1 || err != nil || base.calls != 1 {
		t.Fatalf("Write() = %d, %v, calls=%d", n, err, base.calls)
	}
}

type shortNilWriter struct{ calls int }

func (writer *shortNilWriter) Write([]byte) (int, error) {
	writer.calls++
	return 1, nil
}

func mustInternalInjector(t *testing.T, boundary Boundary, fault Fault) *Injector {
	t.Helper()
	injector, err := New(Config{Rules: []Rule{{
		ID: "internal", Scope: boundary, Activation: Active, Maximum: 1,
		Terminal: Continue, Observation: Suppress, Schedule: Every(1),
		Faults: []Fault{fault},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return injector
}

var _ io.Writer = (*shortNilWriter)(nil)
