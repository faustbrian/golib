package prompts

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

// QueryGeneration identifies one scheduled dynamic-option query. Results may
// be applied only while their generation remains current.
type QueryGeneration uint64

// OptionProvider resolves options for a complete caller-visible query. It
// must honor context cancellation and must not retain or mutate returned
// option slices after returning.
type OptionProvider[T any] func(context.Context, string) ([]Option[T], error)

// DynamicOptionsConfig defines a caller-driven, deterministic dynamic-option
// session. Debouncing is checked against Clock without timers or goroutines.
type DynamicOptionsConfig[T any] struct {
	Clock         Clock
	Provider      OptionProvider[T]
	Debounce      time.Duration
	MaxOptions    int
	MaxQueryRunes int
}

// DynamicOptions owns scheduling and generation-safe option replacement. The
// caller schedules a query and calls Resolve after the debounce deadline.
type DynamicOptions[T any] struct {
	mutex          sync.RWMutex
	clock          Clock
	provider       OptionProvider[T]
	debounce       time.Duration
	maxOptions     int
	maxQueryRunes  int
	generation     QueryGeneration
	query          string
	due            time.Time
	currentOptions []Option[T]
}

// NewDynamicOptions creates an explicit dynamic-option session.
func NewDynamicOptions[T any](config DynamicOptionsConfig[T]) (*DynamicOptions[T], error) {
	if config.Clock == nil || config.Provider == nil || config.Debounce < 0 ||
		config.MaxOptions < 0 || config.MaxQueryRunes < 0 {
		return nil, &Error{
			Kind: ErrorInvalidDefinition, Operation: "define dynamic options",
			Cause: ErrInvalidDefinition,
		}
	}
	if config.MaxOptions == 0 {
		config.MaxOptions = 10_000
	}
	if config.MaxQueryRunes == 0 {
		config.MaxQueryRunes = 256
	}

	return &DynamicOptions[T]{
		clock: config.Clock, provider: config.Provider, debounce: config.Debounce,
		maxOptions: config.MaxOptions, maxQueryRunes: config.MaxQueryRunes,
	}, nil
}

// Schedule supersedes every prior query and returns its new generation.
func (dynamic *DynamicOptions[T]) Schedule(query string) (QueryGeneration, error) {
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > dynamic.maxQueryRunes {
		return 0, &Error{
			Kind: ErrorUnsupported, Operation: "schedule dynamic options",
			Cause: ErrUnsupported,
		}
	}

	dynamic.mutex.Lock()
	defer dynamic.mutex.Unlock()
	dynamic.generation++
	dynamic.query = query
	dynamic.due = dynamic.clock.Now().Add(dynamic.debounce)

	return dynamic.generation, nil
}

// Resolve calls the provider only after the scheduled debounce deadline. Its
// applied result is false for an early or superseded generation.
func (dynamic *DynamicOptions[T]) Resolve(
	ctx context.Context,
	generation QueryGeneration,
) ([]Option[T], bool, error) {
	if ctx == nil {
		return nil, false, &Error{
			Kind: ErrorInvalidDefinition, Operation: "resolve dynamic options",
			Cause: ErrInvalidDefinition,
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, contextFailure("", err)
	}

	dynamic.mutex.RLock()
	if generation == 0 || generation != dynamic.generation || dynamic.clock.Now().Before(dynamic.due) {
		dynamic.mutex.RUnlock()
		return nil, false, nil
	}
	query := dynamic.query
	dynamic.mutex.RUnlock()

	options, err := callOptionProvider(ctx, dynamic.provider, query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, contextFailure("", err)
		}

		return nil, false, &Error{
			Kind: ErrorAdapter, Operation: "resolve dynamic options", Cause: err,
		}
	}
	if err := validateDynamicOptions(options, dynamic.maxOptions); err != nil {
		return nil, false, &Error{
			Kind: ErrorAdapter, Operation: "resolve dynamic options", Cause: ErrAdapter,
		}
	}
	owned := append([]Option[T](nil), options...)

	dynamic.mutex.Lock()
	defer dynamic.mutex.Unlock()
	if generation != dynamic.generation {
		return nil, false, nil
	}
	dynamic.currentOptions = owned

	return append([]Option[T](nil), owned...), true, nil
}

// Snapshot returns a defensive option slice and the latest scheduled
// generation. It is safe to call concurrently with Schedule and Resolve.
func (dynamic *DynamicOptions[T]) Snapshot() ([]Option[T], QueryGeneration) {
	dynamic.mutex.RLock()
	defer dynamic.mutex.RUnlock()

	return append([]Option[T](nil), dynamic.currentOptions...), dynamic.generation
}

func callOptionProvider[T any](
	ctx context.Context,
	provider OptionProvider[T],
	query string,
) (options []Option[T], resultErr error) {
	defer func() {
		if recover() != nil {
			options = nil
			resultErr = ErrAdapter
		}
	}()

	return provider(ctx, query)
}

func validateDynamicOptions[T any](options []Option[T], maximum int) error {
	if len(options) > maximum {
		return fmt.Errorf("%w: option count exceeds configured bound", ErrAdapter)
	}
	identities := make(map[string]struct{}, len(options))
	for _, option := range options {
		if option.id == "" || option.label == "" {
			return fmt.Errorf("%w: invalid dynamic option", ErrAdapter)
		}
		if _, duplicate := identities[option.id]; duplicate {
			return fmt.Errorf("%w: duplicate dynamic option identity", ErrAdapter)
		}
		identities[option.id] = struct{}{}
	}

	return nil
}
