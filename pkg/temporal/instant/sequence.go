package instant

import (
	"iter"
	"sort"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// All returns a stable iterator over normalized periods.
func (s Set) All() iter.Seq[Period] {
	return func(yield func(Period) bool) {
		for _, period := range s.periods {
			if !yield(period) {
				return
			}
		}
	}
}

// Search returns the containing period index or the stable insertion index.
func (s Set) Search(value time.Time) (int, bool) {
	index := sort.Search(len(s.periods), func(index int) bool {
		return !s.periods[index].end.Before(value)
	})
	for candidate := index; candidate < len(s.periods) && candidate <= index+1; candidate++ {
		if s.periods[candidate].Includes(value) {
			return candidate, true
		}
		if s.periods[candidate].start.After(value) {
			break
		}
	}
	return index, false
}

// Transform applies mapper in stable order and returns a newly normalized set.
func (s Set) Transform(mapper func(Period) (Period, error)) (Set, error) {
	if mapper == nil {
		return Set{}, temporal.ErrUnsupported
	}
	periods := make([]Period, len(s.periods))
	for index, period := range s.periods {
		mapped, err := mapper(period)
		if err != nil {
			return Set{}, err
		}
		periods[index] = mapped
	}
	return NewSet(s.effectiveLimits(), periods...)
}

// Reduce folds periods in stable order without exposing mutable storage.
func Reduce[T any](set Set, initial T, reducer func(T, Period) (T, error)) (T, error) {
	if reducer == nil {
		return initial, temporal.ErrUnsupported
	}
	result := initial
	for _, period := range set.periods {
		var err error
		result, err = reducer(result, period)
		if err != nil {
			return initial, err
		}
	}
	return result, nil
}
