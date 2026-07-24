// Package timezone converts between civil local values and instants using
// explicit IANA locations and deterministic gap/fold policies.
package timezone

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

var (
	// ErrInvalidLocalTime identifies invalid date or time-of-day components.
	ErrInvalidLocalTime = errors.New("calendar/timezone: invalid local time")
	// ErrInvalidLocation identifies a nil location.
	ErrInvalidLocation = errors.New("calendar/timezone: explicit location required")
	// ErrNonexistent identifies a local value in a timezone gap.
	ErrNonexistent = errors.New("calendar/timezone: nonexistent local time")
	// ErrAmbiguous identifies a local value in a timezone fold.
	ErrAmbiguous = errors.New("calendar/timezone: ambiguous local time")
	// ErrOffsetMismatch identifies an explicit offset with no matching occurrence.
	ErrOffsetMismatch = errors.New("calendar/timezone: offset does not match")
	// ErrInvalidZone identifies an invalid, missing, or oversized IANA name.
	ErrInvalidZone = errors.New("calendar/timezone: invalid IANA zone")
)

// MaxZoneNameBytes bounds IANA timezone identifiers before database lookup.
const MaxZoneNameBytes = 255

// Resolution is a sealed local-time resolution policy.
type Resolution interface{ resolution() }

// Choice selects behavior for ordinary, ambiguous, and nonexistent local time.
type Choice uint8

const (
	// Reject rejects gaps and folds.
	Reject Choice = iota + 1
	// Earlier selects the chronologically earlier occurrence of a fold.
	Earlier
	// Later selects the chronologically later occurrence of a fold.
	Later
)

func (Choice) resolution() {}

type offsetMatch struct{ seconds int }

func (offsetMatch) resolution() {}

// MatchOffset selects the fold occurrence with offsetSeconds east of UTC.
func MatchOffset(offsetSeconds int) Resolution { return offsetMatch{seconds: offsetSeconds} }

// LoadLocation validates a bounded IANA name before delegating transition
// calculation to the standard library's authoritative timezone loader.
func LoadLocation(name string) (*time.Location, error) {
	if name == "" || len(name) > MaxZoneNameBytes || !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return nil, ErrInvalidZone
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, ErrInvalidZone
		}
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrInvalidZone
	}
	return location, nil
}

// LocalDateTime is an immutable civil date plus local time-of-day. It contains
// neither a timezone nor an offset.
type LocalDateTime struct {
	date       calendar.Date
	hour       uint8
	minute     uint8
	second     uint8
	nanosecond uint32
}

// NewLocalDateTime validates and constructs a LocalDateTime.
func NewLocalDateTime(date calendar.Date, hour, minute, second, nanosecond int) (LocalDateTime, error) {
	if !date.IsValid() || hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 || nanosecond < 0 || nanosecond >= int(time.Second) {
		return LocalDateTime{}, ErrInvalidLocalTime
	}
	return LocalDateTime{date: date, hour: uint8(hour), minute: uint8(minute), second: uint8(second), nanosecond: uint32(nanosecond)}, nil
}

// MustLocalDateTime is NewLocalDateTime but panics for invalid components.
func MustLocalDateTime(date calendar.Date, hour, minute, second, nanosecond int) LocalDateTime {
	local, err := NewLocalDateTime(date, hour, minute, second, nanosecond)
	if err != nil {
		panic(err)
	}
	return local
}

// Date returns the civil date.
func (l LocalDateTime) Date() calendar.Date { return l.date }

// Hour returns the hour from 0 through 23.
func (l LocalDateTime) Hour() int { return int(l.hour) }

// Minute returns the minute from 0 through 59.
func (l LocalDateTime) Minute() int { return int(l.minute) }

// Second returns the second from 0 through 59.
func (l LocalDateTime) Second() int { return int(l.second) }

// Nanosecond returns the nanosecond from 0 through 999,999,999.
func (l LocalDateTime) Nanosecond() int { return int(l.nanosecond) }

// IsValid reports whether l contains valid components.
func (l LocalDateTime) IsValid() bool {
	_, err := NewLocalDateTime(l.date, l.Hour(), l.Minute(), l.Second(), l.Nanosecond())
	return err == nil
}

// Resolve maps local to an instant in loc according to policy. It enumerates
// verified occurrences instead of accepting time.Date's implicit gap/fold
// choice. Work is bounded to 145 location lookups and candidate checks.
func Resolve(local LocalDateTime, loc *time.Location, policy Resolution) (time.Time, error) {
	if !local.IsValid() {
		return time.Time{}, ErrInvalidLocalTime
	}
	if loc == nil {
		return time.Time{}, ErrInvalidLocation
	}
	if policy == nil {
		return time.Time{}, fmt.Errorf("%w: nil resolution policy", ErrInvalidLocalTime)
	}
	policy.resolution()
	candidates := occurrences(local, loc)
	if len(candidates) == 0 {
		return time.Time{}, ErrNonexistent
	}
	if match, ok := policy.(offsetMatch); ok {
		for _, candidate := range candidates {
			_, offset := candidate.Zone()
			if offset == match.seconds {
				return candidate, nil
			}
		}
		return time.Time{}, ErrOffsetMismatch
	}
	choice, ok := policy.(Choice)
	if !ok || choice < Reject || choice > Later {
		return time.Time{}, fmt.Errorf("%w: unknown resolution policy", ErrInvalidLocalTime)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if choice == Earlier {
		return candidates[0], nil
	}
	if choice == Later {
		return candidates[len(candidates)-1], nil
	}
	return time.Time{}, ErrAmbiguous
}

// FromInstant converts instant to civil local components in loc.
func FromInstant(instant time.Time, loc *time.Location) (LocalDateTime, error) {
	if loc == nil {
		return LocalDateTime{}, ErrInvalidLocation
	}
	observed := instant.In(loc)
	date, err := calendar.NewDate(observed.Date())
	if err != nil {
		return LocalDateTime{}, err
	}
	return NewLocalDateTime(date, observed.Hour(), observed.Minute(), observed.Second(), observed.Nanosecond())
}

// DayRange returns the half-open instant range [start, next-day-start) for a
// civil date. It never fabricates an end-of-day local value.
func DayRange(date calendar.Date, loc *time.Location, policy Resolution) (start, end time.Time, err error) {
	if !date.IsValid() {
		return time.Time{}, time.Time{}, calendar.ErrInvalidDate
	}
	next, err := date.AddDays(1)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err = Resolve(MustLocalDateTime(date, 0, 0, 0, 0), loc, policy)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err = Resolve(MustLocalDateTime(next, 0, 0, 0, 0), loc, policy)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

func occurrences(local LocalDateTime, loc *time.Location) []time.Time {
	wall := time.Date(local.Date().Year(), local.Date().Month(), local.Date().Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
	offsets := make(map[int]struct{}, 4)
	for hour := -72; hour <= 72; hour++ {
		_, offset := wall.Add(time.Duration(hour) * time.Hour).In(loc).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, len(offsets))
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second).In(loc)
		if !sameWall(candidate, local) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	return candidates
}

func sameWall(instant time.Time, local LocalDateTime) bool {
	year, month, day := instant.Date()
	return year == local.Date().Year() && month == local.Date().Month() && day == local.Date().Day() &&
		instant.Hour() == local.Hour() && instant.Minute() == local.Minute() && instant.Second() == local.Second() && instant.Nanosecond() == local.Nanosecond()
}
