package outbox

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestUUIDFromReaderSetsVersionAndVariant(t *testing.T) {
	t.Parallel()

	id, err := uuidFromReader(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatalf("create UUID: %v", err)
	}
	if id != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("UUID = %q", id)
	}
}

func TestUUIDFromReaderPreservesReadFailure(t *testing.T) {
	t.Parallel()

	readErr := errors.New("entropy unavailable")
	_, err := uuidFromReader(errorReader{err: readErr})
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
}

func TestDefaultUUIDGeneratorCreatesUUID(t *testing.T) {
	t.Parallel()

	id, err := randomUUID()
	if err != nil {
		t.Fatalf("create UUID: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("UUID length = %d, want 36", len(id))
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

var _ io.Reader = errorReader{}
