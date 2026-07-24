package openinghours

import (
	"slices"
	"unicode/utf8"
)

const (
	maxExceptions      = 4096
	maxProvenanceBytes = 128
)

// ExceptionOperation defines how a dated rule changes inherited availability.
type ExceptionOperation uint8

const (
	// ExceptionReplace replaces availability for its civil date.
	ExceptionReplace ExceptionOperation = iota
	// ExceptionAdd unions availability into its civil date.
	ExceptionAdd
	// ExceptionSubtract removes availability from its civil date.
	ExceptionSubtract
	// ExceptionClose closes its civil date completely.
	ExceptionClose
)

// ConflictPolicy controls equal-priority exception handling.
type ConflictPolicy uint8

const (
	// RejectAmbiguous rejects equal-priority rules without unique precedence.
	RejectAmbiguous ConflictPolicy = iota
	// ResolveCanonical resolves equal priorities by stable provenance order.
	ResolveCanonical
)

// ExceptionConfig is copied by NewException.
type ExceptionConfig struct {
	Date      Date
	Operation ExceptionOperation
	Rule      DayRule
	Priority  int
	Source    string
	Revision  string
}

// Exception is an immutable exact-date availability operation.
type Exception struct {
	date      Date
	operation ExceptionOperation
	rule      DayRule
	priority  int
	source    string
	revision  string
	set       string
}

// Date returns the exact exception date.
func (e Exception) Date() Date { return e.date }

// Operation returns the exception operation.
func (e Exception) Operation() ExceptionOperation { return e.operation }

// Rule returns a detached immutable rule.
func (e Exception) Rule() DayRule { return cloneRule(e.rule) }

// Priority returns the deterministic precedence priority.
func (e Exception) Priority() int { return e.priority }

// Source returns bounded provenance.
func (e Exception) Source() string { return e.source }

// Revision returns bounded provenance revision.
func (e Exception) Revision() string { return e.revision }

// Set returns the optional named exception set.
func (e Exception) Set() string { return e.set }

// ExceptionSet is an immutable named group flattened before evaluation.
type ExceptionSet struct {
	name       string
	exceptions []Exception
}

// NewExceptionSet validates, names, and copies a non-empty exception group.
func NewExceptionSet(name string, input []Exception) (ExceptionSet, error) {
	if name == "" || len(name) > maxProvenanceBytes || !utf8.ValidString(name) || len(input) == 0 {
		return ExceptionSet{}, newError("new exception set", CodeInvalidState)
	}
	if len(input) > maxExceptions {
		return ExceptionSet{}, newError("new exception set", CodeLimitExceeded)
	}
	exceptions := slices.Clone(input)
	for index := range exceptions {
		if !validDate(exceptions[index].date) || exceptions[index].source == "" || exceptions[index].revision == "" {
			return ExceptionSet{}, newError("new exception set", CodeInvalidState)
		}
		exceptions[index].rule = cloneRule(exceptions[index].rule)
		exceptions[index].set = name
	}

	return ExceptionSet{name: name, exceptions: exceptions}, nil
}

// Name returns the bounded set name.
func (set ExceptionSet) Name() string { return set.name }

// Exceptions returns detached exception values and range slices.
func (set ExceptionSet) Exceptions() []Exception {
	result := slices.Clone(set.exceptions)
	for index := range result {
		result[index].rule = cloneRule(result[index].rule)
	}

	return result
}

// ExceptionRangeConfig expands a bounded inclusive civil-date range.
type ExceptionRangeConfig struct {
	Name         string
	Start        Date
	End          Date
	MaximumDates int
	Operation    ExceptionOperation
	Rule         DayRule
	Priority     int
	Source       string
	Revision     string
}

// ExpandExceptionRange resolves a multi-day rule to deterministic exact dates.
func ExpandExceptionRange(config ExceptionRangeConfig) (ExceptionSet, error) {
	if !validDate(config.Start) || !validDate(config.End) || compareDate(config.Start, config.End) > 0 {
		return ExceptionSet{}, newError("expand exception range", CodeInvalidDate)
	}
	if config.MaximumDates <= 0 || config.MaximumDates > maxExceptions {
		return ExceptionSet{}, newError("expand exception range", CodeLimitExceeded)
	}
	exceptions := make([]Exception, 0, min(config.MaximumDates, 32))
	for date := config.Start; ; {
		if len(exceptions) == config.MaximumDates {
			return ExceptionSet{}, newError("expand exception range", CodeLimitExceeded)
		}
		exception, err := NewException(ExceptionConfig{
			Date: date, Operation: config.Operation, Rule: config.Rule,
			Priority: config.Priority, Source: config.Source, Revision: config.Revision,
		})
		if err != nil {
			return ExceptionSet{}, err
		}
		exceptions = append(exceptions, exception)
		if date == config.End {
			break
		}
		date, _ = addDate(date, 1) // Valid ordered dates cannot overflow before End.
	}

	return NewExceptionSet(config.Name, exceptions)
}

// NewException validates an exact-date exception and its bounded provenance.
func NewException(config ExceptionConfig) (Exception, error) {
	if !validDate(config.Date) {
		return Exception{}, newError("new exception", CodeInvalidDate)
	}
	if config.Operation > ExceptionClose || config.Priority < -1_000_000 || config.Priority > 1_000_000 {
		return Exception{}, newError("new exception", CodeInvalidState)
	}
	if len(config.Source) > maxProvenanceBytes || len(config.Revision) > maxProvenanceBytes {
		return Exception{}, newError("new exception", CodeLimitExceeded)
	}
	if config.Source == "" || config.Revision == "" {
		return Exception{}, newError("new exception", CodeInvalidState)
	}
	if config.Operation == ExceptionClose {
		if config.Rule.state != DayInherited {
			return Exception{}, newError("new exception", CodeInvalidState)
		}
	} else if config.Rule.state == DayInherited || config.Rule.state == DayClosed {
		return Exception{}, newError("new exception", CodeInvalidState)
	}

	return Exception{
		date: config.Date, operation: config.Operation, rule: cloneRule(config.Rule),
		priority: config.Priority, source: config.Source, revision: config.Revision,
	}, nil
}

func normalizeExceptions(input []Exception, policy ConflictPolicy) ([]Exception, error) {
	if len(input) > maxExceptions {
		return nil, newError("normalize exceptions", CodeLimitExceeded)
	}
	if policy > ResolveCanonical {
		return nil, newError("normalize exceptions", CodeInvalidState)
	}

	result := slices.Clone(input)
	type revisionKey struct {
		date             Date
		source, revision string
	}
	seenRevisions := make(map[revisionKey]struct{}, len(result))
	for index := range result {
		result[index].rule = cloneRule(result[index].rule)
		key := revisionKey{
			date: result[index].date, source: result[index].source,
			revision: result[index].revision,
		}
		if _, duplicate := seenRevisions[key]; duplicate {
			return nil, newError("normalize exceptions", CodeDuplicateRevision)
		}
		seenRevisions[key] = struct{}{}
	}
	slices.SortFunc(result, compareException)
	for index := 1; index < len(result); index++ {
		previous, current := result[index-1], result[index]
		if previous.date != current.date {
			continue
		}
		if previous.priority == current.priority && policy == RejectAmbiguous {
			return nil, newError("normalize exceptions", CodeAmbiguousException)
		}
	}

	return result, nil
}

func compareException(left, right Exception) int {
	if comparison := compareDate(left.date, right.date); comparison != 0 {
		return comparison
	}
	if left.priority < right.priority {
		return -1
	}
	if left.priority > right.priority {
		return 1
	}
	if left.source < right.source {
		return -1
	}
	if left.source > right.source {
		return 1
	}
	if left.revision < right.revision {
		return -1
	}
	if left.revision > right.revision {
		return 1
	}

	return int(left.operation) - int(right.operation)
}

func (s Schedule) exceptionsFor(date Date) []Exception {
	if s.data == nil {
		return nil
	}

	start, _ := slices.BinarySearchFunc(s.data.exceptions, Exception{date: date}, func(item, target Exception) int {
		return compareDate(item.date, target.date)
	})
	if start == len(s.data.exceptions) || s.data.exceptions[start].date != date {
		return nil
	}
	end := start
	for end < len(s.data.exceptions) && s.data.exceptions[end].date == date {
		end++
	}

	return s.data.exceptions[start:end]
}
