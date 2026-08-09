// Package gomoney adds exact money packaging-cost comparison without
// making monetary dependencies part of the root module.
package gomoney

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/money"
)

var (
	// ErrInvalidCosts identifies an empty, invalid, or incompatible cost mapping.
	ErrInvalidCosts = errors.New("gomoney objective: invalid costs")
	// ErrMissingCost identifies a selected container type without a configured
	// packaging cost.
	ErrMissingCost = errors.New("gomoney objective: missing container cost")
	// ErrDuplicateTypeID identifies repeated container type IDs supplied through
	// NewFromEntries.
	ErrDuplicateTypeID = errors.New("gomoney objective: duplicate container type ID")
	// ErrUnsupportedScale identifies a money context whose scale is not fixed.
	ErrUnsupportedScale = errors.New("gomoney objective: unsupported money scale")
	// ErrNegativeCost identifies a negative cost rejected by the active policy.
	ErrNegativeCost = errors.New("gomoney objective: negative container cost")
)

// Limits bounds cost collections copied by the constructors.
type Limits struct {
	// MaxTypes bounds configured container type costs.
	MaxTypes uint32
	// MaxIDBytes bounds each container type identifier.
	MaxIDBytes uint32
}

// DefaultLimits returns conservative limits for untrusted cost maps.
func DefaultLimits() Limits { return Limits{MaxTypes: 1_000, MaxIDBytes: 1_024} }

// Policy controls bounded construction and whether negative values explicitly
// model credits or rebates. DefaultPolicy rejects negative costs.
type Policy struct {
	// Limits bounds the copied mapping and its type IDs.
	Limits Limits
	// AllowNegativeCosts permits values that explicitly model credits or rebates.
	AllowNegativeCosts bool
}

// DefaultPolicy returns the default resource limits and rejects negative costs.
func DefaultPolicy() Policy { return Policy{Limits: DefaultLimits()} }

// Entry is one container type ID and exact packaging cost. Entry construction
// is useful when duplicate IDs must be detected before forming a Go map.
type Entry struct {
	// TypeID is the exact Knapsack container type identifier.
	TypeID string
	// Cost is the exact cost for each selected instance of TypeID.
	Cost money.Money
}

// Costs is an immutable exact packaging-cost objective keyed by container
// type ID.
type Costs struct {
	typeIDs []string
	values  []money.Money
	zero    money.Money
}

// New validates and copies a cost map using DefaultPolicy.
func New(values map[string]money.Money) (Costs, error) {
	return NewWithPolicy(values, DefaultPolicy())
}

// NewWithLimits validates, sorts, and defensively copies a bounded nonempty
// single-currency cost map.
func NewWithLimits(values map[string]money.Money, limits Limits) (Costs, error) {
	policy := DefaultPolicy()
	policy.Limits = limits
	return NewWithPolicy(values, policy)
}

// NewWithPolicy validates and copies a cost map using an explicit negative-cost
// and resource policy. A Go map has already collapsed duplicate keys; callers
// that need duplicate detection must use NewFromEntries.
func NewWithPolicy(values map[string]money.Money, policy Policy) (Costs, error) {
	if err := validateCount(uint64(len(values)), policy.Limits); err != nil {
		return Costs{}, err
	}
	entries := make([]Entry, 0, len(values))
	for typeID, cost := range values {
		entries = append(entries, Entry{TypeID: typeID, Cost: cost})
	}
	return newFromEntries(entries, policy)
}

// NewFromEntries validates, sorts, and defensively copies a bounded nonempty
// entry sequence. It rejects duplicate type IDs and requires one currency and
// one fixed Default or Custom money context across all values.
func NewFromEntries(entries []Entry, policy Policy) (Costs, error) {
	if err := validateCount(uint64(len(entries)), policy.Limits); err != nil {
		return Costs{}, err
	}
	return newFromEntries(entries, policy)
}

func newFromEntries(entries []Entry, policy Policy) (Costs, error) {
	limits := policy.Limits
	owned := slices.Clone(entries)
	slices.SortFunc(owned, func(left, right Entry) int {
		return strings.Compare(left.TypeID, right.TypeID)
	})
	for index, entry := range owned {
		if strings.TrimSpace(entry.TypeID) == "" || uint64(len(entry.TypeID)) > uint64(limits.MaxIDBytes) {
			return Costs{}, ErrInvalidCosts
		}
		if index > 0 && entry.TypeID == owned[index-1].TypeID {
			return Costs{}, invalidCosts(ErrDuplicateTypeID)
		}
	}
	for _, entry := range owned {
		if !entry.Cost.Valid() {
			return Costs{}, invalidCosts(money.ErrInvalidMoney)
		}
		if entry.Cost.Context().Kind() != money.ContextDefault &&
			entry.Cost.Context().Kind() != money.ContextCustom {
			return Costs{}, invalidCosts(ErrUnsupportedScale)
		}
		if entry.Cost.Sign() < 0 && !policy.AllowNegativeCosts {
			return Costs{}, invalidCosts(ErrNegativeCost)
		}
	}

	first := owned[0].Cost
	result := Costs{
		typeIDs: make([]string, len(owned)),
		values:  make([]money.Money, len(owned)),
	}
	for index, entry := range owned {
		if entry.Cost.Currency() != first.Currency() {
			return Costs{}, invalidCosts(money.ErrCurrencyMismatch)
		}
		if entry.Cost.Context() != first.Context() {
			return Costs{}, invalidCosts(money.ErrContextMismatch)
		}
		result.typeIDs[index] = entry.TypeID
		result.values[index] = entry.Cost
	}
	// Identical immutable values are necessarily compatible, and subtracting a
	// value from itself cannot exceed the amount bounds.
	result.zero, _ = first.Sub(first)
	return result, nil
}

// Valid reports whether the objective contains aligned type IDs and costs.
func (c Costs) Valid() bool {
	return c.zero.Valid()
}

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

// Total sums configured exact costs for every selected container instance. It
// applies the Money amount bound only to the final order-independent total.
func (c Costs) Total(plan knapsack.Plan) (money.Money, error) {
	if !c.Valid() {
		return money.Money{}, ErrInvalidCosts
	}
	coefficient := new(big.Int)
	for _, container := range plan.Containers() {
		cost, ok := c.cost(container.TypeID)
		if !ok {
			return money.Money{}, ErrMissingCost
		}
		coefficient.Add(coefficient, cost.Amount().Decimal().Coefficient())
	}
	// Accumulating before applying Money's final amount bound avoids
	// order-sensitive intermediate overflow when an explicit negative-cost
	// policy is active.
	aggregationLimits := gomath.DefaultLimits()
	aggregationLimits.MaxInputDigits = money.MaxAmountDigits
	aggregationLimits.MaxOutputDigits = money.MaxAmountDigits
	exact, err := decimal.FromBig(
		coefficient,
		-int32(c.zero.Context().Scale()),
		aggregationLimits,
	)
	if err != nil {
		return money.Money{}, err
	}
	return money.Parse(exact.String(), c.zero.Currency(), c.zero.Context())
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

func invalidCosts(cause error) error {
	return fmt.Errorf("%w: %w", ErrInvalidCosts, cause)
}

func validateCount(count uint64, limits Limits) error {
	if limits.MaxTypes == 0 {
		return ErrInvalidCosts
	}
	if limits.MaxIDBytes == 0 {
		return ErrInvalidCosts
	}
	if count == 0 {
		return ErrInvalidCosts
	}
	if count > uint64(limits.MaxTypes) {
		return ErrInvalidCosts
	}
	return nil
}
