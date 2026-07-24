package httpclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPaginatorIsLazyBoundedAndPreservesTypedItems(t *testing.T) {
	t.Parallel()

	type item struct{ ID int }
	clock := &paginationTestClock{now: time.Unix(1_700_000_000, 0)}
	fetches := 0
	paginator, err := NewPaginator(PaginationOptions[item, int]{
		Initial: 1,
		Key:     func(page int) (string, error) { return fmt.Sprintf("page:%d", page), nil },
		Fetch: func(_ context.Context, page int) (PaginationPage[item, int], error) {
			fetches++
			clock.now = clock.now.Add(time.Second)
			return PaginationPage[item, int]{
				Items: []item{{ID: page*10 + 1}, {ID: page*10 + 2}},
				Next:  page + 1, HasNext: page < 2, ResponseBytes: 20,
			}, nil
		},
		Clock: clock,
		Limits: PaginationLimits{
			MaximumPages: 2, MaximumItems: 4, MaximumElapsed: 3 * time.Second,
			MaximumResponseBytes: 40, MaximumEmptyPages: 1, MaximumContinuationBytes: 32,
		},
	})
	if err != nil {
		t.Fatalf("construct paginator: %v", err)
	}
	if fetches != 0 {
		t.Fatalf("constructor fetched %d pages", fetches)
	}
	var ids []int
	for {
		value, ok, nextErr := paginator.Next(context.Background())
		if nextErr != nil {
			t.Fatalf("iterate: %v", nextErr)
		}
		if !ok {
			break
		}
		ids = append(ids, value.ID)
	}
	if fmt.Sprint(ids) != "[11 12 21 22]" || fetches != 2 {
		t.Fatalf("items = %v, fetches = %d", ids, fetches)
	}
	state := paginator.State()
	if !state.Done || state.Pages != 2 || state.Items != 4 || state.ResponseBytes != 40 || state.Elapsed != 2*time.Second {
		t.Fatalf("final state = %#v", state)
	}
}

func TestPaginatorResumePreservesBufferedItemsAndCycleHistory(t *testing.T) {
	t.Parallel()

	fetches := 0
	options := PaginationOptions[string, string]{
		Initial: "cursor-a",
		Key:     func(cursor string) (string, error) { return cursor, nil },
		Fetch: func(_ context.Context, cursor string) (PaginationPage[string, string], error) {
			fetches++
			if cursor == "cursor-a" {
				return PaginationPage[string, string]{
					Items: []string{"one", "two"}, Next: "cursor-b", HasNext: true, ResponseBytes: 2,
				}, nil
			}

			return PaginationPage[string, string]{Items: []string{"three"}, ResponseBytes: 1}, nil
		},
	}
	paginator, err := NewPaginator(options)
	if err != nil {
		t.Fatalf("construct paginator: %v", err)
	}
	value, ok, err := paginator.Next(context.Background())
	if err != nil || !ok || value != "one" {
		t.Fatalf("first item = %q, %v, %v", value, ok, err)
	}
	state := paginator.State()
	state.Buffered[0] = "mutated-copy"
	state = paginator.State()
	resumedOptions := options
	resumedOptions.Resume = &state
	resumed, err := NewPaginator(resumedOptions)
	if err != nil {
		t.Fatalf("resume paginator: %v", err)
	}
	var values []string
	for {
		value, present, nextErr := resumed.Next(context.Background())
		if nextErr != nil {
			t.Fatalf("resume iteration: %v", nextErr)
		}
		if !present {
			break
		}
		values = append(values, value)
	}
	if fmt.Sprint(values) != "[two three]" || fetches != 2 {
		t.Fatalf("resumed values = %v, fetches = %d", values, fetches)
	}
}

func TestPaginatorDetectsContinuationCyclesBeforeRefetch(t *testing.T) {
	t.Parallel()

	fetches := 0
	paginator, err := NewPaginator(PaginationOptions[int, string]{
		Initial: "a",
		Key:     func(cursor string) (string, error) { return cursor, nil },
		Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
			fetches++

			return PaginationPage[int, string]{Items: []int{1}, Next: "a", HasNext: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct paginator: %v", err)
	}
	if _, ok, err := paginator.Next(context.Background()); err != nil || !ok {
		t.Fatalf("first item error = %v, present %v", err, ok)
	}
	_, _, err = paginator.Next(context.Background())
	if !errors.Is(err, ErrPaginationCycle) || fetches != 1 {
		t.Fatalf("cycle error = %v, fetches = %d", err, fetches)
	}
}

func TestPaginatorEnforcesEveryBudgetBeforeExposingPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits PaginationLimits
		page   PaginationPage[int, string]
		clock  *paginationTestClock
	}{
		{name: "items", limits: PaginationLimits{MaximumItems: 1}, page: PaginationPage[int, string]{Items: []int{1, 2}}},
		{name: "bytes", limits: PaginationLimits{MaximumResponseBytes: 1}, page: PaginationPage[int, string]{Items: []int{1}, ResponseBytes: 2}},
		{name: "elapsed", limits: PaginationLimits{MaximumElapsed: time.Second}, page: PaginationPage[int, string]{Items: []int{1}}, clock: &paginationTestClock{advanceOnNow: 2 * time.Second}},
		{name: "empty pages", limits: PaginationLimits{MaximumEmptyPages: 1}, page: PaginationPage[int, string]{Next: "b", HasNext: true}},
		{name: "continuation bytes", limits: PaginationLimits{MaximumContinuationBytes: 1}, page: PaginationPage[int, string]{Next: "long", HasNext: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := test.clock
			if clock == nil {
				clock = &paginationTestClock{now: time.Unix(1_700_000_000, 0)}
			}
			fetches := 0
			paginator, err := NewPaginator(PaginationOptions[int, string]{
				Initial: "a", Key: func(cursor string) (string, error) { return cursor, nil },
				Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
					fetches++

					return test.page, nil
				},
				Clock: clock, Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("construct paginator: %v", err)
			}
			_, _, err = paginator.Next(context.Background())
			if !errors.Is(err, ErrPaginationLimit) {
				t.Fatalf("budget error = %v, fetches = %d", err, fetches)
			}
		})
	}
}

func TestPaginatorHonorsCancellationAndSecretSafeFailures(t *testing.T) {
	t.Parallel()

	secret := errors.New("cursor secret do-not-render")
	paginator, err := NewPaginator(PaginationOptions[int, string]{
		Initial: "secret-cursor",
		Key:     func(string) (string, error) { return "", secret },
		Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
			t.Fatal("fetch must not run")

			return PaginationPage[int, string]{}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct paginator: %v", err)
	}
	_, _, err = paginator.Next(context.Background())
	if !errors.Is(err, secret) || strings.Contains(err.Error(), "do-not-render") || strings.Contains(err.Error(), "secret-cursor") {
		t.Fatalf("continuation error = %q", err)
	}

	paginator, err = NewPaginator(PaginationOptions[int, string]{
		Initial: "a", Key: func(value string) (string, error) { return value, nil },
		Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
			return PaginationPage[int, string]{}, secret
		},
	})
	if err != nil {
		t.Fatalf("construct failing paginator: %v", err)
	}
	_, _, err = paginator.Next(context.Background())
	if !errors.Is(err, secret) || strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("fetch error = %q", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = paginator.Next(canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled iteration error = %v", err)
	}
}

func TestPaginatorRejectsInvalidConfigurationAndResumeState(t *testing.T) {
	t.Parallel()

	validFetch := func(context.Context, string) (PaginationPage[int, string], error) {
		return PaginationPage[int, string]{}, nil
	}
	validKey := func(value string) (string, error) { return value, nil }
	var nilFetch PaginationFetcher[int, string]
	var nilKey PaginationContinuationKey[string]
	var nilClock *paginationTestClock
	for _, options := range []PaginationOptions[int, string]{
		{Fetch: nilFetch, Key: validKey},
		{Fetch: validFetch, Key: nilKey},
		{Fetch: validFetch, Key: validKey, Clock: nilClock},
		{Fetch: validFetch, Key: validKey, Limits: PaginationLimits{MaximumPages: -1}},
		{Fetch: validFetch, Key: validKey, Resume: &PaginationState[int, string]{Pages: -1}},
		{Fetch: validFetch, Key: validKey, Resume: &PaginationState[int, string]{BufferedIndex: 2, Buffered: []int{1}}},
		{Fetch: validFetch, Key: validKey, Resume: &PaginationState[int, string]{HasNext: true, Pages: 101}},
		{Fetch: validFetch, Key: validKey, Resume: &PaginationState[int, string]{}},
		{Fetch: validFetch, Key: validKey, Limits: PaginationLimits{MaximumContinuationBytes: 1}, Resume: &PaginationState[int, string]{HasNext: true, Seen: []string{"long"}}},
		{Fetch: validFetch, Key: validKey, Resume: &PaginationState[int, string]{HasNext: true, Seen: []string{"same", "same"}}},
	} {
		if _, err := NewPaginator(options); !errors.Is(err, ErrInvalidPagination) {
			t.Fatalf("invalid options error = %v", err)
		}
	}
	var nilContext context.Context
	paginator, err := NewPaginator(PaginationOptions[int, string]{Fetch: validFetch, Key: validKey})
	if err != nil {
		t.Fatalf("construct paginator: %v", err)
	}
	if _, _, err := paginator.Next(nilContext); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestPaginatorBoundaryFailuresDoNotExposeOrConsumePages(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		paginator, err := NewPaginator(PaginationOptions[int, int]{
			Initial: 1, Key: func(page int) (string, error) { return fmt.Sprint(page), nil },
			Fetch: func(_ context.Context, page int) (PaginationPage[int, int], error) {
				return PaginationPage[int, int]{Items: []int{page}, Next: page + 1, HasNext: true}, nil
			},
			Limits: PaginationLimits{MaximumPages: 1},
		})
		if err != nil {
			t.Fatalf("construct paginator: %v", err)
		}
		if _, ok, err := paginator.Next(context.Background()); err != nil || !ok {
			t.Fatalf("first page item = %v, %v", ok, err)
		}
		if _, _, err := paginator.Next(context.Background()); !errors.Is(err, ErrPaginationLimit) {
			t.Fatalf("page limit error = %v", err)
		}
	})

	tests := []struct {
		name    string
		initial string
		key     PaginationContinuationKey[string]
		fetch   PaginationFetcher[int, string]
		limits  PaginationLimits
		want    error
	}{
		{
			name: "current continuation bytes", initial: "long",
			key: func(value string) (string, error) { return value, nil },
			fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				t.Fatal("fetch must not run")

				return PaginationPage[int, string]{}, nil
			},
			limits: PaginationLimits{MaximumContinuationBytes: 1}, want: ErrPaginationLimit,
		},
		{
			name: "negative response bytes", initial: "a",
			key: func(value string) (string, error) { return value, nil },
			fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				return PaginationPage[int, string]{ResponseBytes: -1}, nil
			},
			want: ErrInvalidPagination,
		},
		{
			name: "next continuation key", initial: "a",
			key: func(value string) (string, error) {
				if value == "b" {
					return "", errors.New("next key failure")
				}

				return value, nil
			},
			fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				return PaginationPage[int, string]{Items: []int{1}, Next: "b", HasNext: true}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paginator, err := NewPaginator(PaginationOptions[int, string]{
				Initial: test.initial, Key: test.key, Fetch: test.fetch, Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("construct paginator: %v", err)
			}
			_, ok, nextErr := paginator.Next(context.Background())
			if ok || nextErr == nil {
				t.Fatalf("boundary result = %v, %v", ok, nextErr)
			}
			if test.want != nil && !errors.Is(nextErr, test.want) {
				t.Fatalf("boundary cause = %v", nextErr)
			}
		})
	}

	t.Run("elapsed after fetch", func(t *testing.T) {
		clock := &paginationTestClock{now: time.Unix(1_700_000_000, 0)}
		paginator, err := NewPaginator(PaginationOptions[int, string]{
			Initial: "a", Key: func(value string) (string, error) { return value, nil },
			Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				clock.now = clock.now.Add(2 * time.Second)

				return PaginationPage[int, string]{Items: []int{1}}, nil
			},
			Clock: clock, Limits: PaginationLimits{MaximumElapsed: time.Second},
		})
		if err != nil {
			t.Fatalf("construct paginator: %v", err)
		}
		if _, _, err := paginator.Next(context.Background()); !errors.Is(err, ErrPaginationLimit) {
			t.Fatalf("post-fetch elapsed error = %v", err)
		}
	})

	t.Run("empty terminal page", func(t *testing.T) {
		paginator, err := NewPaginator(PaginationOptions[int, string]{
			Key: func(value string) (string, error) { return value, nil },
			Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				return PaginationPage[int, string]{}, nil
			},
		})
		if err != nil {
			t.Fatalf("construct paginator: %v", err)
		}
		if _, ok, err := paginator.Next(context.Background()); err != nil || ok || !paginator.State().Done {
			t.Fatalf("empty terminal result = %v, %v, %#v", ok, err, paginator.State())
		}
	})

	t.Run("backward and overflowing elapsed time", func(t *testing.T) {
		clock := &paginationTestClock{now: time.Unix(100, 0)}
		resume := PaginationState[int, string]{HasNext: true, Elapsed: time.Duration(1<<63 - 2)}
		paginator, err := NewPaginator(PaginationOptions[int, string]{
			Key: func(value string) (string, error) { return value, nil },
			Fetch: func(context.Context, string) (PaginationPage[int, string], error) {
				return PaginationPage[int, string]{}, nil
			},
			Clock: clock, Limits: PaginationLimits{MaximumElapsed: time.Duration(1<<63 - 1)}, Resume: &resume,
		})
		if err != nil {
			t.Fatalf("construct paginator: %v", err)
		}
		clock.now = clock.now.Add(-time.Second)
		if err := paginator.updateElapsed(); err != nil {
			t.Fatalf("backward elapsed error = %v", err)
		}
		clock.now = paginator.started.Add(2 * time.Second)
		if err := paginator.updateElapsed(); !errors.Is(err, ErrPaginationLimit) {
			t.Fatalf("overflow elapsed error = %v", err)
		}
	})
}

type paginationTestClock struct {
	now          time.Time
	advanceOnNow time.Duration
}

func (clock *paginationTestClock) Now() time.Time {
	result := clock.now
	clock.now = clock.now.Add(clock.advanceOnNow)

	return result
}

func (*paginationTestClock) Wait(context.Context, time.Duration) error { return nil }
