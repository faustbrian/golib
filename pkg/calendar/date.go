// Package calendar provides immutable civil-calendar values and arithmetic.
// A Date represents a Gregorian calendar day, never an instant or timezone.
package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	// MinYear is the earliest supported civil year.
	MinYear = 1
	// MaxYear is the latest supported civil year.
	MaxYear = 9999
	// MaxParseBytes bounds all canonical date input.
	MaxParseBytes = 10
)

var (
	// ErrInvalidDate identifies impossible, unsupported, and zero dates.
	ErrInvalidDate = errors.New("calendar: invalid date")
	// ErrInvalidFormat identifies non-canonical date input.
	ErrInvalidFormat = errors.New("calendar: invalid format")
	// ErrArithmetic identifies rejected or overflowing calendar arithmetic.
	ErrArithmetic = errors.New("calendar: arithmetic failed")
)

// ArithmeticPolicy defines how month and year arithmetic handles a day that
// does not exist in the destination month.
type ArithmeticPolicy uint8

const (
	// Clamp selects the last valid day in the destination month.
	Clamp ArithmeticPolicy = iota + 1
	// Reject returns ErrArithmetic when the original day does not exist.
	Reject
	// Overflow carries excess days forward from the destination month.
	Overflow
)

// Date is an immutable proleptic Gregorian civil date. Its zero value is
// invalid; use NewDate, MustDate, or ParseDate to construct a valid value.
type Date struct {
	year  uint16
	month uint8
	day   uint8
}

// ComponentDifference is a signed calendar decomposition. Years are applied
// first, then months under the caller's policy, then exact calendar days. All
// non-zero components have the sign of the destination relative to the source.
type ComponentDifference struct {
	Years  int
	Months int
	Days   int
}

// NewDate validates and constructs a Date.
func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < MinYear || year > MaxYear || month < time.January || month > time.December || day < 1 || day > daysInMonth(year, month) {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidDate, year, month, day)
	}
	// #nosec G115 -- all values were validated against their target widths above.
	return Date{year: uint16(year), month: uint8(month), day: uint8(day)}, nil
}

// MustDate is NewDate but panics when the components are invalid. It is
// intended for constants, fixtures, and package initialization.
func MustDate(year int, month time.Month, day int) Date {
	d, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// ParseDate parses exactly YYYY-MM-DD using ASCII digits.
func ParseDate(input string) (Date, error) {
	if len(input) != MaxParseBytes || !utf8.ValidString(input) || input[4] != '-' || input[7] != '-' {
		return Date{}, fmt.Errorf("%w: expected YYYY-MM-DD", ErrInvalidFormat)
	}
	for i := range len(input) {
		if i == 4 || i == 7 {
			continue
		}
		if input[i] < '0' || input[i] > '9' {
			return Date{}, fmt.Errorf("%w: expected ASCII digits", ErrInvalidFormat)
		}
	}
	year := decimal(input[0:4])
	month := decimal(input[5:7])
	day := decimal(input[8:10])
	d, err := NewDate(year, time.Month(month), day)
	if err != nil {
		return Date{}, fmt.Errorf("%w: %w", ErrInvalidFormat, err)
	}
	return d, nil
}

func decimal(s string) int {
	n := 0
	for i := range len(s) {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// DateFromTime extracts the civil date observed in loc. A nil location is
// rejected because conversion must always be explicit.
func DateFromTime(instant time.Time, loc *time.Location) (Date, error) {
	if loc == nil {
		return Date{}, fmt.Errorf("%w: nil location", ErrInvalidDate)
	}
	year, month, day := instant.In(loc).Date()
	return NewDate(year, month, day)
}

// IsValid reports whether d is a supported Date.
func (d Date) IsValid() bool {
	_, err := NewDate(int(d.year), time.Month(d.month), int(d.day))
	return err == nil
}

// Year returns the Gregorian year, or zero for an invalid Date.
func (d Date) Year() int { return int(d.year) }

// Month returns the Gregorian month, or zero for an invalid Date.
func (d Date) Month() time.Month { return time.Month(d.month) }

// Day returns the day of month, or zero for an invalid Date.
func (d Date) Day() int { return int(d.day) }

// String returns canonical YYYY-MM-DD, or an empty string for an invalid Date.
func (d Date) String() string {
	if !d.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// Equal reports whether two dates are equal. Invalid dates compare unequal.
func (d Date) Equal(other Date) bool { return d.IsValid() && d == other }

// Compare returns -1, 0, or 1 according to chronological civil ordering.
// It returns ErrInvalidDate if either operand is invalid.
func (d Date) Compare(other Date) (int, error) {
	if !d.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	if d == other {
		return 0, nil
	}
	if d.year < other.year || d.year == other.year && (d.month < other.month || d.month == other.month && d.day < other.day) {
		return -1, nil
	}
	return 1, nil
}

// IsLeapYear reports whether d's Gregorian year contains February 29.
func (d Date) IsLeapYear() bool { return d.IsValid() && isLeap(d.Year()) }

// DaysInMonth returns the length of d's month, or zero for an invalid Date.
func (d Date) DaysInMonth() int {
	if !d.IsValid() {
		return 0
	}
	return daysInMonth(d.Year(), d.Month())
}

// Weekday returns d's weekday. It returns time.Sunday for an invalid Date;
// callers that may hold a zero value should check IsValid first.
func (d Date) Weekday() time.Weekday { return d.asTime().Weekday() }

// DayOfYear returns 1 through 366, or zero for an invalid Date.
func (d Date) DayOfYear() int {
	if !d.IsValid() {
		return 0
	}
	return d.asTime().YearDay()
}

// ISOWeek returns the ISO 8601 week-year and week, or zeros if d is invalid.
func (d Date) ISOWeek() (year, week int) {
	if !d.IsValid() {
		return 0, 0
	}
	return d.asTime().ISOWeek()
}

// AddDays adds calendar days, never elapsed durations.
func (d Date) AddDays(days int) (Date, error) {
	if !d.IsValid() {
		return Date{}, ErrInvalidDate
	}
	ordinal := dayOrdinal(d)
	maximum := dayOrdinal(MustDate(MaxYear, time.December, 31))
	if days < -ordinal || days > maximum-ordinal {
		return Date{}, fmt.Errorf("%w: year range", ErrArithmetic)
	}
	t := d.asTime().AddDate(0, 0, days)
	return NewDate(t.Date())
}

// AddWeeks adds calendar weeks.
func (d Date) AddWeeks(weeks int) (Date, error) {
	days, ok := checkedMultiply(weeks, 7)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddDays(days)
}

// AddMonths adds calendar months using policy.
func (d Date) AddMonths(months int, policy ArithmeticPolicy) (Date, error) {
	if !d.IsValid() {
		return Date{}, ErrInvalidDate
	}
	if policy < Clamp || policy > Overflow {
		return Date{}, fmt.Errorf("%w: unknown policy", ErrArithmetic)
	}
	base := (d.Year()-1)*12 + int(d.Month()) - 1
	maximum := MaxYear*12 - 1
	if months < -base || months > maximum-base {
		return Date{}, fmt.Errorf("%w: year range", ErrArithmetic)
	}
	total := base + months
	year, monthIndex := total/12+1, total%12
	month := time.Month(monthIndex + 1)
	last := daysInMonth(year, month)
	if d.Day() <= last {
		return NewDate(year, month, d.Day())
	}
	if policy == Clamp {
		return NewDate(year, month, last)
	}
	if policy == Reject {
		return Date{}, fmt.Errorf("%w: day %d absent from %04d-%02d", ErrArithmetic, d.Day(), year, month)
	}
	t := time.Date(year, month, d.Day(), 0, 0, 0, 0, time.UTC)
	return NewDate(t.Date())
}

// AddQuarters adds three months per quarter using policy.
func (d Date) AddQuarters(quarters int, policy ArithmeticPolicy) (Date, error) {
	months, ok := checkedMultiply(quarters, 3)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddMonths(months, policy)
}

// AddSemesters adds six months per semester using policy.
func (d Date) AddSemesters(semesters int, policy ArithmeticPolicy) (Date, error) {
	months, ok := checkedMultiply(semesters, 6)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddMonths(months, policy)
}

// AddYears adds calendar years using policy.
func (d Date) AddYears(years int, policy ArithmeticPolicy) (Date, error) {
	months, ok := checkedMultiply(years, 12)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddMonths(months, policy)
}

// SubDays subtracts calendar days.
func (d Date) SubDays(days int) (Date, error) {
	value, ok := checkedNegate(days)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddDays(value)
}

// SubWeeks subtracts calendar weeks.
func (d Date) SubWeeks(weeks int) (Date, error) {
	value, ok := checkedNegate(weeks)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddWeeks(value)
}

// SubMonths subtracts calendar months using policy.
func (d Date) SubMonths(months int, policy ArithmeticPolicy) (Date, error) {
	value, ok := checkedNegate(months)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddMonths(value, policy)
}

// SubQuarters subtracts calendar quarters using policy.
func (d Date) SubQuarters(quarters int, policy ArithmeticPolicy) (Date, error) {
	value, ok := checkedNegate(quarters)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddQuarters(value, policy)
}

// SubSemesters subtracts calendar semesters using policy.
func (d Date) SubSemesters(semesters int, policy ArithmeticPolicy) (Date, error) {
	value, ok := checkedNegate(semesters)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddSemesters(value, policy)
}

// SubYears subtracts calendar years using policy.
func (d Date) SubYears(years int, policy ArithmeticPolicy) (Date, error) {
	value, ok := checkedNegate(years)
	if !ok {
		return Date{}, ErrArithmetic
	}
	return d.AddYears(value, policy)
}

// ComponentsUntil decomposes the movement from d to other into years, months,
// and days using the caller's explicit month/year arithmetic policy.
func (d Date) ComponentsUntil(other Date, policy ArithmeticPolicy) (ComponentDifference, error) {
	comparison, err := d.Compare(other)
	if err != nil {
		return ComponentDifference{}, err
	}
	if comparison == 0 {
		return ComponentDifference{}, nil
	}
	if comparison > 0 {
		years := other.Year() - d.Year()
		cursor, err := d.AddYears(years, policy)
		if err != nil {
			return ComponentDifference{}, err
		}
		if order, _ := cursor.Compare(other); order < 0 {
			years++
			cursor, err = d.AddYears(years, policy)
			if err != nil {
				return ComponentDifference{}, err
			}
		}
		months := (other.Year()-cursor.Year())*12 + int(other.Month()-cursor.Month())
		candidate, err := cursor.AddMonths(months, policy)
		if err != nil {
			return ComponentDifference{}, err
		}
		if order, _ := candidate.Compare(other); order < 0 {
			months++
			candidate, err = cursor.AddMonths(months, policy)
			if err != nil {
				return ComponentDifference{}, err
			}
		}
		return ComponentDifference{Years: years, Months: months, Days: candidate.DaysUntil(other)}, nil
	}
	years := other.Year() - d.Year()
	cursor, err := d.AddYears(years, policy)
	if err != nil {
		return ComponentDifference{}, err
	}
	if order, _ := cursor.Compare(other); order > 0 {
		years--
		cursor, err = d.AddYears(years, policy)
		if err != nil {
			return ComponentDifference{}, err
		}
	}
	months := (other.Year()-cursor.Year())*12 + int(other.Month()-cursor.Month())
	candidate, err := cursor.AddMonths(months, policy)
	if err != nil {
		return ComponentDifference{}, err
	}
	if order, _ := candidate.Compare(other); order > 0 {
		months--
		candidate, err = cursor.AddMonths(months, policy)
		if err != nil {
			return ComponentDifference{}, err
		}
	}
	return ComponentDifference{Years: years, Months: months, Days: candidate.DaysUntil(other)}, nil
}

// DaysUntil returns other-d in calendar days. Invalid operands return zero.
func (d Date) DaysUntil(other Date) int {
	if !d.IsValid() || !other.IsValid() {
		return 0
	}
	return dayOrdinal(other) - dayOrdinal(d)
}

// MarshalText implements encoding.TextMarshaler.
func (d Date) MarshalText() ([]byte, error) {
	if !d.IsValid() {
		return nil, ErrInvalidDate
	}
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(text []byte) error {
	if d == nil {
		return ErrInvalidDate
	}
	parsed, err := ParseDate(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalJSON encodes a Date as its canonical JSON string.
func (d Date) MarshalJSON() ([]byte, error) {
	if !d.IsValid() {
		return nil, ErrInvalidDate
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON decodes a canonical JSON date string.
func (d *Date) UnmarshalJSON(data []byte) error {
	if d == nil {
		return ErrInvalidDate
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%w: JSON string: %w", ErrInvalidFormat, err)
	}
	return d.UnmarshalText([]byte(text))
}

func (d Date) asTime() time.Time {
	if !d.IsValid() {
		return time.Time{}
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func isLeap(year int) bool { return year%4 == 0 && (year%100 != 0 || year%400 == 0) }

func daysInMonth(year int, month time.Month) int {
	if month < time.January || month > time.December {
		return 0
	}
	if month == time.February && isLeap(year) {
		return 29
	}
	lengths := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	return lengths[month-1]
}

func checkedMultiply(a, b int) (int, bool) {
	result := a * b
	return result, a == 0 || result/a == b
}

func checkedNegate(value int) (int, bool) {
	if value == -int(^uint(0)>>1)-1 {
		return 0, false
	}
	return -value, true
}

func dayOrdinal(date Date) int {
	year := date.Year() - 1
	return year*365 + year/4 - year/100 + year/400 + date.DayOfYear() - 1
}
