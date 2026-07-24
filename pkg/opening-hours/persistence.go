package openinghours

import "database/sql/driver"

// Value implements driver.Valuer using canonical JSON suitable for JSONB.
func (s Schedule) Value() (driver.Value, error) {
	encoded, err := s.CanonicalJSON()
	if err != nil {
		return nil, err
	}

	return encoded, nil
}

// Scan implements sql.Scanner for JSON/JSONB bytes, strings, and NULL. NULL
// produces the fail-closed zero schedule.
func (s *Schedule) Scan(source any) error {
	if s == nil {
		return newError("scan", CodeInvalidState)
	}
	if source == nil {
		*s = Schedule{}
		return nil
	}
	var data []byte
	switch value := source.(type) {
	case []byte:
		data = append([]byte(nil), value...)
	case string:
		data = []byte(value)
	default:
		return newError("scan", CodeInvalidEncoding)
	}
	parsed, err := ParseJSON(data)
	if err != nil {
		return err
	}
	*s = parsed

	return nil
}
