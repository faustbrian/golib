package postgres

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/jackc/pgx/v5/pgtype"
)

// InstantRange is a nullable database/sql tstzrange adapter.
type InstantRange struct {
	period instant.Period
	valid  bool
}

// NewInstantRange validates and wraps a losslessly representable period.
func NewInstantRange(period instant.Period) (InstantRange, error) {
	value, err := InstantRangeValue(period)
	if err != nil {
		return InstantRange{}, err
	}
	canonical, _ := InstantPeriod(value)
	return InstantRange{period: canonical, valid: true}, nil
}

// Period returns the wrapped period and whether the SQL value is non-NULL.
func (r InstantRange) Period() (instant.Period, bool) {
	return r.period, r.valid
}

// Value implements driver.Valuer using strict canonical PostgreSQL range text.
func (r InstantRange) Value() (driver.Value, error) {
	if !r.valid {
		return nil, nil //nolint:nilnil // database/sql encodes SQL NULL as nil, nil.
	}
	value, _ := InstantRangeValue(r.period)
	return string(pgBracket(value.LowerType, true)) +
		`"` + value.Lower.Format(time.RFC3339Nano) + `","` +
		value.Upper.Format(time.RFC3339Nano) + `"` +
		string(pgBracket(value.UpperType, false)), nil
}

// Scan implements sql.Scanner for NULL, string, and []byte sources.
func (r *InstantRange) Scan(source any) error {
	if r == nil {
		return temporal.ErrUnsupported
	}
	if source == nil {
		*r = InstantRange{}
		return nil
	}
	text, err := sqlText(source)
	if err != nil {
		return err
	}
	value, err := parseInstantRange(text)
	if err != nil {
		return err
	}
	period, err := InstantPeriod(value)
	if err != nil {
		return err
	}
	*r = InstantRange{period: period, valid: true}
	return nil
}

// DateRange is a nullable database/sql daterange adapter. Noncanonical input
// bounds are normalized to represented civil dates.
type DateRange struct {
	period dateperiod.Period
	valid  bool
}

// NewDateRange validates, canonicalizes, and wraps a civil-date period.
func NewDateRange(period dateperiod.Period) (DateRange, error) {
	value, err := DateRangeValue(period)
	if err != nil {
		return DateRange{}, err
	}
	canonical, _ := DatePeriod(value)
	return DateRange{period: canonical, valid: true}, nil
}

// Period returns the wrapped period and whether the SQL value is non-NULL.
func (r DateRange) Period() (dateperiod.Period, bool) {
	return r.period, r.valid
}

// Value implements driver.Valuer in PostgreSQL's canonical daterange form.
func (r DateRange) Value() (driver.Value, error) {
	if !r.valid {
		return nil, nil //nolint:nilnil // database/sql encodes SQL NULL as nil, nil.
	}
	value, _ := DateRangeValue(r.period)
	return string(pgBracket(value.LowerType, true)) + value.Lower.String() + "," +
		value.Upper.String() + string(pgBracket(value.UpperType, false)), nil
}

// Scan implements sql.Scanner for NULL, string, and []byte sources.
func (r *DateRange) Scan(source any) error {
	if r == nil {
		return temporal.ErrUnsupported
	}
	if source == nil {
		*r = DateRange{}
		return nil
	}
	text, err := sqlText(source)
	if err != nil {
		return err
	}
	value, err := parseDateRange(text)
	if err != nil {
		return err
	}
	period, err := DatePeriod(value)
	if err != nil {
		return err
	}
	*r = DateRange{period: period, valid: true}
	return nil
}

func parseInstantRange(text string) (pgtype.Range[time.Time], error) {
	lowerType, upperType, body, err := parseRangeShell(text)
	if err != nil {
		return pgtype.Range[time.Time]{}, err
	}
	if len(body) < 5 || body[0] != '"' || body[len(body)-1] != '"' {
		return pgtype.Range[time.Time]{}, temporal.ErrParse
	}
	inner := body[1 : len(body)-1]
	separator := strings.Index(inner, `","`)
	if separator <= 0 || strings.Contains(inner[separator+3:], `","`) {
		return pgtype.Range[time.Time]{}, temporal.ErrParse
	}
	lower, err := time.Parse(time.RFC3339Nano, inner[:separator])
	if err != nil {
		return pgtype.Range[time.Time]{}, fmt.Errorf("%w: lower timestamp", temporal.ErrParse)
	}
	upper, err := time.Parse(time.RFC3339Nano, inner[separator+3:])
	if err != nil {
		return pgtype.Range[time.Time]{}, fmt.Errorf("%w: upper timestamp", temporal.ErrParse)
	}
	return pgtype.Range[time.Time]{
		Lower: lower, Upper: upper, LowerType: lowerType, UpperType: upperType, Valid: true,
	}, nil
}

func parseDateRange(text string) (pgtype.Range[calendar.Date], error) {
	lowerType, upperType, body, err := parseRangeShell(text)
	if err != nil {
		return pgtype.Range[calendar.Date]{}, err
	}
	separator := strings.IndexByte(body, ',')
	if separator <= 0 || separator == len(body)-1 || strings.ContainsRune(body[separator+1:], ',') {
		return pgtype.Range[calendar.Date]{}, temporal.ErrParse
	}
	lower, err := calendar.ParseDate(body[:separator])
	if err != nil {
		return pgtype.Range[calendar.Date]{}, fmt.Errorf("%w: lower date", temporal.ErrParse)
	}
	upper, err := calendar.ParseDate(body[separator+1:])
	if err != nil {
		return pgtype.Range[calendar.Date]{}, fmt.Errorf("%w: upper date", temporal.ErrParse)
	}
	return pgtype.Range[calendar.Date]{
		Lower: lower, Upper: upper, LowerType: lowerType, UpperType: upperType, Valid: true,
	}, nil
}

func parseRangeShell(text string) (pgtype.BoundType, pgtype.BoundType, string, error) {
	if len(text) > temporal.DefaultLimits().ParseBytes {
		return 0, 0, "", &temporal.LimitError{
			Field: "parse_bytes", Value: len(text), Max: temporal.DefaultLimits().ParseBytes,
		}
	}
	if len(text) < 3 || text == "empty" {
		return 0, 0, "", temporal.ErrUnsupported
	}
	lower, ok := pgBoundFromBracket(text[0], true)
	if !ok {
		return 0, 0, "", temporal.ErrParse
	}
	upper, ok := pgBoundFromBracket(text[len(text)-1], false)
	if !ok {
		return 0, 0, "", temporal.ErrParse
	}
	return lower, upper, text[1 : len(text)-1], nil
}

func sqlText(source any) (string, error) {
	switch value := source.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", temporal.ErrUnsupported
	}
}

func pgBracket(bound pgtype.BoundType, lower bool) byte {
	if lower {
		if bound == pgtype.Inclusive {
			return '['
		}
		return '('
	}
	if bound == pgtype.Inclusive {
		return ']'
	}
	return ')'
}

func pgBoundFromBracket(bracket byte, lower bool) (pgtype.BoundType, bool) {
	if lower {
		switch bracket {
		case '[':
			return pgtype.Inclusive, true
		case '(':
			return pgtype.Exclusive, true
		}
	} else {
		switch bracket {
		case ']':
			return pgtype.Inclusive, true
		case ')':
			return pgtype.Exclusive, true
		}
	}
	return 0, false
}
