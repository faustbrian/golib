// Package encoding provides strict, bounded, versioned canonical JSON for
// normalized packing artifacts.
package encoding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

var (
	// ErrInvalidEncoding identifies malformed or non-canonical JSON input.
	ErrInvalidEncoding = errors.New("knapsack encoding: invalid JSON")
	// ErrDuplicateKey identifies an object containing the same key twice.
	ErrDuplicateKey = errors.New("knapsack encoding: duplicate object key")
	// ErrEncodingLimit identifies input rejected by a decoding resource bound.
	ErrEncodingLimit = errors.New("knapsack encoding: resource limit exceeded")
	// ErrUnsupportedVersion identifies an unknown request or plan schema.
	ErrUnsupportedVersion = errors.New("knapsack encoding: unsupported version")
)

// Version is the canonical request and plan schema emitted by this package.
const Version = "v1"

// Limits bounds JSON size, nesting depth, and collection cardinality before
// decoded values reach the domain constructors.
type Limits struct {
	// MaxBytes is the maximum UTF-8 JSON byte length.
	MaxBytes int64
	// MaxDepth is the maximum nested object or array depth.
	MaxDepth int
	// MaxCollection is the maximum members in any one object or array.
	MaxCollection int
}

// DefaultLimits returns conservative bounds for untrusted JSON input.
func DefaultLimits() Limits { return Limits{MaxBytes: 16 << 20, MaxDepth: 64, MaxCollection: 100_000} }

type planEnvelope struct {
	Version string            `json:"version"`
	Plan    knapsack.PlanSpec `json:"plan"`
}

type quantityWire struct {
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
}
type resolutionWire struct {
	Length quantityWire `json:"length"`
	Mass   quantityWire `json:"mass"`
}
type itemWire struct {
	ID                 string                 `json:"id"`
	SKU                string                 `json:"sku,omitempty"`
	Dimensions         geometry.Dimensions    `json:"dimensions"`
	Weight             int64                  `json:"weight"`
	Orientations       []geometry.Orientation `json:"orientations"`
	Attributes         map[string]string      `json:"attributes,omitempty"`
	FragileTop         bool                   `json:"fragile_top,omitempty"`
	MaxSupportedWeight *int64                 `json:"max_supported_weight,omitempty"`
	MinimumSupportPPM  uint32                 `json:"minimum_support_ppm,omitempty"`
	Group              string                 `json:"group,omitempty"`
	IncompatibleGroups []string               `json:"incompatible_groups,omitempty"`
	MaxStackCount      uint32                 `json:"max_stack_count,omitempty"`
	Priority           int64                  `json:"priority,omitempty"`
}
type cuboidWire struct {
	Origin     geometry.Point      `json:"origin"`
	Dimensions geometry.Dimensions `json:"dimensions"`
}
type stockWire struct {
	Unlimited bool   `json:"unlimited"`
	Count     uint32 `json:"count,omitempty"`
}
type containerWire struct {
	ID               string                          `json:"id"`
	Dimensions       geometry.Dimensions             `json:"dimensions"`
	MaxContentWeight int64                           `json:"max_content_weight"`
	TareWeight       int64                           `json:"tare_weight,omitempty"`
	MaxGrossWeight   int64                           `json:"max_gross_weight,omitempty"`
	HasGrossWeight   bool                            `json:"has_gross_weight,omitempty"`
	Stock            stockWire                       `json:"stock"`
	Priority         int64                           `json:"priority,omitempty"`
	MaxItemCount     uint32                          `json:"max_item_count,omitempty"`
	AllowedClasses   []string                        `json:"allowed_classes,omitempty"`
	Reserved         []cuboidWire                    `json:"reserved,omitempty"`
	CenterOfGravity  *knapsack.CenterOfGravityBounds `json:"center_of_gravity,omitempty"`
}
type requestEnvelope struct {
	Version    string          `json:"version"`
	Resolution resolutionWire  `json:"resolution"`
	Limits     knapsack.Limits `json:"limits"`
	Items      []itemWire      `json:"items"`
	Containers []containerWire `json:"containers"`
}

// MarshalPlan returns deterministic versioned JSON for an immutable plan.
func MarshalPlan(plan knapsack.Plan) ([]byte, error) {
	return json.Marshal(planEnvelope{Version: Version, Plan: plan.Spec()})
}

// MarshalRequest returns deterministic versioned JSON preserving exact lattice
// quantities and normalized request limits.
func MarshalRequest(request knapsack.NormalizedRequest) ([]byte, error) {
	resolution := request.Resolution()
	envelope := requestEnvelope{Version: Version, Resolution: resolutionWire{Length: wireQuantity(resolution.Length), Mass: wireQuantity(resolution.Mass)}, Limits: request.Limits()}
	for _, item := range request.Items() {
		envelope.Items = append(envelope.Items, itemWire{ID: item.ID, SKU: item.SKU, Dimensions: item.Dimensions, Weight: item.Weight, Orientations: item.Orientations, Attributes: item.Attributes, FragileTop: item.FragileTop, MaxSupportedWeight: item.MaxSupportedWeight, MinimumSupportPPM: item.MinimumSupportPPM, Group: item.Group, IncompatibleGroups: item.IncompatibleGroups, MaxStackCount: item.MaxStackCount, Priority: item.Priority})
	}
	for _, container := range request.Containers() {
		wire := containerWire{ID: container.ID, Dimensions: container.Dimensions, MaxContentWeight: container.MaxContentWeight, TareWeight: container.TareWeight, MaxGrossWeight: container.MaxGrossWeight, HasGrossWeight: container.HasGrossWeight, Stock: stockWire{Unlimited: container.Stock.Unlimited(), Count: container.Stock.Count()}, Priority: container.Priority, MaxItemCount: container.MaxItemCount, AllowedClasses: container.AllowedClasses, CenterOfGravity: container.CenterOfGravity}
		for _, reserved := range container.Reserved {
			wire.Reserved = append(wire.Reserved, cuboidWire{Origin: reserved.Origin(), Dimensions: reserved.Dimensions()})
		}
		envelope.Containers = append(envelope.Containers, wire)
	}
	return json.Marshal(envelope)
}

// UnmarshalRequest strictly decodes and validates a versioned normalized
// request. It rejects unknown fields, duplicate keys, trailing data, invalid
// UTF-8, unsupported versions, and resource-limit violations.
func UnmarshalRequest(input []byte, limits Limits) (knapsack.NormalizedRequest, error) {
	if err := limits.validate(input); err != nil {
		return knapsack.NormalizedRequest{}, err
	}
	if err := validateStrict(input, limits); err != nil {
		return knapsack.NormalizedRequest{}, err
	}
	var envelope requestEnvelope
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return knapsack.NormalizedRequest{}, fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	if envelope.Version != Version {
		return knapsack.NormalizedRequest{}, fmt.Errorf("%w: %q", ErrUnsupportedVersion, envelope.Version)
	}
	length, err := parseQuantity(envelope.Resolution.Length)
	if err != nil {
		return knapsack.NormalizedRequest{}, err
	}
	mass, err := parseQuantity(envelope.Resolution.Mass)
	if err != nil {
		return knapsack.NormalizedRequest{}, err
	}
	spec := knapsack.NormalizedSpec{Resolution: knapsack.Resolution{Length: length, Mass: mass}, Limits: envelope.Limits}
	for _, item := range envelope.Items {
		spec.Items = append(spec.Items, knapsack.NormalizedItem{ID: item.ID, SKU: item.SKU, Dimensions: item.Dimensions, Weight: item.Weight, Orientations: item.Orientations, Attributes: item.Attributes, FragileTop: item.FragileTop, MaxSupportedWeight: item.MaxSupportedWeight, MinimumSupportPPM: item.MinimumSupportPPM, Group: item.Group, IncompatibleGroups: item.IncompatibleGroups, MaxStackCount: item.MaxStackCount, Priority: item.Priority})
	}
	for _, container := range envelope.Containers {
		stock := knapsack.FiniteStock(container.Stock.Count)
		if container.Stock.Unlimited {
			stock = knapsack.UnlimitedStock()
		}
		normalized := knapsack.NormalizedContainer{ID: container.ID, Dimensions: container.Dimensions, MaxContentWeight: container.MaxContentWeight, TareWeight: container.TareWeight, MaxGrossWeight: container.MaxGrossWeight, HasGrossWeight: container.HasGrossWeight, Stock: stock, Priority: container.Priority, MaxItemCount: container.MaxItemCount, AllowedClasses: container.AllowedClasses, CenterOfGravity: container.CenterOfGravity}
		for _, reserved := range container.Reserved {
			cuboid, cuboidErr := geometry.NewCuboid(reserved.Origin, reserved.Dimensions)
			if cuboidErr != nil {
				return knapsack.NormalizedRequest{}, fmt.Errorf("%w: %w", ErrInvalidEncoding, cuboidErr)
			}
			normalized.Reserved = append(normalized.Reserved, cuboid)
		}
		spec.Containers = append(spec.Containers, normalized)
	}
	request, err := knapsack.NewNormalizedRequest(spec)
	if err != nil {
		return knapsack.NormalizedRequest{}, fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	return request, nil
}

func wireQuantity(quantity measurement.Quantity) quantityWire {
	return quantityWire{Amount: quantity.Amount().String(), Unit: string(quantity.Unit())}
}
func parseQuantity(wire quantityWire) (measurement.Quantity, error) {
	amount, err := decimal.Parse(wire.Amount)
	if err != nil {
		return measurement.Quantity{}, fmt.Errorf("%w: quantity amount", ErrInvalidEncoding)
	}
	quantity, err := measurement.New(amount, measurement.Unit(wire.Unit))
	if err != nil {
		return measurement.Quantity{}, fmt.Errorf("%w: quantity unit", ErrInvalidEncoding)
	}
	return quantity, nil
}

// UnmarshalPlan strictly decodes and validates a versioned immutable plan.
func UnmarshalPlan(input []byte, limits Limits) (knapsack.Plan, error) {
	if err := limits.validate(input); err != nil {
		return knapsack.Plan{}, err
	}
	if err := validateStrict(input, limits); err != nil {
		return knapsack.Plan{}, err
	}
	var envelope planEnvelope
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return knapsack.Plan{}, fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	if envelope.Version != Version {
		return knapsack.Plan{}, fmt.Errorf("%w: %q", ErrUnsupportedVersion, envelope.Version)
	}
	plan, err := knapsack.NewPlan(envelope.Plan)
	if err != nil {
		return knapsack.Plan{}, fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	return plan, nil
}

func (l Limits) validate(input []byte) error {
	if l.MaxBytes <= 0 || l.MaxDepth <= 0 || l.MaxCollection <= 0 {
		return ErrEncodingLimit
	}
	if int64(len(input)) > l.MaxBytes {
		return ErrEncodingLimit
	}
	return nil
}

func validateStrict(input []byte, limits Limits) error {
	if !utf8.Valid(input) {
		return ErrInvalidEncoding
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	if err := validateValue(decoder, first, limits, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return ErrInvalidEncoding
		}
		return fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}
	return nil
}

func validateValue(decoder *json.Decoder, token json.Token, limits Limits, depth int) error {
	if depth > limits.MaxDepth {
		return ErrEncodingLimit
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		count := 0
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
			}
			// encoding/json guarantees object member tokens are strings.
			key, _ := keyToken.(string)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: %s", ErrDuplicateKey, key)
			}
			seen[key] = struct{}{}
			count++
			if count > limits.MaxCollection {
				return ErrEncodingLimit
			}
			value, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
			}
			if err := validateValue(decoder, value, limits, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return ErrInvalidEncoding
		}
	case '[':
		count := 0
		for decoder.More() {
			count++
			if count > limits.MaxCollection {
				return ErrEncodingLimit
			}
			value, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
			}
			if err := validateValue(decoder, value, limits, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return ErrInvalidEncoding
		}
	}
	return nil
}
