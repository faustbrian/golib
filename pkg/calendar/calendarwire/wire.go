// Package calendarwire provides bounded canonical wire helpers for Date.
package calendarwire

import (
	"errors"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

const (
	// Version identifies the stable canonical date wire contract.
	Version = 1
	// MaxBytes bounds an encoded canonical date including JSON syntax.
	MaxBytes = 64
)

// ErrSizeLimit identifies input exceeding MaxBytes.
var ErrSizeLimit = errors.New("calendar/wire: size limit exceeded")

// EncodeDate encodes a Date as the version-1 canonical JSON string.
func EncodeDate(date calendar.Date) ([]byte, error) {
	if !date.IsValid() {
		return nil, calendar.ErrInvalidDate
	}
	return date.MarshalJSON()
}

// DecodeDate decodes exactly one bounded canonical JSON date string.
func DecodeDate(payload []byte) (calendar.Date, error) {
	if len(payload) > MaxBytes {
		return calendar.Date{}, ErrSizeLimit
	}
	if len(payload) != calendar.MaxParseBytes+2 || payload[0] != '"' || payload[len(payload)-1] != '"' {
		return calendar.Date{}, calendar.ErrInvalidFormat
	}
	return calendar.ParseDate(string(payload[1 : len(payload)-1]))
}
