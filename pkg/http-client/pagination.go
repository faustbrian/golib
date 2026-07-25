package httpclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrInvalidPagination indicates malformed pagination policy or state.
	ErrInvalidPagination = errors.New("invalid HTTP pagination policy")
	// ErrPaginationLimit indicates that a finite pagination budget was reached.
	ErrPaginationLimit = errors.New("HTTP pagination limit reached")
	// ErrPaginationCycle indicates a repeated continuation identity.
	ErrPaginationCycle = errors.New("HTTP pagination continuation cycle")
	// ErrPaginationFetch indicates that a page fetcher failed.
	ErrPaginationFetch = errors.New("HTTP pagination fetch failed")
)

const (
	defaultMaximumPaginationPages             = 100
	defaultMaximumPaginationItems             = 10_000
	defaultMaximumPaginationElapsed           = 5 * time.Minute
	defaultMaximumPaginationResponseBytes     = 64 << 20
	defaultMaximumPaginationEmptyPages        = 3
	defaultMaximumPaginationContinuationBytes = 4 << 10
)

// PaginationPage is one typed fetch result and its next continuation.
type PaginationPage[Item any, Continuation any] struct {
	Items         []Item
	Next          Continuation
	HasNext       bool
	ResponseBytes int64
}

// PaginationFetcher loads one page for an opaque typed continuation.
type PaginationFetcher[Item any, Continuation any] func(
	context.Context,
	Continuation,
) (PaginationPage[Item, Continuation], error)

// PaginationContinuationKey returns a deterministic bounded cycle key without
// exposing the continuation through errors or telemetry.
type PaginationContinuationKey[Continuation any] func(Continuation) (string, error)

// PaginationLimits are finite cumulative iterator budgets. Zero fields select
// production defaults.
type PaginationLimits struct {
	MaximumPages             int
	MaximumItems             int
	MaximumElapsed           time.Duration
	MaximumResponseBytes     int64
	MaximumEmptyPages        int
	MaximumContinuationBytes int
}

// PaginationState is a resumable snapshot including unconsumed typed items and
// continuation cycle history.
type PaginationState[Item any, Continuation any] struct {
	Continuation  Continuation
	HasNext       bool
	Done          bool
	Buffered      []Item
	BufferedIndex int
	Pages         int
	Items         int
	ResponseBytes int64
	EmptyPages    int
	Elapsed       time.Duration
	Seen          []string
}

// PaginationOptions configures one lazy typed iterator.
type PaginationOptions[Item any, Continuation any] struct {
	Initial Continuation
	Fetch   PaginationFetcher[Item, Continuation]
	Key     PaginationContinuationKey[Continuation]
	Limits  PaginationLimits
	Clock   RetryClock
	Resume  *PaginationState[Item, Continuation]
}

// PaginationError reports a safe failure category without rendering a cursor,
// vendor response, item, or underlying cause.
type PaginationError struct {
	Kind  string
	Cause error
}

// Error implements error without rendering pagination data.
func (err *PaginationError) Error() string {
	return fmt.Sprintf("HTTP pagination %s failed", err.Kind)
}

// Unwrap returns the stable category and underlying cause.
func (err *PaginationError) Unwrap() error { return err.Cause }

// Paginator lazily yields typed items. It is safe for concurrent calls, which
// are serialized to preserve input order and continuation ownership.
type Paginator[Item any, Continuation any] struct {
	mu          sync.Mutex
	fetch       PaginationFetcher[Item, Continuation]
	key         PaginationContinuationKey[Continuation]
	limits      PaginationLimits
	clock       RetryClock
	started     time.Time
	baseElapsed time.Duration
	state       PaginationState[Item, Continuation]
	seen        map[string]struct{}
}

// NewPaginator validates policy and constructs a lazy iterator without
// fetching a page.
func NewPaginator[Item any, Continuation any](
	options PaginationOptions[Item, Continuation],
) (*Paginator[Item, Continuation], error) {
	if nilLike(options.Fetch) {
		return nil, fmt.Errorf("%w: fetcher is nil", ErrInvalidPagination)
	}
	if nilLike(options.Key) {
		return nil, fmt.Errorf("%w: continuation key is nil", ErrInvalidPagination)
	}
	limits, err := resolvePaginationLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidPagination)
	}
	state := PaginationState[Item, Continuation]{
		Continuation: options.Initial,
		HasNext:      true,
	}
	if options.Resume != nil {
		state = clonePaginationState(*options.Resume)
		if err := validatePaginationState(state, limits); err != nil {
			return nil, err
		}
	}
	seen := make(map[string]struct{}, len(state.Seen))
	for _, key := range state.Seen {
		if len(key) > limits.MaximumContinuationBytes {
			return nil, fmt.Errorf("%w: stored continuation key is too long", ErrInvalidPagination)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: stored continuation key is duplicated", ErrInvalidPagination)
		}
		seen[key] = struct{}{}
	}

	return &Paginator[Item, Continuation]{
		fetch: options.Fetch, key: options.Key, limits: limits, clock: clock,
		started: clock.Now(), baseElapsed: state.Elapsed, state: state, seen: seen,
	}, nil
}

func resolvePaginationLimits(limits PaginationLimits) (PaginationLimits, error) {
	if limits.MaximumPages == 0 {
		limits.MaximumPages = defaultMaximumPaginationPages
	}
	if limits.MaximumItems == 0 {
		limits.MaximumItems = defaultMaximumPaginationItems
	}
	if limits.MaximumElapsed == 0 {
		limits.MaximumElapsed = defaultMaximumPaginationElapsed
	}
	if limits.MaximumResponseBytes == 0 {
		limits.MaximumResponseBytes = defaultMaximumPaginationResponseBytes
	}
	if limits.MaximumEmptyPages == 0 {
		limits.MaximumEmptyPages = defaultMaximumPaginationEmptyPages
	}
	if limits.MaximumContinuationBytes == 0 {
		limits.MaximumContinuationBytes = defaultMaximumPaginationContinuationBytes
	}
	if limits.MaximumPages < 0 || limits.MaximumItems < 0 || limits.MaximumElapsed < 0 ||
		limits.MaximumResponseBytes < 0 || limits.MaximumEmptyPages < 0 ||
		limits.MaximumContinuationBytes < 0 {
		return PaginationLimits{}, fmt.Errorf("%w: limit is negative", ErrInvalidPagination)
	}

	return limits, nil
}

func validatePaginationState[Item any, Continuation any](
	state PaginationState[Item, Continuation],
	limits PaginationLimits,
) error {
	if state.Pages < 0 || state.Items < 0 || state.ResponseBytes < 0 ||
		state.EmptyPages < 0 || state.Elapsed < 0 || state.BufferedIndex < 0 ||
		state.BufferedIndex > len(state.Buffered) {
		return fmt.Errorf("%w: resume counters are invalid", ErrInvalidPagination)
	}
	if state.Pages > limits.MaximumPages || state.Items > limits.MaximumItems ||
		state.ResponseBytes > limits.MaximumResponseBytes ||
		state.EmptyPages > limits.MaximumEmptyPages || state.Elapsed > limits.MaximumElapsed {
		return fmt.Errorf("%w: resume state exceeds configured limits", ErrInvalidPagination)
	}
	if !state.Done && !state.HasNext && state.BufferedIndex == len(state.Buffered) {
		return fmt.Errorf("%w: resume terminal state is inconsistent", ErrInvalidPagination)
	}

	return nil
}

// Next returns the next item, false at clean exhaustion, or a typed failure.
func (paginator *Paginator[Item, Continuation]) Next(ctx context.Context) (Item, bool, error) {
	paginator.mu.Lock()
	defer paginator.mu.Unlock()

	var zero Item
	if ctx == nil {
		return zero, false, fmt.Errorf("%w: iteration context is nil", ErrInvalidPagination)
	}
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	for {
		if err := paginator.updateElapsed(); err != nil {
			return zero, false, err
		}
		if paginator.state.BufferedIndex < len(paginator.state.Buffered) {
			item := paginator.state.Buffered[paginator.state.BufferedIndex]
			paginator.state.BufferedIndex++
			if paginator.state.BufferedIndex == len(paginator.state.Buffered) && !paginator.state.HasNext {
				paginator.state.Done = true
			}

			return item, true, nil
		}
		paginator.state.Buffered = nil
		paginator.state.BufferedIndex = 0
		if paginator.state.Done || !paginator.state.HasNext {
			paginator.state.Done = true

			return zero, false, nil
		}
		if paginator.state.Pages >= paginator.limits.MaximumPages {
			return zero, false, paginationLimitError("pages")
		}
		if err := paginator.fetchPage(ctx); err != nil {
			return zero, false, err
		}
	}
}

func (paginator *Paginator[Item, Continuation]) fetchPage(ctx context.Context) error {
	key, err := paginator.key(paginator.state.Continuation)
	if err != nil {
		return &PaginationError{Kind: "continuation", Cause: err}
	}
	if len(key) > paginator.limits.MaximumContinuationBytes {
		return paginationLimitError("continuation bytes")
	}
	if _, exists := paginator.seen[key]; exists {
		return &PaginationError{Kind: "continuation cycle", Cause: ErrPaginationCycle}
	}

	page, err := paginator.fetch(ctx, paginator.state.Continuation)
	if err != nil {
		return &PaginationError{Kind: "fetch", Cause: errors.Join(ErrPaginationFetch, err)}
	}
	if page.ResponseBytes < 0 {
		return &PaginationError{Kind: "response bytes", Cause: ErrInvalidPagination}
	}
	if page.HasNext {
		nextKey, keyErr := paginator.key(page.Next)
		if keyErr != nil {
			return &PaginationError{Kind: "continuation", Cause: keyErr}
		}
		if len(nextKey) > paginator.limits.MaximumContinuationBytes {
			return paginationLimitError("continuation bytes")
		}
	}
	nextItems := paginator.state.Items + len(page.Items)
	nextBytes := paginator.state.ResponseBytes + page.ResponseBytes
	if nextItems > paginator.limits.MaximumItems || nextItems < paginator.state.Items {
		return paginationLimitError("items")
	}
	if nextBytes > paginator.limits.MaximumResponseBytes || nextBytes < paginator.state.ResponseBytes {
		return paginationLimitError("response bytes")
	}
	if err := paginator.updateElapsed(); err != nil {
		return err
	}
	nextEmptyPages := 0
	if len(page.Items) == 0 {
		nextEmptyPages = paginator.state.EmptyPages + 1
		if nextEmptyPages > paginator.limits.MaximumEmptyPages {
			return paginationLimitError("empty pages")
		}
	}
	paginator.seen[key] = struct{}{}
	paginator.state.Seen = append(paginator.state.Seen, key)
	paginator.state.Pages++
	paginator.state.Items = nextItems
	paginator.state.ResponseBytes = nextBytes
	paginator.state.EmptyPages = nextEmptyPages
	paginator.state.Continuation = page.Next
	paginator.state.HasNext = page.HasNext
	paginator.state.Buffered = append([]Item(nil), page.Items...)
	paginator.state.BufferedIndex = 0
	if len(page.Items) == 0 && !page.HasNext {
		paginator.state.Done = true
	}

	return nil
}

func (paginator *Paginator[Item, Continuation]) updateElapsed() error {
	now := paginator.clock.Now()
	elapsed := now.Sub(paginator.started)
	if elapsed < 0 {
		elapsed = 0
	}
	paginator.state.Elapsed = paginator.baseElapsed + elapsed
	if paginator.state.Elapsed > paginator.limits.MaximumElapsed ||
		paginator.state.Elapsed < paginator.baseElapsed {
		return paginationLimitError("elapsed time")
	}

	return nil
}

func paginationLimitError(kind string) error {
	return &PaginationError{Kind: kind, Cause: ErrPaginationLimit}
}

// State returns an independent resumable snapshot.
func (paginator *Paginator[Item, Continuation]) State() PaginationState[Item, Continuation] {
	paginator.mu.Lock()
	defer paginator.mu.Unlock()
	_ = paginator.updateElapsed()

	return clonePaginationState(paginator.state)
}

func clonePaginationState[Item any, Continuation any](
	state PaginationState[Item, Continuation],
) PaginationState[Item, Continuation] {
	state.Buffered = append([]Item(nil), state.Buffered...)
	state.Seen = append([]string(nil), state.Seen...)

	return state
}
