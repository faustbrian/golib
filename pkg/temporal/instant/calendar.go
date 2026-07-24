package instant

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// CivilUnit identifies a calendar boundary used for explicit snapping.
type CivilUnit uint8

const (
	Second CivilUnit = iota + 1
	Minute
	Hour
	Day
	ISOWeek
	Month
	Quarter
	Semester
	Year
	ISOYear
)

// SnapDirection selects the boundary at or before/after an instant.
type SnapDirection uint8

const (
	Floor SnapDirection = iota + 1
	Ceil
)

// Snap maps value to a civil boundary in location. DST gaps and folds are
// resolved only through the explicit calendar policy.
func Snap(value time.Time, unit CivilUnit, direction SnapDirection, location *time.Location, policy calendartz.Resolution) (time.Time, error) {
	if direction != Floor && direction != Ceil {
		return time.Time{}, temporal.ErrUnsupported
	}
	boundary, exact, err := civilFloor(value, unit, location, policy)
	if err != nil {
		return time.Time{}, err
	}
	if direction == Floor || exact {
		return boundary, nil
	}
	return nextCivilBoundary(boundary, unit, location, policy)
}

// SnapOutward returns the smallest canonical half-open civil-boundary range
// that contains every member of p.
func (p Period) SnapOutward(unit CivilUnit, location *time.Location, policy calendartz.Resolution) (Period, error) {
	if p.IsEmpty() {
		return Period{}, temporal.ErrEmpty
	}
	start, err := Snap(p.start, unit, Floor, location, policy)
	if err != nil {
		return Period{}, err
	}
	end, err := Snap(p.end, unit, Ceil, location, policy)
	if err != nil {
		return Period{}, err
	}
	if p.bounds.IncludesEnd() && end.Equal(p.end) {
		end, err = nextCivilBoundary(end, unit, location, policy)
		if err != nil {
			return Period{}, err
		}
	}
	return Range(start, end)
}

func civilFloor(value time.Time, unit CivilUnit, location *time.Location, policy calendartz.Resolution) (time.Time, bool, error) {
	local, err := calendartz.FromInstant(value, location)
	if err != nil {
		return time.Time{}, false, err
	}
	date := local.Date()
	hour, minute, second := local.Hour(), local.Minute(), local.Second()
	nanosecond := 0
	switch unit {
	case Second:
		nanosecond = 0
	case Minute:
		second, nanosecond = 0, 0
	case Hour:
		minute, second, nanosecond = 0, 0, 0
	case Day:
		hour, minute, second, nanosecond = 0, 0, 0, 0
	case ISOWeek:
		date, hour, minute, second, nanosecond = date.StartOfISOWeek(), 0, 0, 0, 0
	case Month:
		date, hour, minute, second, nanosecond = date.StartOfMonth(), 0, 0, 0, 0
	case Quarter:
		date, hour, minute, second, nanosecond = date.StartOfQuarter(), 0, 0, 0, 0
	case Semester:
		date, hour, minute, second, nanosecond = date.StartOfSemester(), 0, 0, 0, 0
	case Year:
		date, hour, minute, second, nanosecond = date.StartOfYear(), 0, 0, 0, 0
	case ISOYear:
		date, hour, minute, second, nanosecond = date.StartOfISOYear(), 0, 0, 0, 0
	default:
		return time.Time{}, false, temporal.ErrUnsupported
	}
	boundary, err := resolveCivil(date, hour, minute, second, nanosecond, location, policy)
	if err != nil {
		return time.Time{}, false, err
	}
	return boundary, value.Equal(boundary), nil
}

func nextCivilBoundary(value time.Time, unit CivilUnit, location *time.Location, policy calendartz.Resolution) (time.Time, error) {
	local, err := calendartz.FromInstant(value, location)
	if err != nil {
		return time.Time{}, err
	}
	date := local.Date()
	hour, minute, second, nanosecond := local.Hour(), local.Minute(), local.Second(), local.Nanosecond()
	switch unit {
	case Second, Minute, Hour:
		increments := [...]time.Duration{
			Second: time.Second,
			Minute: time.Minute,
			Hour:   time.Hour,
		}
		increment := increments[unit]
		wall := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, second, nanosecond, time.UTC).Add(increment)
		date, err = calendar.NewDate(wall.Date())
		if err != nil {
			return time.Time{}, err
		}
		hour, minute, second, nanosecond = wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond()
	case Day:
		date, err = date.AddDays(1)
	case ISOWeek:
		date, err = date.AddWeeks(1)
	case Month:
		date, err = date.AddMonths(1, calendar.Reject)
	case Quarter:
		date, err = date.AddQuarters(1, calendar.Reject)
	case Semester:
		date, err = date.AddSemesters(1, calendar.Reject)
	case Year:
		date, err = date.AddYears(1, calendar.Reject)
	case ISOYear:
		isoYear, _ := date.ISOWeek()
		if isoYear >= calendar.MaxYear {
			return time.Time{}, calendar.ErrArithmetic
		}
		week, _ := calendar.NewISOWeek(isoYear+1, 1) // validated year always has week 1
		date = week.FirstDate()
	default:
		return time.Time{}, temporal.ErrUnsupported
	}
	if err != nil {
		return time.Time{}, err
	}
	return resolveCivil(date, hour, minute, second, nanosecond, location, policy)
}

func resolveCivil(date calendar.Date, hour, minute, second, nanosecond int, location *time.Location, policy calendartz.Resolution) (time.Time, error) {
	local, err := calendartz.NewLocalDateTime(date, hour, minute, second, nanosecond)
	if err != nil {
		return time.Time{}, err
	}
	resolved, err := calendartz.Resolve(local, location, policy)
	if err != nil {
		return time.Time{}, fmt.Errorf("snap civil boundary: %w", err)
	}
	return stripMonotonic(resolved), nil
}
