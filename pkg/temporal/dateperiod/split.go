package dateperiod

import (
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// SplitDays partitions represented dates into closed chunks of at most step
// calendar days. Structural endpoint bounds are canonicalized while set
// membership is conserved exactly.
func (p Period) SplitDays(step int, limits temporal.Limits) ([]Period, error) {
	if step <= 0 {
		return nil, temporal.ErrStep
	}
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	first, last, ok := p.includedRange()
	if !ok {
		return nil, nil
	}

	days := first.DaysUntil(last) + 1
	count := days / step
	if days%step != 0 {
		count++
	}
	if count > limits.Steps {
		return nil, &temporal.LimitError{Field: "steps", Value: count, Max: limits.Steps}
	}

	result := make([]Period, 0, count)
	start := first
	remaining := days
	for remaining > 0 {
		chunkDays := min(step, remaining)
		end := last
		if chunkDays < remaining {
			end, _ = start.AddDays(chunkDays - 1)
		}
		chunk, _ := New(start, end, temporal.Closed)
		result = append(result, chunk)
		remaining -= chunkDays
		if remaining > 0 {
			start, _ = end.AddDays(1)
		}
	}
	return result, nil
}
