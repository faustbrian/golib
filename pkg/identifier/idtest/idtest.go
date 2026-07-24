// Package idtest provides deterministic clocks, entropy, and reusable
// identifier assertions. Its sources are for tests and must not be used for
// production identifier generation.
package idtest

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

// Clock is a mutex-protected deterministic clock.
type Clock struct {
	mutex sync.RWMutex
	now   time.Time
}

// NewClock starts a deterministic clock at an explicit instant.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

// Now returns the controlled instant.
func (clock *Clock) Now() time.Time {
	clock.mutex.RLock()
	defer clock.mutex.RUnlock()

	return clock.now
}

// Set replaces the controlled instant.
func (clock *Clock) Set(now time.Time) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()

	clock.now = now
}

// Advance moves the controlled instant by a duration, including backwards for
// rollback tests.
func (clock *Clock) Advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()

	clock.now = clock.now.Add(duration)
}

// Reader expands a seed and counter with SHA-256 for reproducible test bytes.
// It is deterministic, not a cryptographic entropy source.
type Reader struct {
	mutex   sync.Mutex
	seed    []byte
	counter uint64
	buffer  []byte
}

// NewReader copies a deterministic seed.
func NewReader(seed []byte) *Reader {
	copied := make([]byte, len(seed))
	copy(copied, seed)

	return &Reader{seed: copied}
}

// Read fills output deterministically and never returns a short read.
func (reader *Reader) Read(output []byte) (int, error) {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()

	written := 0
	for written < len(output) {
		if len(reader.buffer) == 0 {
			counter := make([]byte, 8)
			binary.BigEndian.PutUint64(counter, reader.counter)
			digest := sha256.Sum256(append(append([]byte(nil), reader.seed...), counter...))
			reader.buffer = digest[:]
			reader.counter++
		}
		copied := copy(output[written:], reader.buffer)
		written += copied
		reader.buffer = reader.buffer[copied:]
	}

	return written, nil
}

type failingReader struct{ err error }

func (reader failingReader) Read([]byte) (int, error) { return 0, reader.err }

// ErrorReader returns an entropy source that always reports err.
func ErrorReader(err error) io.Reader { return failingReader{err: err} }

// AssertUnique generates count values, fails on errors or collisions, and
// returns the generated sequence for additional assertions.
func AssertUnique[T comparable](t testing.TB, generator identifier.Generator[T], count int) []T {
	t.Helper()

	seen := make(map[T]struct{}, count)
	values := make([]T, 0, count)
	for index := 0; index < count; index++ {
		value, err := generator.New()
		if err != nil {
			t.Fatalf("generate identifier %d: %v", index, err)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("identifier collision at generation %d", index)
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}

	return values
}

type stringValue interface {
	comparable
	fmt.Stringer
}

// AssertCanonical proves formatting parses back to the identical value.
func AssertCanonical[T stringValue](t testing.TB, value T, parse func(string) (T, error)) {
	t.Helper()

	text := value.String()
	parsed, err := parse(text)
	if err != nil {
		t.Fatalf("parse canonical %q: %v", text, err)
	}
	if parsed != value {
		t.Fatalf("canonical round trip changed %q", text)
	}
}

// AssertStrictlyOrdered proves each adjacent value is greater than its
// predecessor under compare.
func AssertStrictlyOrdered[T any](t testing.TB, values []T, compare func(T, T) int) {
	t.Helper()

	for index := 1; index < len(values); index++ {
		if compare(values[index-1], values[index]) >= 0 {
			t.Fatalf("identifier sequence is not strictly ordered at %d", index)
		}
	}
}
