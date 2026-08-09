// Package ruleenginetemporal adapts temporal periods without adding a
// temporal dependency to the core rule-engine module.
package ruleenginetemporal

import (
	"context"
	"fmt"
	"strings"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

const (
	periodPrefix  = "period:"
	instantPrefix = "instant:"

	maxTimestampBytes = len("9999-12-31T23:59:59.999999999+23:59")
	maxPeriodBytes    = len(periodPrefix) + 2*maxTimestampBytes + len("||[]")
	maxInstantBytes   = len(instantPrefix) + maxTimestampBytes
)

// Period operator names identify interval relations and instant membership
// checks over tagged temporal values.
const (
	OpPeriodEqual          ruleengine.OperatorName = "period_equal"
	OpPeriodBefore         ruleengine.OperatorName = "period_before"
	OpPeriodAfter          ruleengine.OperatorName = "period_after"
	OpPeriodOverlaps       ruleengine.OperatorName = "period_overlaps"
	OpPeriodContainsPeriod ruleengine.OperatorName = "period_contains_period"
	OpPeriodContains       ruleengine.OperatorName = "period_contains_instant"
)

// Period encodes an immutable period with exact UTC endpoints and bounds.
// It rejects endpoints outside RFC 3339's four-digit year range.
func Period(value instant.Period) (ruleengine.Value, error) {
	start, err := encodeInstant(value.Start())
	if err != nil {
		return ruleengine.Value{}, fmt.Errorf("rule-engine temporal: encode period start: %w", err)
	}
	end, err := encodeInstant(value.End())
	if err != nil {
		return ruleengine.Value{}, fmt.Errorf("rule-engine temporal: encode period end: %w", err)
	}

	return ruleengine.String(periodPrefix + start + "|" + end + "|" + value.Bounds().String()), nil
}

// Instant encodes an instant as canonical UTC RFC 3339 with nanosecond
// precision. It rejects values outside RFC 3339's four-digit year range.
func Instant(value time.Time) (ruleengine.Value, error) {
	encoded, err := encodeInstant(value)
	if err != nil {
		return ruleengine.Value{}, fmt.Errorf("rule-engine temporal: encode instant: %w", err)
	}

	return ruleengine.String(instantPrefix + encoded), nil
}

// Operators returns a fresh complete temporal operator set.
func Operators() []ruleengine.Operator {
	return []ruleengine.Operator{
		periodRelationOperator{name: OpPeriodEqual, match: instant.Period.SetEqual},
		periodRelationOperator{name: OpPeriodBefore, match: instant.Period.IsBefore},
		periodRelationOperator{name: OpPeriodAfter, match: instant.Period.IsAfter},
		periodRelationOperator{name: OpPeriodOverlaps, match: instant.Period.Overlaps},
		periodRelationOperator{name: OpPeriodContainsPeriod, match: instant.Period.Contains},
		periodContainsOperator{},
	}
}

type periodRelationOperator struct {
	name  ruleengine.OperatorName
	match func(instant.Period, instant.Period) bool
}

func (operator periodRelationOperator) Name() ruleengine.OperatorName { return operator.name }
func (periodRelationOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (operator periodRelationOperator) Evaluate(ctx context.Context, left, right ruleengine.Value) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	leftPeriod, err := parsePeriod(left)
	if err != nil {
		return false, err
	}
	rightPeriod, err := parsePeriod(right)
	if err != nil {
		return false, err
	}
	return operator.match(leftPeriod, rightPeriod), nil
}

type periodContainsOperator struct{}

func (periodContainsOperator) Name() ruleengine.OperatorName { return OpPeriodContains }
func (periodContainsOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (periodContainsOperator) Evaluate(ctx context.Context, left, right ruleengine.Value) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	period, err := parsePeriod(left)
	if err != nil {
		return false, err
	}
	point, err := parseInstant(right)
	if err != nil {
		return false, err
	}
	return period.Includes(point), nil
}

func parsePeriod(value ruleengine.Value) (instant.Period, error) {
	text, ok := value.StringValue()
	if !ok {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	if len(text) > maxPeriodBytes {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	payload, ok := strings.CutPrefix(text, periodPrefix)
	if !ok {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 3 {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	start, err := parseTimestamp(parts[0])
	if err != nil {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period start: %w", err)
	}
	end, err := parseTimestamp(parts[1])
	if err != nil {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period end: %w", err)
	}
	var bounds temporal.Bounds
	if err := bounds.UnmarshalText([]byte(parts[2])); err != nil {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period bounds: %w", err)
	}
	return instant.New(start, end, bounds)
}

func parseInstant(value ruleengine.Value) (time.Time, error) {
	text, ok := value.StringValue()
	if !ok {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant")
	}
	if len(text) > maxInstantBytes {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant")
	}
	payload, ok := strings.CutPrefix(text, instantPrefix)
	if !ok {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant")
	}
	parsed, err := parseTimestamp(payload)
	if err != nil {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant: %w", err)
	}
	return parsed, nil
}

func parseTimestamp(text string) (time.Time, error) {
	if !validTimestampShape(text) {
		return time.Time{}, fmt.Errorf("invalid RFC3339 nanosecond timestamp")
	}
	if !validTimestampOffset(text) {
		return time.Time{}, fmt.Errorf("invalid RFC3339 numeric offset")
	}

	return time.Parse(time.RFC3339Nano, text)
}

func validTimestampShape(text string) bool {
	if len(text) < len("0000-00-00T00:00:00Z") {
		return false
	}
	if len(text) > maxTimestampBytes {
		return false
	}

	switch text[19] {
	case ',':
		return false
	case '.':
	default:
		return true
	}
	zoneOffset := strings.IndexAny(text[20:], "Z+-")
	return zoneOffset >= 1 && zoneOffset <= 9
}

func validTimestampOffset(text string) bool {
	if text[len(text)-1] == 'Z' {
		return true
	}
	hour := text[len(text)-5 : len(text)-3]
	if hour > "23" {
		return false
	}
	minute := text[len(text)-2:]
	return minute <= "59"
}

func encodeInstant(value time.Time) (string, error) {
	encoded, err := value.UTC().AppendText(nil)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}
