package openinghours

import (
	"slices"
	"time"

	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

// DayState distinguishes absence, ranged opening, full-day opening, and closure.
type DayState uint8

const (
	// DayInherited delegates to a lower-precedence rule source.
	DayInherited DayState = iota
	// DayOpenRanges opens only the rule's explicit local-time ranges.
	DayOpenRanges
	// DayOpenAllDay opens the complete owned civil day.
	DayOpenAllDay
	// DayClosed explicitly closes the owned civil day.
	DayClosed
)

// OverlapPolicy makes normalization of overlapping or adjacent input explicit.
type OverlapPolicy uint8

const (
	// RejectOverlap rejects overlapping ranges but permits adjacency.
	RejectOverlap OverlapPolicy = iota
	// RejectOverlapAndAdjacent rejects both overlap and adjacency.
	RejectOverlapAndAdjacent
	// MergeOverlap merges overlap but preserves adjacent ranges.
	MergeOverlap
	// MergeAdjacent merges both overlapping and adjacent ranges.
	MergeAdjacent
)

// DayRule is an immutable weekly or dated availability rule.
type DayRule struct {
	state  DayState
	ranges []Range
}

// Inherited returns a rule that delegates to the lower-precedence source.
func Inherited() DayRule { return DayRule{state: DayInherited} }

// OpenAllDay returns an explicit full-day rule.
func OpenAllDay() DayRule { return DayRule{state: DayOpenAllDay} }

// Closed returns an explicit closure rule.
func Closed() DayRule { return DayRule{state: DayClosed} }

// OpenRanges constructs a canonical ranged rule using the selected policy.
func OpenRanges(input []Range, policy OverlapPolicy) (DayRule, error) {
	if len(input) == 0 || len(input) > maxRangesPerDay {
		return DayRule{}, newError("open ranges", CodeLimitExceeded)
	}
	if policy > MergeAdjacent {
		return DayRule{}, newError("open ranges", CodeInvalidState)
	}

	ranges := slices.Clone(input)
	for _, item := range ranges {
		if !item.start.valid() || !item.end.valid() || item.start == item.end {
			return DayRule{}, newError("open ranges", CodeInvalidRange)
		}
	}
	slices.SortFunc(ranges, compareRange)

	type linearRange struct{ start, end int64 }
	normalized := make([]linearRange, 0, len(ranges))
	for _, item := range ranges {
		end := item.end.nanosecond
		if item.Overnight() {
			end += nanosecondsPerDay
		}
		current := linearRange{start: item.start.nanosecond, end: end}
		if len(normalized) == 0 {
			normalized = append(normalized, current)
			continue
		}

		previous := &normalized[len(normalized)-1]
		overlaps := current.start < previous.end
		adjacent := current.start == previous.end
		if overlaps {
			if policy == RejectOverlap || policy == RejectOverlapAndAdjacent {
				return DayRule{}, newError("open ranges", CodeOverlap)
			}
			if current.end > previous.end {
				previous.end = current.end
			}
			if previous.end-previous.start > nanosecondsPerDay {
				return DayRule{}, newError("open ranges", CodeDayBoundaryOverflow)
			}
			continue
		}
		if adjacent {
			if policy == RejectOverlapAndAdjacent {
				return DayRule{}, newError("open ranges", CodeAdjacent)
			}
			if policy == MergeAdjacent {
				previous.end = current.end
				if previous.end-previous.start > nanosecondsPerDay {
					return DayRule{}, newError("open ranges", CodeDayBoundaryOverflow)
				}
				continue
			}
		}
		normalized = append(normalized, current)
	}

	result := make([]Range, 0, len(normalized))
	for _, item := range normalized {
		if item.end-item.start == nanosecondsPerDay {
			if item.start == 0 && len(normalized) == 1 {
				return OpenAllDay(), nil
			}
			return DayRule{}, newError("open ranges", CodeDayBoundaryOverflow)
		}
		end := item.end
		if end >= nanosecondsPerDay {
			end -= nanosecondsPerDay
		}
		result = append(result, Range{
			start: LocalTime{nanosecond: item.start},
			end:   LocalTime{nanosecond: end},
		})
	}

	return DayRule{state: DayOpenRanges, ranges: result}, nil
}

// State returns the explicit day state.
func (rule DayRule) State() DayState { return rule.state }

// Ranges returns a detached copy of canonical ranges.
func (rule DayRule) Ranges() []Range { return slices.Clone(rule.ranges) }

func compareRange(left, right Range) int {
	if left.start.nanosecond < right.start.nanosecond {
		return -1
	}
	if left.start.nanosecond > right.start.nanosecond {
		return 1
	}
	if left.end.nanosecond < right.end.nanosecond {
		return -1
	}
	if left.end.nanosecond > right.end.nanosecond {
		return 1
	}
	return 0
}

// Config is copied by NewSchedule.
type Config struct {
	Timezone         string
	Weekly           map[time.Weekday]DayRule
	Exceptions       []Exception
	ExceptionSets    []ExceptionSet
	ConflictPolicy   ConflictPolicy
	Metadata         Metadata
	EffectiveStart   *Date
	EffectiveEnd     *Date
	OutsideEffective OutsideEffectivePolicy
}

type scheduleData struct {
	timezone          string
	location          *time.Location
	weekly            [7]DayRule
	exceptions        []Exception
	composition       *composition
	depth             int
	metadata          Metadata
	effectiveStart    Date
	hasEffectiveStart bool
	effectiveEnd      Date
	hasEffectiveEnd   bool
	outsideEffective  OutsideEffectivePolicy
}

// Schedule is an immutable availability value. Its zero value is closed and
// carries no timezone; it never means always open.
type Schedule struct {
	data *scheduleData
}

// NewSchedule validates and copies all caller-owned input.
func NewSchedule(config Config) (Schedule, error) {
	location, err := calendartz.LoadLocation(config.Timezone)
	if err != nil {
		return Schedule{}, newError("new schedule", CodeInvalidTimezone)
	}

	data := &scheduleData{timezone: config.Timezone, location: location, depth: 1}
	metadata, err := validateMetadata(config.Metadata)
	if err != nil {
		return Schedule{}, err
	}
	data.metadata = metadata
	if config.OutsideEffective > OutsideError {
		return Schedule{}, newError("new schedule", CodeInvalidState)
	}
	data.outsideEffective = config.OutsideEffective
	if config.EffectiveStart != nil {
		if !validDate(*config.EffectiveStart) {
			return Schedule{}, newError("new schedule", CodeInvalidDate)
		}
		data.effectiveStart, data.hasEffectiveStart = *config.EffectiveStart, true
	}
	if config.EffectiveEnd != nil {
		if !validDate(*config.EffectiveEnd) {
			return Schedule{}, newError("new schedule", CodeInvalidDate)
		}
		data.effectiveEnd, data.hasEffectiveEnd = *config.EffectiveEnd, true
	}
	if data.hasEffectiveStart && data.hasEffectiveEnd && compareDate(data.effectiveStart, data.effectiveEnd) > 0 {
		return Schedule{}, newError("new schedule", CodeInvalidInterval)
	}
	for weekday, rule := range config.Weekly {
		if weekday < time.Sunday || weekday > time.Saturday {
			return Schedule{}, newError("new schedule", CodeInvalidWeekday)
		}
		if rule.state > DayClosed {
			return Schedule{}, newError("new schedule", CodeInvalidState)
		}
		data.weekly[weekday] = cloneRule(rule)
	}
	allExceptions := slices.Clone(config.Exceptions)
	for _, set := range config.ExceptionSets {
		if set.name == "" || len(set.exceptions) == 0 {
			return Schedule{}, newError("new schedule", CodeInvalidState)
		}
		allExceptions = append(allExceptions, set.exceptions...)
		if len(allExceptions) > maxExceptions {
			return Schedule{}, newError("new schedule", CodeLimitExceeded)
		}
	}
	exceptions, err := normalizeExceptions(allExceptions, config.ConflictPolicy)
	if err != nil {
		return Schedule{}, err
	}
	data.exceptions = exceptions

	return Schedule{data: data}, nil
}

func cloneRule(rule DayRule) DayRule {
	rule.ranges = slices.Clone(rule.ranges)

	return rule
}

// DayResult is a detached result safe for caller mutation.
type DayResult struct {
	State  DayState
	Ranges []Range
}

// Ranges resolves the effective weekly rule for date. Missing and inherited
// weekly rules are closed because there is no lower-precedence source.
func (s Schedule) Ranges(date Date) (DayResult, error) {
	if !validDate(date) {
		return DayResult{}, newError("ranges", CodeInvalidDate)
	}
	if s.data == nil {
		return DayResult{State: DayClosed}, nil
	}

	rule := s.data.weekly[date.Weekday()]
	if rule.state == DayInherited {
		rule = Closed()
	}

	return DayResult{State: rule.state, Ranges: slices.Clone(rule.ranges)}, nil
}

// Timezone returns the explicit IANA timezone identity, or empty for zero value.
func (s Schedule) Timezone() string {
	if s.data == nil {
		return ""
	}

	return s.data.timezone
}
