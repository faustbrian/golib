package openinghours

import (
	"strconv"
	"strings"
)

const (
	// MaxHumanSummaryBytes bounds presentation-oriented schedule summaries.
	MaxHumanSummaryBytes = 64 << 10
)

// HumanSummary returns deterministic presentation text that is not a wire
// encoding and cannot be parsed by UnmarshalText. It excludes labels and
// exception provenance, reporting only the number of dated exceptions.
func (s Schedule) HumanSummary() (string, error) {
	if s.data == nil {
		return "closed schedule (timezone unset)", nil
	}

	var builder strings.Builder
	builder.Grow(512)
	builder.WriteString("timezone ")
	builder.WriteString(s.data.timezone)
	if s.data.composition != nil {
		builder.WriteString("; composition ")
		builder.WriteString(compositionName(s.data.composition.operation))
		builder.WriteString("; depth ")
		builder.WriteString(strconv.Itoa(s.data.depth))

		return boundedHumanSummary(builder.String())
	}

	for weekday, name := range weekdayNames {
		builder.WriteString("; ")
		builder.WriteString(name)
		builder.WriteByte(' ')
		writeHumanRule(&builder, s.data.weekly[weekday])
	}
	builder.WriteString("; exceptions ")
	builder.WriteString(strconv.Itoa(len(s.data.exceptions)))
	if s.data.hasEffectiveStart || s.data.hasEffectiveEnd {
		builder.WriteString("; effective ")
		if s.data.hasEffectiveStart {
			builder.WriteString(formatDate(s.data.effectiveStart))
		} else {
			builder.WriteString("unbounded")
		}
		builder.WriteString(" through ")
		if s.data.hasEffectiveEnd {
			builder.WriteString(formatDate(s.data.effectiveEnd))
		} else {
			builder.WriteString("unbounded")
		}
	}
	builder.WriteString("; outside ")
	if s.data.outsideEffective == OutsideError {
		builder.WriteString("error")
	} else {
		builder.WriteString("closed")
	}

	return boundedHumanSummary(builder.String())
}

func writeHumanRule(builder *strings.Builder, rule DayRule) {
	switch rule.state {
	case DayOpenRanges:
		for index, item := range rule.ranges {
			if index > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(formatLocalTime(item.start))
			builder.WriteByte('-')
			builder.WriteString(formatLocalTime(item.end))
		}
	case DayOpenAllDay:
		builder.WriteString("all day")
	case DayClosed:
		builder.WriteString("closed")
	case DayInherited:
		builder.WriteString("inherited")
	}
}

func boundedHumanSummary(summary string) (string, error) {
	if len(summary) > MaxHumanSummaryBytes {
		return "", newError("human summary", CodeLimitExceeded)
	}

	return summary, nil
}
