// Package international defines shared contracts for versioned international
// identifier datasets. Identifier-specific behavior lives in subpackages.
package international

import "encoding/json"

// Status describes the registry standing of an identifier. It is metadata,
// not a claim that an identifier is suitable for a particular business use.
type Status uint8

const (
	// StatusUnknown means no authoritative status is available.
	StatusUnknown Status = iota
	// StatusOfficial identifies a currently assigned standards entry.
	StatusOfficial
	// StatusReserved identifies an entry retained by its authority.
	StatusReserved
	// StatusTransitional identifies an entry in a defined transition period.
	StatusTransitional
	// StatusDeleted identifies an entry removed from current assignment.
	StatusDeleted
	// StatusUserAssigned identifies a standards-defined private-use entry.
	StatusUserAssigned
	// StatusHistoric identifies an entry available only through opt-in history.
	StatusHistoric
)

// String returns the stable wire spelling of the status.
func (status Status) String() string {
	switch status {
	case StatusOfficial:
		return "official"
	case StatusReserved:
		return "reserved"
	case StatusTransitional:
		return "transitional"
	case StatusDeleted:
		return "deleted"
	case StatusUserAssigned:
		return "user-assigned"
	case StatusHistoric:
		return "historic"
	case StatusUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Known reports whether the status came from authoritative metadata.
func (status Status) Known() bool {
	return status >= StatusOfficial && status <= StatusHistoric
}

// ParseStatus parses one stable wire spelling without aliases or casing repair.
func ParseStatus(input string) (Status, error) {
	switch input {
	case "unknown":
		return StatusUnknown, nil
	case "official":
		return StatusOfficial, nil
	case "reserved":
		return StatusReserved, nil
	case "transitional":
		return StatusTransitional, nil
	case "deleted":
		return StatusDeleted, nil
	case "user-assigned":
		return StatusUserAssigned, nil
	case "historic":
		return StatusHistoric, nil
	default:
		return StatusUnknown, NewParseError("status", "unknown wire spelling")
	}
}

// MarshalText returns the stable status wire spelling.
func (status Status) MarshalText() ([]byte, error) {
	if status > StatusHistoric {
		return nil, NewParseError("status", "unknown enum value")
	}
	return []byte(status.String()), nil
}

// UnmarshalText parses one stable status wire spelling without changing the
// receiver on error.
func (status *Status) UnmarshalText(input []byte) error {
	parsed, err := ParseStatus(string(input))
	if err == nil {
		*status = parsed
	}
	return err
}

// MarshalJSON encodes the status as its stable string spelling.
func (status Status) MarshalJSON() ([]byte, error) {
	text, err := status.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

// UnmarshalJSON accepts only a stable status string spelling.
func (status *Status) UnmarshalJSON(input []byte) error {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return NewParseError("status", "expected JSON string")
	}
	return status.UnmarshalText([]byte(text))
}
