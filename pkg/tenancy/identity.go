// Package tenancy provides explicit tenant identity, propagation, and
// isolation contracts. Tenant identifiers are routing data, never proof of
// authentication, membership, or authorization.
package tenancy

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	// MaxTenantIDBytes is the fixed wire and storage bound for tenant IDs.
	MaxTenantIDBytes = 128
	redactedTenantID = "tenant_[redacted]"
)

// ErrInvalidTenantID reports an empty, oversized, or non-canonical tenant ID.
var ErrInvalidTenantID = errors.New("tenancy: invalid tenant ID")

// TenantID is a validated opaque tenant identifier. Its zero value is invalid.
// IDs use the case-sensitive ASCII alphabet [A-Za-z0-9._:/-], must begin with
// an alphanumeric byte, and are preserved exactly: no case folding, trimming,
// or Unicode normalization occurs.
//
// String deliberately returns a redacted value. Use Value, MarshalText, or
// MarshalJSON only at an explicit trusted propagation or persistence boundary.
type TenantID struct {
	value string
}

// ParseTenantID validates value without changing it.
func ParseTenantID(value string) (TenantID, error) {
	if err := validateTenantID(value); err != nil {
		return TenantID{}, err
	}
	return TenantID{value: value}, nil
}

// MustTenantID is ParseTenantID for static configuration and tests.
func MustTenantID(value string) TenantID {
	id, err := ParseTenantID(value)
	if err != nil {
		panic(err)
	}
	return id
}

// Value returns the canonical raw identifier for explicit trusted boundaries.
func (id TenantID) Value() string { return id.value }

// Equal compares canonical opaque identifiers exactly.
func (id TenantID) Equal(other TenantID) bool { return id == other }

// Valid reports whether id was created from a valid canonical value.
func (id TenantID) Valid() bool { return validateTenantID(id.value) == nil }

// Redacted returns a non-identifying representation safe for diagnostics.
func (id TenantID) Redacted() string { return redactedTenantID }

// String returns a redacted representation to prevent accidental disclosure.
func (id TenantID) String() string { return id.Redacted() }

// GoString returns a redacted representation for Go-syntax diagnostics.
func (id TenantID) GoString() string { return id.Redacted() }

// MarshalText serializes the canonical raw identifier for a trusted boundary.
func (id TenantID) MarshalText() ([]byte, error) {
	if err := validateTenantID(id.value); err != nil {
		return nil, err
	}
	return []byte(id.value), nil
}

// UnmarshalText validates input and clears the receiver on every failure.
func (id *TenantID) UnmarshalText(text []byte) error {
	if id == nil {
		return fmt.Errorf("%w: nil receiver", ErrInvalidTenantID)
	}
	*id = TenantID{}
	parsed, err := ParseTenantID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// MarshalJSON serializes the canonical raw identifier as a JSON string.
func (id TenantID) MarshalJSON() ([]byte, error) {
	if err := validateTenantID(id.value); err != nil {
		return nil, err
	}
	return json.Marshal(id.value)
}

// UnmarshalJSON validates a JSON string and clears the receiver on failure.
func (id *TenantID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return fmt.Errorf("%w: nil receiver", ErrInvalidTenantID)
	}
	*id = TenantID{}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%w: JSON string required", ErrInvalidTenantID)
	}
	parsed, err := ParseTenantID(value)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func validateTenantID(value string) error {
	if len(value) == 0 || len(value) > MaxTenantIDBytes {
		return fmt.Errorf("%w: length", ErrInvalidTenantID)
	}
	if !asciiAlphaNumeric(value[0]) {
		return fmt.Errorf("%w: alphabet", ErrInvalidTenantID)
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if !asciiAlphaNumeric(char) && char != '-' && char != '_' &&
			char != '.' && char != ':' && char != '/' {
			return fmt.Errorf("%w: alphabet", ErrInvalidTenantID)
		}
	}
	return nil
}

func asciiAlphaNumeric(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9'
}
