package prompts

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Decimal is a canonical exact base-10 value.
type Decimal struct {
	digits   string
	scale    uint
	negative bool
}

func (decimal Decimal) String() string {
	digits := decimal.digits
	if digits == "" {
		digits = "0"
	}
	if decimal.scale > 0 {
		split := len(digits) - int(decimal.scale)
		if split <= 0 {
			digits = "0." + strings.Repeat("0", -split) + digits
		} else {
			digits = digits[:split] + "." + digits[split:]
		}
	}
	if decimal.negative && digits != "0" {
		return "-" + digits
	}

	return digits
}

// Scale returns the number of fractional decimal digits.
func (decimal Decimal) Scale() uint { return decimal.scale }

// Date is a calendar date without a time zone or time of day.
type Date struct {
	year  int
	month time.Month
	day   int
}

func (date Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", date.year, date.month, date.day)
}

// Year returns the proleptic Gregorian calendar year.
func (date Date) Year() int { return date.year }

// Month returns the Gregorian calendar month.
func (date Date) Month() time.Month { return date.month }

// Day returns the one-based day of the month.
func (date Date) Day() int { return date.day }

// TimeOfDay is a wall-clock time without a date or time zone.
type TimeOfDay struct {
	hour       int
	minute     int
	second     int
	nanosecond int
}

func (value TimeOfDay) String() string {
	base := fmt.Sprintf("%02d:%02d:%02d", value.hour, value.minute, value.second)
	if value.nanosecond == 0 {
		return base
	}

	fraction := strings.TrimRight(fmt.Sprintf("%09d", value.nanosecond), "0")

	return base + "." + fraction
}

// Hour returns the hour in the range 0 through 23.
func (value TimeOfDay) Hour() int { return value.hour }

// Minute returns the minute in the range 0 through 59.
func (value TimeOfDay) Minute() int { return value.minute }

// Second returns the second in the range 0 through 59.
func (value TimeOfDay) Second() int { return value.second }

// Nanosecond returns the fractional second in nanoseconds.
func (value TimeOfDay) Nanosecond() int { return value.nanosecond }

// PathKind is caller intent only; parsing never consults or mutates the file
// system.
type PathKind uint8

const (
	PathAny PathKind = iota
	PathFile
	PathDirectory
)

// Path is an unverified filesystem path with explicit caller intent.
type Path struct {
	value string
	kind  PathKind
}

func (path Path) String() string { return path.value }

// Kind returns caller intent without asserting anything about the filesystem.
func (path Path) Kind() PathKind { return path.kind }

func parseDecimal(input string) (Decimal, error) {
	if input == "" {
		return Decimal{}, parseIssue("decimal")
	}
	negative := false
	if input[0] == '+' || input[0] == '-' {
		negative = input[0] == '-'
		input = input[1:]
	}
	parts := strings.Split(input, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Decimal{}, parseIssue("decimal")
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return Decimal{}, parseIssue("decimal")
			}
		}
	}
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		digits = "0"
		negative = false
	}

	return Decimal{digits: digits, scale: uint(len(fraction)), negative: negative}, nil
}

func parseDate(input string) (Date, error) {
	parsed, err := time.Parse("2006-01-02", input)
	if err != nil {
		return Date{}, parseIssue("date")
	}

	return Date{year: parsed.Year(), month: parsed.Month(), day: parsed.Day()}, nil
}

func parseTimeOfDay(input string) (TimeOfDay, error) {
	layouts := []string{"15:04", "15:04:05", "15:04:05.999999999"}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, input)
		if err == nil {
			return TimeOfDay{hour: parsed.Hour(), minute: parsed.Minute(), second: parsed.Second(), nanosecond: parsed.Nanosecond()}, nil
		}
	}

	return TimeOfDay{}, parseIssue("time")
}

func parseIssue(kind string) error {
	return NewValidationIssue("invalid_"+kind, "Invalid "+kind+" value")
}

func parseInteger(input string) (int64, error) {
	value, err := strconv.ParseInt(input, 10, 64)
	if err != nil {
		return 0, parseIssue("integer")
	}

	return value, nil
}
