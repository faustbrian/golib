package dateperiod

import (
	"iter"
	"sort"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// All returns a stable iterator over normalized civil-date periods.
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
func (s Set) Search(date calendar.Date) (int, bool) {
	if !date.IsValid() {
		return 0, false
	}
	index := sort.Search(len(s.periods), func(index int) bool {
		comparison, _ := s.periods[index].end.Compare(date)
		return comparison >= 0
	})
	for candidate := index; candidate < len(s.periods) && candidate <= index+1; candidate++ {
		if s.periods[candidate].Includes(date) {
			return candidate, true
		}
		comparison, _ := s.periods[candidate].start.Compare(date)
		if comparison > 0 {
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
