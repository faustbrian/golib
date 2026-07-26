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

// Period encodes an immutable period with exact endpoints and bounds.
func Period(value instant.Period) ruleengine.Value {
	encoded := periodPrefix + value.Start().UTC().Format(time.RFC3339Nano) + "|" +
		value.End().UTC().Format(time.RFC3339Nano) + "|" + value.Bounds().String()
	return ruleengine.String(encoded)
}

// Instant encodes an instant for period membership operators.
func Instant(value time.Time) ruleengine.Value {
	return ruleengine.String(instantPrefix + value.UTC().Format(time.RFC3339Nano))
}

// Operators returns a fresh complete temporal operator set.
func Operators() []ruleengine.Operator {
	return []ruleengine.Operator{
		periodRelationOperator{name: OpPeriodEqual, relations: []temporal.Relation{temporal.Equal}},
		periodRelationOperator{name: OpPeriodBefore, relations: []temporal.Relation{temporal.Before}},
		periodRelationOperator{name: OpPeriodAfter, relations: []temporal.Relation{temporal.After}},
		periodRelationOperator{name: OpPeriodOverlaps, relations: []temporal.Relation{temporal.Overlaps, temporal.OverlappedBy}},
		periodRelationOperator{name: OpPeriodContainsPeriod, relations: []temporal.Relation{temporal.Contains, temporal.Equal}},
		periodContainsOperator{},
	}
}

type periodRelationOperator struct {
	name      ruleengine.OperatorName
	relations []temporal.Relation
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
	relation, err := leftPeriod.RelationTo(rightPeriod)
	if err != nil {
		return false, err
	}
	for _, accepted := range operator.relations {
		if relation == accepted {
			return true, nil
		}
	}
	return false, nil
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
	if !ok || !strings.HasPrefix(text, periodPrefix) {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	parts := strings.Split(strings.TrimPrefix(text, periodPrefix), "|")
	if len(parts) != 3 {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period")
	}
	start, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return instant.Period{}, fmt.Errorf("rule-engine temporal: invalid period start: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, parts[1])
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
	if !ok || !strings.HasPrefix(text, instantPrefix) {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant")
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(text, instantPrefix))
	if err != nil {
		return time.Time{}, fmt.Errorf("rule-engine temporal: invalid instant: %w", err)
	}
	return parsed, nil
}
