// Package gomoney adds exact money packaging-cost comparison without
// making monetary dependencies part of the root module.
package gomoney

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/money"
)

var (
	// ErrInvalidCosts identifies empty, invalid, or mixed-currency cost maps.
	ErrInvalidCosts = errors.New("gomoney objective: invalid costs")
	// ErrMissingCost identifies a selected container type without a configured
	// packaging cost.
	ErrMissingCost = errors.New("gomoney objective: missing container cost")
)

// Limits bounds the cost-map collections copied by NewWithLimits.
type Limits struct {
	// MaxTypes bounds configured container type costs.
	MaxTypes uint32
	// MaxIDBytes bounds each container type identifier.
	MaxIDBytes uint32
}

// DefaultLimits returns conservative limits for untrusted cost maps.
func DefaultLimits() Limits { return Limits{MaxTypes: 1_000, MaxIDBytes: 1_024} }

// Costs is an immutable exact packaging-cost objective keyed by container
// type ID.
type Costs struct {
	typeIDs []string
	values  []money.Money
}

// New validates and copies a cost map using DefaultLimits.
func New(values map[string]money.Money) (Costs, error) {
	return NewWithLimits(values, DefaultLimits())
}

// NewWithLimits validates, sorts, and defensively copies a bounded nonempty
// single-currency cost map.
func NewWithLimits(values map[string]money.Money, limits Limits) (Costs, error) {
	if limits.MaxTypes == 0 || limits.MaxIDBytes == 0 || len(values) == 0 || uint64(len(values)) > uint64(limits.MaxTypes) {
		return Costs{}, ErrInvalidCosts
	}
	typeIDs := make([]string, 0, len(values))
	for typeID := range values {
		if strings.TrimSpace(typeID) == "" || uint64(len(typeID)) > uint64(limits.MaxIDBytes) || !values[typeID].Valid() {
			return Costs{}, ErrInvalidCosts
		}
		typeIDs = append(typeIDs, typeID)
	}
	slices.Sort(typeIDs)
	result := Costs{typeIDs: typeIDs, values: make([]money.Money, len(typeIDs))}
	for index, typeID := range typeIDs {
		result.values[index] = values[typeID]
		if index > 0 {
			if _, err := result.values[0].Compare(result.values[index]); err != nil {
				return Costs{}, ErrInvalidCosts
			}
		}
	}
	return result, nil
}

// Valid reports whether the objective contains aligned type IDs and costs.
func (c Costs) Valid() bool { return len(c.typeIDs) > 0 && len(c.typeIDs) == len(c.values) }

// ComparePlans implements objective.PlanObjective with context cancellation.
func (c Costs) ComparePlans(ctx context.Context, _ knapsack.NormalizedRequest, left, right knapsack.Plan) (int, error) {
	if ctx == nil {
		return 0, knapsack.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Compare(left, right)
}

// Components returns the exact total packaging cost and ISO currency unit.
func (c Costs) Components(ctx context.Context, _ knapsack.NormalizedRequest, plan knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	if ctx == nil {
		return nil, knapsack.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	total, err := c.Total(plan)
	if err != nil {
		return nil, err
	}
	return []knapsack.ScoreComponent{{
		Name: "total_packaging_cost", Direction: "min",
		Unit: total.Currency().String(), Value: total.Amount().String(),
	}}, nil
}

// Total sums configured exact costs for every selected container instance.
func (c Costs) Total(plan knapsack.Plan) (money.Money, error) {
	var total money.Money
	for index, container := range plan.Containers() {
		cost, ok := c.cost(container.TypeID)
		if !ok {
			return money.Money{}, ErrMissingCost
		}
		if index == 0 {
			total = cost
			continue
		}
		var err error
		total, err = total.Add(cost)
		if err != nil {
			return money.Money{}, err
		}
	}
	if !total.Valid() {
		return money.Money{}, ErrInvalidCosts
	}
	return total, nil
}

// Compare prefers lower exact cost, then canonical plan bytes for ties.
func (c Costs) Compare(left, right knapsack.Plan) (int, error) {
	leftTotal, err := c.Total(left)
	if err != nil {
		return 0, err
	}
	rightTotal, err := c.Total(right)
	if err != nil {
		return 0, err
	}
	comparison, err := leftTotal.Compare(rightTotal)
	if err != nil || comparison != 0 {
		return comparison, err
	}
	return strings.Compare(left.CanonicalString(), right.CanonicalString()), nil
}
func (c Costs) cost(typeID string) (money.Money, bool) {
	index, found := slices.BinarySearch(c.typeIDs, typeID)
	if !found {
		return money.Money{}, false
	}
	return c.values[index], true
}
