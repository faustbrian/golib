package calendar

import (
	"fmt"
	"time"
)

// Year is an immutable supported Gregorian year. Its zero value is invalid.
type Year struct{ value uint16 }

// NewYear validates and constructs a Year.
func NewYear(year int) (Year, error) {
	if year < MinYear || year > MaxYear {
		return Year{}, fmt.Errorf("%w: year %d", ErrInvalidDate, year)
	}
	return Year{value: uint16(year)}, nil
}

// ParseYear parses exactly YYYY.
func ParseYear(input string) (Year, error) {
	if !asciiDigits(input, 4) {
		return Year{}, fmt.Errorf("%w: expected YYYY", ErrInvalidFormat)
	}
	return NewYear(decimal(input))
}

// Value returns the numeric year, or zero for an invalid Year.
func (y Year) Value() int { return int(y.value) }

// IsValid reports whether y is supported.
func (y Year) IsValid() bool { return y.Value() >= MinYear && y.Value() <= MaxYear }

// String returns YYYY, or an empty string for an invalid Year.
func (y Year) String() string {
	if !y.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d", y.value)
}

// IsLeap reports whether y has 366 days.
func (y Year) IsLeap() bool { return y.IsValid() && isLeap(y.Value()) }

// Length returns 365 or 366, or zero for an invalid Year.
func (y Year) Length() int {
	if !y.IsValid() {
		return 0
	}
	if y.IsLeap() {
		return 366
	}
	return 365
}

// FirstDate returns January 1, or an invalid Date when y is invalid.
func (y Year) FirstDate() Date {
	if !y.IsValid() {
		return Date{}
	}
	return MustDate(y.Value(), time.January, 1)
}

// LastDate returns December 31, or an invalid Date when y is invalid.
func (y Year) LastDate() Date {
	if !y.IsValid() {
		return Date{}
	}
	return MustDate(y.Value(), time.December, 31)
}

// Contains reports whether d belongs to y.
func (y Year) Contains(d Date) bool { return y.IsValid() && d.IsValid() && d.Year() == y.Value() }

// Add returns a year offset from y.
func (y Year) Add(years int) (Year, error) {
	if !y.IsValid() {
		return Year{}, ErrInvalidDate
	}
	return NewYear(y.Value() + years)
}

// Compare returns -1, 0, or 1 according to chronological ordering.
func (y Year) Compare(other Year) (int, error) {
	if !y.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	return compareInts(y.Value(), other.Value()), nil
}

// YearMonth is an immutable Gregorian month. Its zero value is invalid.
type YearMonth struct {
	year  uint16
	month uint8
}

// NewYearMonth validates and constructs a YearMonth.
func NewYearMonth(year int, month time.Month) (YearMonth, error) {
	if _, err := NewDate(year, month, 1); err != nil {
		return YearMonth{}, err
	}
	// #nosec G115 -- NewDate validated both values against their target widths.
	return YearMonth{year: uint16(year), month: uint8(month)}, nil
}

// MustYearMonth is NewYearMonth but panics for invalid components.
func MustYearMonth(year int, month time.Month) YearMonth {
	value, err := NewYearMonth(year, month)
	if err != nil {
		panic(err)
	}
	return value
}

// ParseYearMonth parses exactly YYYY-MM.
func ParseYearMonth(input string) (YearMonth, error) {
	if len(input) != 7 || input[4] != '-' || !asciiDigits(input[:4], 4) || !asciiDigits(input[5:], 2) {
		return YearMonth{}, fmt.Errorf("%w: expected YYYY-MM", ErrInvalidFormat)
	}
	return NewYearMonth(decimal(input[:4]), time.Month(decimal(input[5:])))
}

// Year returns the Gregorian year, or zero when invalid.
func (ym YearMonth) Year() int { return int(ym.year) }

// Month returns the Gregorian month, or zero when invalid.
func (ym YearMonth) Month() time.Month { return time.Month(ym.month) }

// IsValid reports whether ym is supported.
func (ym YearMonth) IsValid() bool {
	_, err := NewYearMonth(ym.Year(), ym.Month())
	return err == nil
}

// String returns YYYY-MM, or an empty string when invalid.
func (ym YearMonth) String() string {
	if !ym.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", ym.year, ym.month)
}

// Length returns the number of days in ym, or zero when invalid.
func (ym YearMonth) Length() int {
	if !ym.IsValid() {
		return 0
	}
	return daysInMonth(ym.Year(), ym.Month())
}

// FirstDate returns the first date in ym.
func (ym YearMonth) FirstDate() Date {
	if !ym.IsValid() {
		return Date{}
	}
	return MustDate(ym.Year(), ym.Month(), 1)
}

// LastDate returns the last date in ym.
func (ym YearMonth) LastDate() Date {
	if !ym.IsValid() {
		return Date{}
	}
	return MustDate(ym.Year(), ym.Month(), ym.Length())
}

// Contains reports whether d belongs to ym.
func (ym YearMonth) Contains(d Date) bool {
	return ym.IsValid() && d.IsValid() && d.Year() == ym.Year() && d.Month() == ym.Month()
}

// AddMonths navigates by whole calendar months.
func (ym YearMonth) AddMonths(months int) (YearMonth, error) {
	if !ym.IsValid() {
		return YearMonth{}, ErrInvalidDate
	}
	d, err := ym.FirstDate().AddMonths(months, Reject)
	if err != nil {
		return YearMonth{}, err
	}
	return NewYearMonth(d.Year(), d.Month())
}

// Compare returns -1, 0, or 1 according to chronological ordering.
func (ym YearMonth) Compare(other YearMonth) (int, error) {
	if !ym.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	left := (ym.Year()-1)*12 + int(ym.Month())
	right := (other.Year()-1)*12 + int(other.Month())
	return compareInts(left, right), nil
}

// Quarter is an immutable quarter in a Gregorian year.
type Quarter struct {
	year    uint16
	quarter uint8
}

// NewQuarter validates and constructs a Quarter numbered 1 through 4.
func NewQuarter(year, quarter int) (Quarter, error) {
	if _, err := NewYear(year); err != nil || quarter < 1 || quarter > 4 {
		return Quarter{}, fmt.Errorf("%w: quarter %04d-Q%d", ErrInvalidDate, year, quarter)
	}
	// #nosec G115 -- year and quarter were validated above.
	return Quarter{year: uint16(year), quarter: uint8(quarter)}, nil
}

// ParseQuarter parses exactly YYYY-QN.
func ParseQuarter(input string) (Quarter, error) {
	if len(input) != 7 || input[4:6] != "-Q" || !asciiDigits(input[:4], 4) || input[6] < '1' || input[6] > '4' {
		return Quarter{}, fmt.Errorf("%w: expected YYYY-QN", ErrInvalidFormat)
	}
	return NewQuarter(decimal(input[:4]), int(input[6]-'0'))
}

// Year returns the containing year.
func (q Quarter) Year() int { return int(q.year) }

// Number returns 1 through 4, or zero when invalid.
func (q Quarter) Number() int { return int(q.quarter) }

// IsValid reports whether q is supported.
func (q Quarter) IsValid() bool {
	return q.Year() >= MinYear && q.Year() <= MaxYear && q.Number() >= 1 && q.Number() <= 4
}

// String returns YYYY-QN, or an empty string when invalid.
func (q Quarter) String() string {
	if !q.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d-Q%d", q.year, q.quarter)
}

// FirstDate returns the first date in q.
func (q Quarter) FirstDate() Date {
	if !q.IsValid() {
		return Date{}
	}
	return MustDate(q.Year(), time.Month((q.Number()-1)*3+1), 1)
}

// LastDate returns the last date in q.
func (q Quarter) LastDate() Date {
	if !q.IsValid() {
		return Date{}
	}
	return MustDate(q.Year(), time.Month(q.Number()*3), daysInMonth(q.Year(), time.Month(q.Number()*3)))
}

// Length returns the number of days in q.
func (q Quarter) Length() int { return q.FirstDate().DaysUntil(q.LastDate()) + boolInt(q.IsValid()) }

// Contains reports whether d belongs to q.
func (q Quarter) Contains(d Date) bool {
	return q.IsValid() && d.IsValid() && d.Year() == q.Year() && (int(d.Month())-1)/3+1 == q.Number()
}

// Add navigates by whole quarters.
func (q Quarter) Add(quarters int) (Quarter, error) {
	if !q.IsValid() {
		return Quarter{}, ErrInvalidDate
	}
	date, err := q.FirstDate().AddQuarters(quarters, Reject)
	if err != nil {
		return Quarter{}, err
	}
	return NewQuarter(date.Year(), (int(date.Month())-1)/3+1)
}

// Compare returns -1, 0, or 1 according to chronological ordering.
func (q Quarter) Compare(other Quarter) (int, error) {
	if !q.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	return compareInts((q.Year()-1)*4+q.Number(), (other.Year()-1)*4+other.Number()), nil
}

// Semester is an immutable half-year.
type Semester struct {
	year     uint16
	semester uint8
}

// NewSemester validates and constructs a Semester numbered 1 or 2.
func NewSemester(year, semester int) (Semester, error) {
	if _, err := NewYear(year); err != nil || semester < 1 || semester > 2 {
		return Semester{}, fmt.Errorf("%w: semester %04d-H%d", ErrInvalidDate, year, semester)
	}
	// #nosec G115 -- year and semester were validated above.
	return Semester{year: uint16(year), semester: uint8(semester)}, nil
}

// ParseSemester parses exactly YYYY-HN.
func ParseSemester(input string) (Semester, error) {
	if len(input) != 7 || input[4:6] != "-H" || !asciiDigits(input[:4], 4) || input[6] < '1' || input[6] > '2' {
		return Semester{}, fmt.Errorf("%w: expected YYYY-HN", ErrInvalidFormat)
	}
	return NewSemester(decimal(input[:4]), int(input[6]-'0'))
}

// Year returns the containing year.
func (s Semester) Year() int { return int(s.year) }

// Number returns 1 or 2, or zero when invalid.
func (s Semester) Number() int { return int(s.semester) }

// IsValid reports whether s is supported.
func (s Semester) IsValid() bool {
	return s.Year() >= MinYear && s.Year() <= MaxYear && s.Number() >= 1 && s.Number() <= 2
}

// String returns YYYY-HN, or an empty string when invalid.
func (s Semester) String() string {
	if !s.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d-H%d", s.year, s.semester)
}

// FirstDate returns the first date in s.
func (s Semester) FirstDate() Date {
	if !s.IsValid() {
		return Date{}
	}
	return MustDate(s.Year(), time.Month((s.Number()-1)*6+1), 1)
}

// LastDate returns the last date in s.
func (s Semester) LastDate() Date {
	if !s.IsValid() {
		return Date{}
	}
	month := time.Month(s.Number() * 6)
	return MustDate(s.Year(), month, daysInMonth(s.Year(), month))
}

// Length returns the number of days in s.
func (s Semester) Length() int { return s.FirstDate().DaysUntil(s.LastDate()) + boolInt(s.IsValid()) }

// Contains reports whether d belongs to s.
func (s Semester) Contains(d Date) bool {
	return s.IsValid() && d.IsValid() && d.Year() == s.Year() && (int(d.Month())-1)/6+1 == s.Number()
}

// Add navigates by whole semesters.
func (s Semester) Add(semesters int) (Semester, error) {
	if !s.IsValid() {
		return Semester{}, ErrInvalidDate
	}
	date, err := s.FirstDate().AddSemesters(semesters, Reject)
	if err != nil {
		return Semester{}, err
	}
	return NewSemester(date.Year(), (int(date.Month())-1)/6+1)
}

// Compare returns -1, 0, or 1 according to chronological ordering.
func (s Semester) Compare(other Semester) (int, error) {
	if !s.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	return compareInts((s.Year()-1)*2+s.Number(), (other.Year()-1)*2+other.Number()), nil
}

// ISOWeek is an immutable ISO 8601 week-year and week.
type ISOWeek struct {
	year uint16
	week uint8
}

// NewISOWeek validates and constructs an ISO week.
func NewISOWeek(year, week int) (ISOWeek, error) {
	if year < MinYear || year > MaxYear || week < 1 || week > isoWeeksInYear(year) {
		return ISOWeek{}, fmt.Errorf("%w: ISO week %04d-W%02d", ErrInvalidDate, year, week)
	}
	// #nosec G115 -- year and week were validated above.
	return ISOWeek{year: uint16(year), week: uint8(week)}, nil
}

// ParseISOWeek parses exactly YYYY-Www.
func ParseISOWeek(input string) (ISOWeek, error) {
	if len(input) != 8 || input[4:6] != "-W" || !asciiDigits(input[:4], 4) || !asciiDigits(input[6:], 2) {
		return ISOWeek{}, fmt.Errorf("%w: expected YYYY-Www", ErrInvalidFormat)
	}
	return NewISOWeek(decimal(input[:4]), decimal(input[6:]))
}

// Year returns the ISO week-year.
func (w ISOWeek) Year() int { return int(w.year) }

// Week returns the ISO week number.
func (w ISOWeek) Week() int { return int(w.week) }

// IsValid reports whether w exists.
func (w ISOWeek) IsValid() bool {
	_, err := NewISOWeek(w.Year(), w.Week())
	return err == nil
}

// String returns YYYY-Www, or an empty string when invalid.
func (w ISOWeek) String() string {
	if !w.IsValid() {
		return ""
	}
	return fmt.Sprintf("%04d-W%02d", w.year, w.week)
}

// FirstDate returns Monday of w.
func (w ISOWeek) FirstDate() Date {
	if !w.IsValid() {
		return Date{}
	}
	jan4 := MustDate(w.Year(), time.January, 4)
	first, _ := jan4.StartOfISOWeek().AddWeeks(w.Week() - 1)
	return first
}

// LastDate returns Sunday of w.
func (w ISOWeek) LastDate() Date {
	last, _ := w.FirstDate().AddDays(6)
	return last
}

// Contains reports whether d belongs to w.
func (w ISOWeek) Contains(d Date) bool {
	year, week := d.ISOWeek()
	return w.IsValid() && year == w.Year() && week == w.Week()
}

// AddWeeks navigates by whole ISO weeks.
func (w ISOWeek) AddWeeks(weeks int) (ISOWeek, error) {
	if !w.IsValid() {
		return ISOWeek{}, ErrInvalidDate
	}
	date, err := w.FirstDate().AddWeeks(weeks)
	if err != nil {
		return ISOWeek{}, err
	}
	year, week := date.ISOWeek()
	return NewISOWeek(year, week)
}

// Compare returns -1, 0, or 1 according to first-date ordering.
func (w ISOWeek) Compare(other ISOWeek) (int, error) {
	if !w.IsValid() || !other.IsValid() {
		return 0, ErrInvalidDate
	}
	return w.FirstDate().Compare(other.FirstDate())
}

func asciiDigits(input string, length int) bool {
	if len(input) != length {
		return false
	}
	for i := range len(input) {
		if input[i] < '0' || input[i] > '9' {
			return false
		}
	}
	return true
}

func isoWeeksInYear(year int) int {
	_, week := time.Date(year, time.December, 28, 0, 0, 0, 0, time.UTC).ISOWeek()
	return week
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func compareInts(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
