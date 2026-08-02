package faultinject_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestReaderAppliesBoundedByteFaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fault      faultinject.Fault
		reads      int
		wantChunks []string
		wantErrors []error
	}{
		{name: "short", fault: faultinject.ByteFault(faultinject.KindShortRead, faultinject.PhaseDuring, 2, 0), reads: 1, wantChunks: []string{"ab"}},
		{name: "truncate", fault: faultinject.ByteFault(faultinject.KindTruncate, faultinject.PhaseDuring, 3, 0), reads: 1, wantChunks: []string{"abc"}},
		{name: "corrupt", fault: faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 4, 0x20), reads: 1, wantChunks: []string{"Abcd"}},
		{name: "reorder", fault: faultinject.ByteFault(faultinject.KindReorder, faultinject.PhaseAfter, 4, 0), reads: 1, wantChunks: []string{"dcba"}},
		{name: "drop", fault: faultinject.ByteFault(faultinject.KindDrop, faultinject.PhaseAfter, 0, 0), reads: 1, wantChunks: []string{""}, wantErrors: []error{faultinject.ErrDropped}},
		{name: "duplicate", fault: faultinject.ByteFault(faultinject.KindDuplicate, faultinject.PhaseAfter, 4, 0), reads: 2, wantChunks: []string{"abcd", "abcd"}},
		{name: "interrupt", fault: faultinject.ByteFault(faultinject.KindInterrupt, faultinject.PhaseDuring, 2, 0), reads: 1, wantChunks: []string{"ab"}, wantErrors: []error{faultinject.ErrStreamInterrupted}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			injector := scopedInjector(t, faultinject.BoundaryReader, test.fault)
			reader := faultinject.WrapReader(bytes.NewBufferString("abcd"), injector, 11)
			for call := range test.reads {
				buffer := make([]byte, 8)
				n, err := reader.Read(buffer)
				if got := string(buffer[:n]); got != test.wantChunks[call] {
					t.Fatalf("Read(%d) chunk = %q, want %q", call, got, test.wantChunks[call])
				}
				var want error
				if call < len(test.wantErrors) {
					want = test.wantErrors[call]
				}
				if !errors.Is(err, want) {
					t.Fatalf("Read(%d) error = %v, want %v", call, err, want)
				}
			}
		})
	}
}

func TestWriterAppliesBoundedByteFaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fault     faultinject.Fault
		wantN     int
		wantBytes string
		wantError error
	}{
		{name: "short", fault: faultinject.ByteFault(faultinject.KindShortWrite, faultinject.PhaseDuring, 2, 0), wantN: 2, wantBytes: "ab", wantError: io.ErrShortWrite},
		{name: "truncate", fault: faultinject.ByteFault(faultinject.KindTruncate, faultinject.PhaseDuring, 3, 0), wantN: 3, wantBytes: "abc", wantError: io.ErrShortWrite},
		{name: "corrupt", fault: faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseDuring, 4, 0x20), wantN: 4, wantBytes: "Abcd"},
		{name: "corrupt bounded prefix", fault: faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseDuring, 2, 0x20), wantN: 4, wantBytes: "Abcd"},
		{name: "reorder", fault: faultinject.ByteFault(faultinject.KindReorder, faultinject.PhaseDuring, 4, 0), wantN: 4, wantBytes: "dcba"},
		{name: "reorder bounded prefix", fault: faultinject.ByteFault(faultinject.KindReorder, faultinject.PhaseDuring, 2, 0), wantN: 4, wantBytes: "bacd"},
		{name: "drop", fault: faultinject.ByteFault(faultinject.KindDrop, faultinject.PhaseDuring, 0, 0), wantN: 4, wantBytes: ""},
		{name: "duplicate", fault: faultinject.ByteFault(faultinject.KindDuplicate, faultinject.PhaseAfter, 4, 0), wantN: 4, wantBytes: "abcdabcd"},
		{name: "interrupt", fault: faultinject.ByteFault(faultinject.KindInterrupt, faultinject.PhaseDuring, 2, 0), wantN: 2, wantBytes: "ab", wantError: faultinject.ErrStreamInterrupted},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var destination bytes.Buffer
			injector := scopedInjector(t, faultinject.BoundaryWriter, test.fault)
			writer := faultinject.WrapWriter(&destination, injector, 12)
			n, err := writer.Write([]byte("abcd"))
			if n != test.wantN || destination.String() != test.wantBytes || !errors.Is(err, test.wantError) {
				t.Fatalf("Write() = %d, %v, bytes %q; want %d, %v, %q", n, err, destination.String(), test.wantN, test.wantError, test.wantBytes)
			}
		})
	}
}

func TestWriterComposesBoundedTransformsWithoutDiscardingSuffix(t *testing.T) {
	t.Parallel()

	rule := ruleWithFault("composed-writer", faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseDuring, 2, 0x20))
	rule.Scope = faultinject.BoundaryWriter
	rule.Faults = append(rule.Faults,
		faultinject.ByteFault(faultinject.KindReorder, faultinject.PhaseDuring, 2, 0),
	)
	injector := injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}})
	var destination bytes.Buffer

	n, err := faultinject.WrapWriter(&destination, injector, 12).Write([]byte("abcd"))
	if n != 4 || err != nil || destination.String() != "bAcd" {
		t.Fatalf("Write() = %d, %v, bytes %q; want 4, nil, %q", n, err, destination.String(), "bAcd")
	}
}

func TestIOFaultsPreservePartialResultsAndExposeNetworkClass(t *testing.T) {
	t.Parallel()

	partial := &partialWriter{err: errInjected}
	writer := faultinject.WrapWriter(partial, scopedInjector(t, faultinject.BoundaryWriter,
		faultinject.ErrorFault(faultinject.PhaseAfter, faultinject.ErrConnectionReset)), 1)
	n, err := writer.Write([]byte("abcd"))
	if n != 2 || !errors.Is(err, faultinject.ErrConnectionReset) {
		t.Fatalf("partial Write() = %d, %v", n, err)
	}

	for _, test := range []struct {
		kind      faultinject.Kind
		temporary bool
	}{
		{kind: faultinject.KindTemporary, temporary: true},
		{kind: faultinject.KindPermanent, temporary: false},
	} {
		reader := faultinject.WrapReader(bytes.NewBufferString("data"), scopedInjector(t,
			faultinject.BoundaryReader, faultinject.ByteFault(test.kind, faultinject.PhaseBefore, 0, 0)), 1)
		_, err := reader.Read(make([]byte, 4))
		var networkError net.Error
		if !errors.As(err, &networkError) {
			t.Fatalf("%s error = %T %v", test.kind, err, err)
		}
		//lint:ignore SA1019 Required net.Error compatibility contract.
		temporary := networkError.Temporary() //nolint:staticcheck // Required net.Error compatibility contract.
		if temporary != test.temporary || networkError.Timeout() {
			t.Fatalf("%s error = %T %v", test.kind, err, err)
		}
	}
}

func TestDisabledIOWrappersReturnOriginalBoundary(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader(nil)
	writer := &bytes.Buffer{}
	if got := faultinject.WrapReader(reader, nil, 1); !reflect.DeepEqual(got, reader) {
		t.Fatal("disabled reader wrapper changed boundary")
	}
	if got := faultinject.WrapWriter(writer, nil, 1); !reflect.DeepEqual(got, writer) {
		t.Fatal("disabled writer wrapper changed boundary")
	}
}

func scopedInjector(t *testing.T, boundary faultinject.Boundary, fault faultinject.Fault) *faultinject.Injector {
	t.Helper()
	rule := ruleWithFault("io", fault)
	rule.Scope = boundary
	return injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}})
}

type partialWriter struct{ err error }

func (writer *partialWriter) Write(buffer []byte) (int, error) {
	return len(buffer) / 2, writer.err
}
