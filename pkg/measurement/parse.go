package measurement

import (
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/math/decimal"
)

const (
	// MaxTextBytes bounds canonical and profile-based text parsing.
	MaxTextBytes = 4096
	// MaxAliasBytes bounds unit tokens resolved through a profile.
	MaxAliasBytes = 128
	// MaxProfileAliases bounds caller-defined unit-token catalogs.
	MaxProfileAliases = 1024
)

// Profile is an immutable explicit mapping of accepted unit tokens.
type Profile struct {
	aliases map[string]Unit
}

// NewProfile validates and defensively copies an explicit alias-to-unit policy.
func NewProfile(aliases map[string]Unit) (Profile, error) {
	if len(aliases) > MaxProfileAliases {
		return Profile{}, fmt.Errorf("%w: profile exceeds %d aliases", ErrInvalidQuantity, MaxProfileAliases)
	}
	copyOfAliases := make(map[string]Unit, len(aliases))
	for alias, unit := range aliases {
		if len(alias) == 0 || len(alias) > MaxAliasBytes {
			return Profile{}, fmt.Errorf("%w: unit alias length", ErrUnknownUnit)
		}
		if _, err := definitionFor(unit); err != nil {
			return Profile{}, err
		}
		copyOfAliases[alias] = unit
	}

	return Profile{aliases: copyOfAliases}, nil
}

// SymbolProfile accepts only canonical unit symbols.
func SymbolProfile() Profile {
	aliases := make(map[string]Unit, len(unitDefinitions))
	for unit := range unitDefinitions {
		aliases[string(unit)] = unit
	}

	return Profile{aliases: aliases}
}

// Resolve resolves exactly one alias without case or locale inference.
func (p Profile) Resolve(alias string) (Unit, error) {
	if len(alias) == 0 || len(alias) > MaxAliasBytes {
		return "", fmt.Errorf("%w: unit alias length", ErrUnknownUnit)
	}
	unit, ok := p.aliases[alias]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownUnit, alias)
	}
	if _, err := definitionFor(unit); err != nil {
		return "", err
	}

	return unit, nil
}

// Parse parses strict amount-space-unit text under profile.
func Parse(input string, profile Profile) (Quantity, error) {
	if len(input) > MaxTextBytes {
		return Quantity{}, fmt.Errorf("%w: text exceeds %d bytes", ErrInvalidQuantity, MaxTextBytes)
	}
	if strings.TrimSpace(input) != input {
		return Quantity{}, ErrInvalidQuantity
	}
	separator := strings.LastIndexByte(input, ' ')
	if separator <= 0 || separator == len(input)-1 {
		return Quantity{}, ErrInvalidQuantity
	}
	amount, err := decimal.Parse(input[:separator])
	if err != nil {
		return Quantity{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	unit, err := profile.Resolve(input[separator+1:])
	if err != nil {
		return Quantity{}, err
	}

	return New(amount, unit)
}
