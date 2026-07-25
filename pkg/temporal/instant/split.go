package instant

import (
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// SplitForward partitions p from start to end by a positive fixed step.
// Internal cut points belong to the later fragment.
func (p Period) SplitForward(step time.Duration, limits temporal.Limits) ([]Period, error) {
	limits, err := splitLimits(step, limits)
	if err != nil {
		return nil, err
	}
	if p.IsEmpty() {
		return nil, nil
	}
	if p.IsSingleton() {
		return []Period{p}, nil
	}

	result := make([]Period, 0, min(limits.Steps, 16))
	for cursor := p.start; cursor.Before(p.end); {
		if len(result) == limits.Steps || len(result) == limits.OutputPeriods {
			return nil, splitLimitError(len(result)+1, limits)
		}

		next := cursor.Add(step)
		if next.After(p.end) {
			next = p.end
		}
		includeStart := cursor.Equal(p.start) && p.bounds.IncludesStart()
		if !cursor.Equal(p.start) {
			includeStart = true
		}
		includeEnd := next.Equal(p.end) && p.bounds.IncludesEnd()
		result = append(result, Period{
			start:  cursor,
			end:    next,
			bounds: boundsFrom(includeStart, includeEnd),
		})
		cursor = next
	}

	return result, nil
}

// SplitBackward partitions p from end to start by a positive fixed step. The
// returned slice remains ascending; internal cut points belong to the earlier
// fragment.
func (p Period) SplitBackward(step time.Duration, limits temporal.Limits) ([]Period, error) {
	limits, err := splitLimits(step, limits)
	if err != nil {
		return nil, err
	}
	if p.IsEmpty() {
		return nil, nil
	}
	if p.IsSingleton() {
		return []Period{p}, nil
	}

	reversed := make([]Period, 0, min(limits.Steps, 16))
	for cursor := p.end; cursor.After(p.start); {
		if len(reversed) == limits.Steps || len(reversed) == limits.OutputPeriods {
			return nil, splitLimitError(len(reversed)+1, limits)
		}

		previous := cursor.Add(-step)
		if previous.Before(p.start) {
			previous = p.start
		}
		includeStart := previous.Equal(p.start) && p.bounds.IncludesStart()
		includeEnd := cursor.Equal(p.end) && p.bounds.IncludesEnd()
		if !cursor.Equal(p.end) {
			includeEnd = true
		}
		reversed = append(reversed, Period{
			start:  previous,
			end:    cursor,
			bounds: boundsFrom(includeStart, includeEnd),
		})
		cursor = previous
	}

	result := make([]Period, len(reversed))
	for index := range reversed {
		result[len(reversed)-1-index] = reversed[index]
	}

	return result, nil
}

func splitLimits(step time.Duration, limits temporal.Limits) (temporal.Limits, error) {
	if step <= 0 {
		return temporal.Limits{}, temporal.ErrStep
	}
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return temporal.Limits{}, err
	}

	return limits, nil
}

func splitLimitError(value int, limits temporal.Limits) error {
	field := "steps"
	maximum := limits.Steps
	if limits.OutputPeriods < maximum {
		field = "output_periods"
		maximum = limits.OutputPeriods
	}

	return &temporal.LimitError{Field: field, Value: value, Max: maximum}
}
