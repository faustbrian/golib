package outputlimit

import (
	"errors"
	"testing"
)

func TestBufferEnforcesConfiguredAndFallbackLimits(t *testing.T) {
	t.Parallel()

	buffer, err := New(3, 9)
	if err != nil {
		t.Fatal(err)
	}
	written, err := buffer.Write([]byte("abcd"))
	if written != 3 || !errors.Is(err, ErrLimit) || string(buffer.Bytes()) != "abc" || buffer.Len() != 3 {
		t.Fatalf("Write() = %d, %v, %q", written, err, buffer.Bytes())
	}
	written, err = buffer.Write([]byte("z"))
	if written != 0 || !errors.Is(err, ErrLimit) {
		t.Fatalf("full Write() = %d, %v", written, err)
	}

	fallback, err := New(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if written, err := fallback.Write([]byte("ok")); written != 2 || err != nil {
		t.Fatalf("fallback Write() = %d, %v", written, err)
	}
}

func TestNewRejectsNegativeLimit(t *testing.T) {
	t.Parallel()

	if _, err := New(-1, 1); err == nil {
		t.Fatal("New() error = nil")
	}
}
