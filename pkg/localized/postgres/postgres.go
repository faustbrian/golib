// Package postgres provides explicit SQL and pgx JSONB integration.
package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"strings"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/jackc/pgx/v5/pgtype"
)

// Error is a stable privacy-safe persistence error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

// ErrUnsupportedDatabaseType reports a value outside JSON text forms.
const ErrUnsupportedDatabaseType Error = "localized postgres: unsupported database type"

// Text is a nullable localized value for database/sql boundaries.
type Text struct {
	Localized localized.Text
	Valid     bool
}

// NewText creates a non-null SQL value.
func NewText(value localized.Text) Text {
	return Text{Localized: value, Valid: true}
}

// Value implements driver.Valuer. Invalid values become SQL NULL.
func (t Text) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return localized.EncodeJSON(t.Localized)
}

// Scan implements sql.Scanner transactionally for JSON text and SQL NULL.
func (t *Text) Scan(source any) error {
	if t == nil {
		return ErrUnsupportedDatabaseType
	}
	if source == nil {
		*t = Text{}
		return nil
	}
	var data []byte
	switch value := source.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	default:
		return ErrUnsupportedDatabaseType
	}
	decoded, err := localized.DecodeJSON(data, localized.DecodeOptions{})
	if err != nil {
		return err
	}
	*t = NewText(decoded)
	return nil
}

// JSONBCodec returns a pgx codec that preserves generic JSONB behavior while
// using localized's canonical encoding and strict decoding for Text values.
func JSONBCodec() *pgtype.JSONBCodec {
	return &pgtype.JSONBCodec{
		Marshal: func(value any) ([]byte, error) {
			switch typed := value.(type) {
			case localized.Text:
				return localized.EncodeJSON(typed)
			case *localized.Text:
				if typed == nil {
					return []byte("null"), nil
				}
				return localized.EncodeJSON(*typed)
			default:
				return json.Marshal(value)
			}
		},
		Unmarshal: func(data []byte, target any) error {
			if typed, ok := target.(*localized.Text); ok {
				decoded, err := localized.DecodeJSON(data, localized.DecodeOptions{})
				if err != nil {
					return err
				}
				*typed = decoded
				return nil
			}
			return json.Unmarshal(data, target)
		},
	}
}

// Row is a normalized persistence representation with a canonical locale key.
type Row struct {
	Locale string
	Text   string
}

// Rows returns deterministic caller-owned normalized rows.
func Rows(value localized.Text) []Row {
	entries := value.Entries()
	rows := make([]Row, len(entries))
	for i, entry := range entries {
		rows[i] = Row{Locale: entry.Locale.String(), Text: entry.Text}
	}
	return rows
}

// FromRows constructs an immutable value from normalized rows.
func FromRows(rows []Row) (localized.Text, error) {
	entries := make([]localized.Entry, 0, len(rows))
	for _, row := range rows {
		if strings.ContainsAny(row.Locale, "_ \t\r\n") {
			return localized.Text{}, localized.ErrInvalidLocale
		}
		tag, err := locale.Parse(row.Locale)
		if err != nil {
			return localized.Text{}, localized.ErrInvalidLocale
		}
		entries = append(entries, localized.Entry{Locale: tag, Text: row.Text})
	}
	return localized.NewText(entries...)
}
