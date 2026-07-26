// Package discriminator selects Schema Object mapping hints without changing
// JSON Schema validation outcomes.
package discriminator

import (
	"errors"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidDiscriminator reports malformed discriminator or instance data.
	ErrInvalidDiscriminator = errors.New("invalid discriminator selection input")
	// ErrLimitExceeded reports that the mapping count exceeded caller policy.
	ErrLimitExceeded = errors.New("discriminator mapping limit exceeded")
)

// Limits bounds caller-controlled discriminator mappings.
type Limits struct {
	MaxMappings int
}

// DefaultLimits returns conservative standalone selection limits.
func DefaultLimits() Limits {
	return Limits{MaxMappings: 10_000}
}

// TargetKind distinguishes component schema names from URI references.
type TargetKind string

const (
	// TargetSchemaName identifies a component schema name.
	TargetSchemaName TargetKind = "schema-name"
	// TargetURIReference identifies an explicitly URI-shaped mapping target.
	TargetURIReference TargetKind = "uri-reference"
)

// MatchKind identifies how a discriminator target was selected.
type MatchKind string

const (
	// MatchExplicit identifies a key found in mapping.
	MatchExplicit MatchKind = "explicit"
	// MatchImplicit identifies the discriminating value as a schema name.
	MatchImplicit MatchKind = "implicit"
	// MatchDefault identifies defaultMapping after an absent property.
	MatchDefault MatchKind = "default"
)

// Selection is one immutable discriminator hint.
type Selection struct {
	PropertyName string
	Value        string
	Target       string
	TargetKind   TargetKind
	MatchKind    MatchKind
}

// Select derives a schema-selection hint from one Discriminator Object and
// payload. It does not compile schemas, resolve references, or affect schema
// validation.
func Select(
	discriminator jsonvalue.Value,
	instance jsonvalue.Value,
	limits Limits,
) (Selection, bool, error) {
	if discriminator.Kind() != jsonvalue.ObjectKind ||
		instance.Kind() != jsonvalue.ObjectKind || limits.MaxMappings < 1 {
		return Selection{}, false, ErrInvalidDiscriminator
	}
	property, exists := discriminator.Lookup("propertyName")
	if !exists {
		return Selection{}, false, ErrInvalidDiscriminator
	}
	propertyName, valid := property.Text()
	if !valid || propertyName == "" {
		return Selection{}, false, ErrInvalidDiscriminator
	}
	mapping, hasMapping := discriminator.Lookup("mapping")
	if hasMapping {
		members, valid := mapping.Members()
		if !valid {
			return Selection{}, false, ErrInvalidDiscriminator
		}
		if len(members) > limits.MaxMappings {
			return Selection{}, false, ErrLimitExceeded
		}
	}
	value, present := instance.Lookup(propertyName)
	if !present {
		fallback, exists := discriminator.Lookup("defaultMapping")
		if !exists {
			return Selection{}, false, nil
		}
		target, valid := fallback.Text()
		if !valid || target == "" {
			return Selection{}, false, ErrInvalidDiscriminator
		}
		return selection(propertyName, "", target, MatchDefault), true, nil
	}
	actual, valid := value.Text()
	if !valid {
		return Selection{}, false, ErrInvalidDiscriminator
	}
	if hasMapping {
		targetValue, exists := mapping.Lookup(actual)
		if exists {
			target, valid := targetValue.Text()
			if !valid || target == "" {
				return Selection{}, false, ErrInvalidDiscriminator
			}
			return selection(
				propertyName, actual, target, MatchExplicit,
			), true, nil
		}
	}
	return selection(
		propertyName, actual, actual, MatchImplicit,
	), true, nil
}

func selection(
	propertyName string,
	value string,
	target string,
	matchKind MatchKind,
) Selection {
	return Selection{
		PropertyName: propertyName,
		Value:        value,
		Target:       target,
		TargetKind:   targetKind(target),
		MatchKind:    matchKind,
	}
}

func targetKind(target string) TargetKind {
	if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") ||
		strings.HasPrefix(target, "/") || strings.HasPrefix(target, "#") ||
		strings.ContainsAny(target, "/?#:") {
		return TargetURIReference
	}
	return TargetSchemaName
}
