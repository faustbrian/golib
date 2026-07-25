// Package postgres provides database/sql and pgx adapters for PostgreSQL date.
// Ordinary Date rejects NULL and infinity; InfinityDate models those sentinels
// explicitly when an application needs them.
package postgres

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrNull identifies SQL NULL where an ordinary civil date is required.
	ErrNull = errors.New("calendar/postgres: null date")
	// ErrInfinity identifies PostgreSQL infinity where an ordinary date is required.
	ErrInfinity = errors.New("calendar/postgres: infinity requires InfinityDate")
)

// Date adapts a non-null, finite calendar.Date to database/sql and pgx.
type Date struct{ date calendar.Date }

// NewDate constructs a PostgreSQL adapter. Invalid dates remain invalid and
// return calendar.ErrInvalidDate when encoded.
func NewDate(date calendar.Date) Date { return Date{date: date} }

// CalendarDate returns the wrapped civil date.
func (d Date) CalendarDate() calendar.Date { return d.date }

// Value implements database/sql/driver.Valuer using canonical date text.
func (d Date) Value() (driver.Value, error) {
	if !d.date.IsValid() {
		return nil, calendar.ErrInvalidDate
	}
	return d.date.String(), nil
}

// Scan implements database/sql.Scanner.
func (d *Date) Scan(source any) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	date, err := scanFinite(source)
	if err != nil {
		return err
	}
	d.date = date
	return nil
}

// DateValue implements pgtype.DateValuer.
func (d Date) DateValue() (pgtype.Date, error) {
	if !d.date.IsValid() {
		return pgtype.Date{}, calendar.ErrInvalidDate
	}
	return pgtype.Date{Time: time.Date(d.date.Year(), d.date.Month(), d.date.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, nil
}

// ScanDate implements pgtype.DateScanner.
func (d *Date) ScanDate(value pgtype.Date) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	if !value.Valid {
		return ErrNull
	}
	if value.InfinityModifier != pgtype.Finite {
		return ErrInfinity
	}
	date, err := calendar.NewDate(value.Time.Date())
	if err != nil {
		return err
	}
	d.date = date
	return nil
}

// InfinityKind classifies a finite or infinite PostgreSQL date.
type InfinityKind int8

const (
	// NegativeInfinity represents PostgreSQL -infinity.
	NegativeInfinity InfinityKind = -1
	// Finite represents an ordinary civil date.
	Finite InfinityKind = 0
	// PositiveInfinity represents PostgreSQL infinity.
	PositiveInfinity InfinityKind = 1
)

// InfinityDate is the explicit sum type for finite and infinite PostgreSQL
// dates. Its zero value is invalid because it has no finite Date.
type InfinityDate struct {
	kind InfinityKind
	date calendar.Date
}

// NewInfinityDate constructs an infinite value. Finite is rejected at encode
// time because callers must use NewFiniteDate with a concrete date.
func NewInfinityDate(kind InfinityKind) InfinityDate { return InfinityDate{kind: kind} }

// NewFiniteDate constructs an infinity-aware finite value.
func NewFiniteDate(date calendar.Date) InfinityDate { return InfinityDate{kind: Finite, date: date} }

// Kind returns the value classification.
func (d InfinityDate) Kind() InfinityKind { return d.kind }

// Date returns the finite date, or an invalid Date for infinities.
func (d InfinityDate) Date() calendar.Date { return d.date }

// Value implements database/sql/driver.Valuer.
func (d InfinityDate) Value() (driver.Value, error) {
	switch d.kind {
	case NegativeInfinity:
		return "-infinity", nil
	case PositiveInfinity:
		return "infinity", nil
	case Finite:
		if !d.date.IsValid() {
			return nil, calendar.ErrInvalidDate
		}
		return d.date.String(), nil
	default:
		return nil, ErrInfinity
	}
}

// Scan implements database/sql.Scanner.
func (d *InfinityDate) Scan(source any) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	if source == nil {
		return ErrNull
	}
	text, ok := sourceText(source)
	if ok {
		switch text {
		case "-infinity":
			*d = NewInfinityDate(NegativeInfinity)
			return nil
		case "infinity":
			*d = NewInfinityDate(PositiveInfinity)
			return nil
		}
	}
	date, err := scanFinite(source)
	if err != nil {
		return err
	}
	*d = NewFiniteDate(date)
	return nil
}

// DateValue implements pgtype.DateValuer.
func (d InfinityDate) DateValue() (pgtype.Date, error) {
	switch d.kind {
	case NegativeInfinity:
		return pgtype.Date{Valid: true, InfinityModifier: pgtype.NegativeInfinity}, nil
	case PositiveInfinity:
		return pgtype.Date{Valid: true, InfinityModifier: pgtype.Infinity}, nil
	case Finite:
		return NewDate(d.date).DateValue()
	default:
		return pgtype.Date{}, ErrInfinity
	}
}

// ScanDate implements pgtype.DateScanner.
func (d *InfinityDate) ScanDate(value pgtype.Date) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	if !value.Valid {
		return ErrNull
	}
	switch value.InfinityModifier {
	case pgtype.NegativeInfinity:
		*d = NewInfinityDate(NegativeInfinity)
		return nil
	case pgtype.Infinity:
		*d = NewInfinityDate(PositiveInfinity)
		return nil
	case pgtype.Finite:
		date, err := calendar.NewDate(value.Time.Date())
		if err != nil {
			return err
		}
		*d = NewFiniteDate(date)
		return nil
	default:
		return ErrInfinity
	}
}

func scanFinite(source any) (calendar.Date, error) {
	if source == nil {
		return calendar.Date{}, ErrNull
	}
	if text, ok := sourceText(source); ok {
		if text == "infinity" || text == "-infinity" {
			return calendar.Date{}, ErrInfinity
		}
		return calendar.ParseDate(text)
	}
	if value, ok := source.(time.Time); ok {
		return calendar.NewDate(value.Date())
	}
	return calendar.Date{}, fmt.Errorf("calendar/postgres: cannot scan %T", source)
}

func sourceText(source any) (string, bool) {
	switch value := source.(type) {
	case string:
		return value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}
