// Package keyphrasetest provides deterministic sources and statistical test
// helpers. Its distribution checks can detect obvious bias but do not certify
// a cryptographic random source.
package keyphrasetest

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
)

// ErrInvalidSample reports an unusable statistical sample.
var ErrInvalidSample = errors.New("keyphrasetest: invalid statistical sample")

// Source is a finite, concurrency-safe deterministic byte source.
type Source struct {
	mu     sync.Mutex
	data   []byte
	offset int
}

// NewSource copies data into a deterministic source.
func NewSource(data []byte) *Source {
	return &Source{data: append([]byte(nil), data...)}
}

// ReadContext implements keyphrase.Source.
func (s *Source) ReadContext(ctx context.Context, destination []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offset == len(s.data) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[s.offset:])
	s.offset += count
	if count < len(destination) {
		return count, io.ErrUnexpectedEOF
	}
	return count, nil
}

// CounterSource emits every byte value cyclically. It is useful for testing
// rejection-sampling boundaries, not for generating secrets.
type CounterSource struct {
	mu   sync.Mutex
	next byte
}

// NewCounterSource creates a source beginning with byte zero.
func NewCounterSource() *CounterSource {
	return &CounterSource{}
}

// ReadContext implements keyphrase.Source.
func (s *CounterSource) ReadContext(ctx context.Context, destination []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range destination {
		destination[index] = s.next
		s.next++
	}
	return len(destination), nil
}

// ChiSquared returns Pearson's statistic against equal expected counts.
func ChiSquared(counts []uint64) (float64, error) {
	if len(counts) < 2 {
		return math.NaN(), ErrInvalidSample
	}
	total := uint64(0)
	for _, count := range counts {
		total += count
	}
	if total == 0 {
		return math.NaN(), ErrInvalidSample
	}
	expected := float64(total) / float64(len(counts))
	statistic := 0.0
	for _, count := range counts {
		difference := float64(count) - expected
		statistic += difference * difference / expected
	}
	return statistic, nil
}
