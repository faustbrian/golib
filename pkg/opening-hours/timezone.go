package openinghours

import (
	"errors"
	"time"

	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

// LocalResolutionPolicy explicitly resolves DST gaps and folds.
type LocalResolutionPolicy uint8

const (
	// RejectDST rejects both nonexistent and ambiguous local times.
	RejectDST LocalResolutionPolicy = iota
	// PreferEarlier selects the earlier instant in a local-time fold.
	PreferEarlier
	// PreferLater selects the later instant in a local-time fold.
	PreferLater
	// ShiftForward advances a nonexistent local time through a timezone gap.
	ShiftForward
)

// LocalKind classifies a local-to-instant conversion.
type LocalKind uint8

const (
	// LocalExact identifies a local time with exactly one corresponding instant.
	LocalExact LocalKind = iota
	// LocalGap identifies a local time shifted through a timezone gap.
	LocalGap
	// LocalFold identifies a local time selected from a timezone fold.
	LocalFold
)

// ResolvedLocal records the instant and DST classification selected by policy.
type ResolvedLocal struct {
	Instant time.Time
	Kind    LocalKind
}

// IsOpenLocal resolves a civil date and wall-clock time under the explicit DST
// policy, then evaluates the selected instant in the schedule timezone.
func (s Schedule) IsOpenLocal(date Date, localTime LocalTime, policy LocalResolutionPolicy) (Availability, error) {
	if !validDate(date) {
		return Availability{}, newError("is open local", CodeInvalidDate)
	}
	if !localTime.valid() {
		return Availability{}, newError("is open local", CodeInvalidTime)
	}
	if policy > ShiftForward {
		return Availability{}, newError("is open local", CodeInvalidState)
	}
	if s.data == nil {
		return Availability{Explanation: Explanation{Rule: RuleNone}}, nil
	}
	resolved, err := s.ResolveLocal(date, localTime, policy)
	if err != nil {
		return Availability{}, err
	}

	return s.IsOpen(resolved.Instant)
}

// ResolveLocal converts a civil date and wall-clock time in the schedule zone.
// Gaps and folds are never resolved without the caller-selected policy.
func (s Schedule) ResolveLocal(date Date, localTime LocalTime, policy LocalResolutionPolicy) (ResolvedLocal, error) {
	if !validDate(date) {
		return ResolvedLocal{}, newError("resolve local", CodeInvalidDate)
	}
	if !localTime.valid() {
		return ResolvedLocal{}, newError("resolve local", CodeInvalidTime)
	}
	if s.data == nil {
		return ResolvedLocal{}, newError("resolve local", CodeInvalidTimezone)
	}
	if policy > ShiftForward {
		return ResolvedLocal{}, newError("resolve local", CodeInvalidState)
	}
	local, _ := calendartz.NewLocalDateTime(
		date, localTime.Hour(), localTime.Minute(), localTime.Second(), localTime.Nanosecond(),
	)
	instant, err := calendartz.Resolve(local, s.data.location, calendartz.Reject)
	if err == nil {
		return ResolvedLocal{Instant: instant, Kind: LocalExact}, nil
	}
	if errors.Is(err, calendartz.ErrAmbiguous) {
		var resolution calendartz.Resolution
		switch policy {
		case PreferEarlier:
			resolution = calendartz.Earlier
		case PreferLater:
			resolution = calendartz.Later
		case RejectDST, ShiftForward:
			return ResolvedLocal{}, newError("resolve local", CodeAmbiguousLocalTime)
		}
		instant, _ = calendartz.Resolve(local, s.data.location, resolution)

		return ResolvedLocal{Instant: instant, Kind: LocalFold}, nil
	}
	if policy == ShiftForward {
		shifted := shiftForwardGap(s.data.location, date, localTime)
		if !shifted.IsZero() {
			return ResolvedLocal{Instant: shifted, Kind: LocalGap}, nil
		}
	}

	return ResolvedLocal{}, newError("resolve local", CodeNonexistentLocalTime)
}

func shiftForwardGap(location *time.Location, date Date, localTime LocalTime) time.Time {
	wall := time.Date(
		date.Year(), date.Month(), date.Day(),
		localTime.Hour(), localTime.Minute(), localTime.Second(), localTime.Nanosecond(),
		time.UTC,
	)
	offsets := make(map[int]struct{}, 4)
	for offset := -48 * time.Hour; offset <= 48*time.Hour; offset += 30 * time.Minute {
		_, seconds := wall.Add(offset).In(location).Zone()
		offsets[seconds] = struct{}{}
	}

	var shifted time.Time
	var shiftedDelta time.Duration
	for seconds := range offsets {
		candidate := wall.Add(-time.Duration(seconds) * time.Second)
		localized := candidate.In(location)
		localizedWall := time.Date(
			localized.Year(), localized.Month(), localized.Day(), localized.Hour(),
			localized.Minute(), localized.Second(), localized.Nanosecond(), time.UTC,
		)
		delta := localizedWall.Sub(wall)
		if delta > 0 && (shifted.IsZero() || delta < shiftedDelta ||
			delta == shiftedDelta && candidate.Before(shifted)) {
			shifted = candidate
			shiftedDelta = delta
		}
	}

	return shifted
}

// IsOpen evaluates an absolute instant in the schedule's explicit timezone.
func (s Schedule) IsOpen(instant time.Time) (Availability, error) {
	if s.data == nil {
		return Availability{Explanation: Explanation{Rule: RuleNone}}, nil
	}
	localized := instant.In(s.data.location)
	date, err := NewDate(localized.Year(), localized.Month(), localized.Day())
	if err != nil {
		return Availability{}, err
	}
	// Wall-clock components produced by time.Time always satisfy LocalTime.
	localTime, _ := NewLocalTime(
		localized.Hour(), localized.Minute(), localized.Second(), localized.Nanosecond(),
	)

	return s.isOpenCivil(date, localTime)
}
