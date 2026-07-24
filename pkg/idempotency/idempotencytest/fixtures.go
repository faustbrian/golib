// Package idempotencytest provides reusable adapter conformance and fixtures.
package idempotencytest

import (
	"fmt"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/clock/manual"
)

// Clock is a concurrency-safe deterministic clock for store tests.
type Clock struct {
	mu    sync.RWMutex
	clock *manual.Clock
}

// NewClock constructs a deterministic clock at now.
func NewClock(now time.Time) *Clock {
	inner, _ := manual.New(now)
	return &Clock{clock: inner}
}

// Now returns the current deterministic instant.
func (c *Clock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clock.Now()
}

// Advance moves the clock forward by duration.
func (c *Clock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inner, _ := manual.New(c.clock.Now().Add(duration))
	c.clock = inner
}

// Set replaces the current deterministic instant.
func (c *Clock) Set(now time.Time) {
	inner, _ := manual.New(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock = inner
}

// TokenSource emits deterministic unique ownership tokens for tests.
type TokenSource struct {
	mu     sync.Mutex
	prefix string
	next   uint64
}

// NewTokenSource constructs a token source using prefix.
func NewTokenSource(prefix string) *TokenSource {
	return &TokenSource{prefix: prefix}
}

// Next returns the next deterministic token.
func (s *TokenSource) Next() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return fmt.Sprintf("%s-%d", s.prefix, s.next), nil
}
