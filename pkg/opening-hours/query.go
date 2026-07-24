package openinghours

import "slices"

type segment struct {
	start int64
	end   int64
}

// RuleKind identifies the broad rule source used by a query.
type RuleKind uint8

const (
	// RuleNone means no schedule rule supplied availability.
	RuleNone RuleKind = iota
	// RuleWeekly means the current date's weekly rule supplied availability.
	RuleWeekly
	// RuleWeeklySpill means the preceding date's overnight rule supplied it.
	RuleWeeklySpill
	// RuleException means a dated exception supplied the final result.
	RuleException
	// RuleComposition means schedule algebra supplied the final result.
	RuleComposition
	// RuleOutsideEffective means the queried date is outside configured dates.
	RuleOutsideEffective
)

// Explanation reports bounded, non-sensitive rule provenance.
type Explanation struct {
	Rule     RuleKind
	Timezone string
	Source   string
	Revision string
}

// Availability is the explained result of a point query.
type Availability struct {
	Open        bool
	Explanation Explanation
}

func (s Schedule) isOpenCivil(date Date, localTime LocalTime) (Availability, error) {
	inside, err := s.withinEffective(date)
	if err != nil {
		return Availability{}, err
	}
	if !inside {
		return Availability{Explanation: Explanation{
			Rule: RuleOutsideEffective, Timezone: s.data.timezone,
		}}, nil
	}

	segments, spill, exceptions, err := s.effectiveSegments(date)
	if err != nil {
		return Availability{}, err
	}
	open := contains(segments, localTime.nanosecond)
	explanation := Explanation{Timezone: s.data.timezone, Rule: RuleWeekly}
	if s.data.composition != nil {
		explanation.Rule = RuleComposition
	} else if len(exceptions) > 0 {
		last := exceptions[len(exceptions)-1]
		explanation.Rule = RuleException
		explanation.Source = last.source
		explanation.Revision = last.revision
	} else if spill && open {
		explanation.Rule = RuleWeeklySpill
	}

	return Availability{Open: open, Explanation: explanation}, nil
}

func (s Schedule) effectiveSegments(date Date) ([]segment, bool, []Exception, error) {
	inside, err := s.withinEffective(date)
	if err != nil {
		return nil, false, nil, err
	}
	if !inside {
		return nil, false, nil, nil
	}
	if s.data.composition != nil {
		left, _, _, err := s.data.composition.left.effectiveSegments(date)
		if err != nil {
			return nil, false, nil, err
		}
		right, _, _, err := s.data.composition.right.effectiveSegments(date)
		if err != nil {
			return nil, false, nil, err
		}
		switch s.data.composition.operation {
		case compositionUnion:
			return unionSegments(left, right), false, nil, nil
		case compositionIntersection:
			return intersectSegments(left, right), false, nil, nil
		case compositionSubtract:
			return subtractSegments(left, right), false, nil, nil
		case compositionOverlay:
			// The right operand was evaluated above, so its effective-range
			// policy has already succeeded for this date.
			mask, _ := s.data.composition.right.overlayMask(date)
			return unionSegments(subtractSegments(left, mask), right), false, nil, nil
		}
	}
	current := ruleSegments(s.data.weekly[date.Weekday()])
	current = clipSegments(current, 0, nanosecondsPerDay, 0)
	previousDate, err := addDate(date, -1)
	if err != nil {
		previousDate = Date{}
	}
	var previousOwned []segment
	if validDate(previousDate) {
		previousOwned = s.ownedSegments(previousDate)
	}
	spillSegments := clipSegments(previousOwned, nanosecondsPerDay, 2*nanosecondsPerDay, -nanosecondsPerDay)
	combined := unionSegments(current, spillSegments)
	exceptions := s.exceptionsFor(date)
	combined = applyExceptions(combined, exceptions)
	combined = clipSegments(combined, 0, nanosecondsPerDay, 0)

	return combined, len(spillSegments) > 0, exceptions, nil
}

func (s Schedule) overlayMask(date Date) ([]segment, error) {
	inside, err := s.withinEffective(date)
	if err != nil {
		return nil, err
	}
	if !inside {
		return nil, nil
	}
	if s.data.composition != nil || s.data.weekly[date.Weekday()].state != DayInherited ||
		len(s.exceptionsFor(date)) > 0 {
		return []segment{{start: 0, end: nanosecondsPerDay}}, nil
	}
	previous, _ := addDate(date, -1)
	if !validDate(previous) {
		return nil, nil
	}
	owned := s.ownedSegments(previous)

	return clipSegments(owned, nanosecondsPerDay, 2*nanosecondsPerDay, -nanosecondsPerDay), nil
}

func (s Schedule) ownedSegments(date Date) []segment {
	base := ruleSegments(s.data.weekly[date.Weekday()])

	return applyExceptions(base, s.exceptionsFor(date))
}

func ruleSegments(rule DayRule) []segment {
	switch rule.state {
	case DayOpenAllDay:
		return []segment{{start: 0, end: nanosecondsPerDay}}
	case DayOpenRanges:
		result := make([]segment, 0, len(rule.ranges))
		for _, item := range rule.ranges {
			end := item.end.nanosecond
			if item.Overnight() {
				end += nanosecondsPerDay
			}
			result = append(result, segment{start: item.start.nanosecond, end: end})
		}
		return normalizeSegments(result)
	case DayInherited, DayClosed:
		return nil
	default:
		return nil
	}
}

func applyExceptions(base []segment, exceptions []Exception) []segment {
	result := slices.Clone(base)
	for _, exception := range exceptions {
		switch exception.operation {
		case ExceptionClose:
			result = nil
		case ExceptionReplace:
			result = ruleSegments(exception.rule)
		case ExceptionAdd:
			result = unionSegments(result, ruleSegments(exception.rule))
		case ExceptionSubtract:
			result = subtractSegments(result, ruleSegments(exception.rule))
		}
	}

	return result
}

func normalizeSegments(input []segment) []segment {
	if len(input) == 0 {
		return nil
	}
	result := make([]segment, len(input))
	copy(result, input)
	slices.SortFunc(result, compareSegment)
	output := result[:1]
	for _, item := range result[1:] {
		last := &output[len(output)-1]
		if item.start <= last.end {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		output = append(output, item)
	}

	return output
}

func compareSegment(left, right segment) int {
	if left.start < right.start {
		return -1
	}
	if left.start > right.start {
		return 1
	}
	if left.end < right.end {
		return -1
	}
	if left.end > right.end {
		return 1
	}
	return 0
}

func unionSegments(left, right []segment) []segment {
	combined := make([]segment, 0, len(left)+len(right))
	combined = append(combined, left...)
	combined = append(combined, right...)

	return normalizeSegments(combined)
}

func intersectSegments(left, right []segment) []segment {
	left = normalizeSegments(left)
	right = normalizeSegments(right)
	result := make([]segment, 0, min(len(left), len(right)))
	for leftIndex, rightIndex := 0, 0; leftIndex < len(left) && rightIndex < len(right); {
		start := max(left[leftIndex].start, right[rightIndex].start)
		end := min(left[leftIndex].end, right[rightIndex].end)
		if start < end {
			result = append(result, segment{start: start, end: end})
		}
		if left[leftIndex].end < right[rightIndex].end {
			leftIndex++
		} else {
			rightIndex++
		}
	}

	return result
}

func subtractSegments(base, removals []segment) []segment {
	result := slices.Clone(base)
	for _, removal := range normalizeSegments(removals) {
		next := make([]segment, 0, len(result)+1)
		for _, item := range result {
			if removal.end <= item.start || removal.start >= item.end {
				next = append(next, item)
				continue
			}
			if removal.start > item.start {
				next = append(next, segment{start: item.start, end: removal.start})
			}
			if removal.end < item.end {
				next = append(next, segment{start: removal.end, end: item.end})
			}
		}
		result = next
	}

	return result
}

func clipSegments(input []segment, minimum, maximum, shift int64) []segment {
	result := make([]segment, 0, len(input))
	for _, item := range input {
		start := max(item.start, minimum)
		end := min(item.end, maximum)
		if start < end {
			result = append(result, segment{start: start + shift, end: end + shift})
		}

	}

	return result
}

func contains(segments []segment, point int64) bool {
	for _, item := range segments {
		if point >= item.start && point < item.end {
			return true
		}
	}

	return false
}
