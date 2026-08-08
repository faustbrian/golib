// Package scheduler defines code-based schedules and distributed execution.
package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"

	schedulercron "github.com/faustbrian/golib/pkg/scheduler/cron"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

var (
	// ErrInvalidExpression reports a schedule with invalid cron syntax.
	ErrInvalidExpression = errors.New("scheduler: invalid cron expression")
	// ErrInvalidTimezone reports a schedule with an unavailable IANA zone.
	ErrInvalidTimezone = errors.New("scheduler: invalid timezone")
	// ErrDuplicateSchedule reports repeated schedule names at compilation.
	ErrDuplicateSchedule = errors.New("scheduler: duplicate schedule")
	// ErrScheduleNotFound reports an unknown registry schedule name.
	ErrScheduleNotFound = errors.New("scheduler: schedule not found")
	// ErrOccurrenceLimit reports a due scan beyond its global candidate cap.
	ErrOccurrenceLimit = errors.New("scheduler: occurrence scan limit exceeded")
)

// MaxOccurrenceScan bounds candidates inspected by one Due call.
const MaxOccurrenceScan = 10_000

// Occurrence is one deterministic physical schedule boundary.
type Occurrence struct {
	ScheduleID     string    `json:"schedule_id"`
	ScheduleName   string    `json:"schedule_name"`
	Task           string    `json:"task"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	Attempt        int       `json:"attempt"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type compiledSchedule struct {
	schedule Schedule
	cron     schedulercron.Schedule
}

// Registry is an immutable compiled set of named schedules.
type Registry struct {
	entries map[string]compiledSchedule
	names   []string
}

// ScheduleOverview combines an immutable schedule definition with its next
// enabled execution boundary. Next is zero for a disabled schedule.
type ScheduleOverview struct {
	Schedule Schedule
	Next     time.Time
}

// Compile validates and freezes a complete named schedule set.
func Compile(schedules ...Schedule) (*Registry, error) {
	if len(schedules) > MaxSchedules {
		return nil, fmt.Errorf("%w: registry exceeds %d schedules", ErrResourceLimit, MaxSchedules)
	}
	registry := &Registry{entries: make(map[string]compiledSchedule, len(schedules))}
	var errs []error
	for _, schedule := range schedules {
		if _, exists := registry.entries[schedule.Name]; exists {
			errs = append(errs, fmt.Errorf("%w: %s", ErrDuplicateSchedule, schedule.Name))
		} else {
			parsed, err := schedulercron.Compile(schedule.Expression, schedule.Timezone)
			if err != nil {
				classification := ErrInvalidExpression
				if errors.Is(err, schedulercron.ErrInvalidTimezone) {
					classification = ErrInvalidTimezone
				}
				errs = append(errs, fmt.Errorf("%w: %s: %w", classification, schedule.Name, err))
			} else {
				if hasRecurringConstraints(schedule) {
					// The cron compiler loaded this exact zone successfully above.
					location, _ := time.LoadLocation(schedule.Timezone)
					parsed = constrainedSchedule{
						Schedule: parsed,
						days:     slices.Clone(schedule.DaysOfWeek),
						windows:  slices.Clone(schedule.TimeWindows),
						location: location,
					}
				}
				registry.entries[schedule.Name] = compiledSchedule{
					schedule: cloneSchedule(schedule),
					cron: jitteredSchedule{
						Schedule: parsed,
						offset:   deterministicJitter(schedule.Identity, schedule.Jitter),
					},
				}
				registry.names = append(registry.names, schedule.Name)
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	slices.Sort(registry.names)
	return registry, nil
}

type constrainedSchedule struct {
	schedulercron.Schedule
	days     []time.Weekday
	windows  []TimeWindow
	location *time.Location
}

func (schedule constrainedSchedule) Next(after time.Time) time.Time {
	cursor := after
	for range 200_000 {
		next := schedule.Schedule.Next(cursor)
		if next.IsZero() {
			return time.Time{}
		}
		local := next.In(schedule.location)
		if len(schedule.days) > 0 && !slices.Contains(schedule.days, local.Weekday()) {
			nextDay := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, schedule.location)
			cursor = nextDay.Add(-time.Nanosecond)
			continue
		}
		if schedule.withinTimeWindows(local) {
			return next
		}
		cursor = local.Truncate(time.Minute).Add(time.Minute - time.Nanosecond)
	}
	return time.Time{}
}

func (schedule constrainedSchedule) withinTimeWindows(local time.Time) bool {
	minute := time.Duration(local.Hour()*60+local.Minute()) * time.Minute
	for _, window := range schedule.windows {
		inside := withinTimeWindow(minute, window)
		if (!window.Excluded && !inside) || (window.Excluded && inside) {
			return false
		}
	}
	return true
}

func withinTimeWindow(value time.Duration, window TimeWindow) bool {
	if window.Start <= window.End {
		return value >= window.Start && value <= window.End
	}
	return value >= window.Start || value <= window.End
}

type jitteredSchedule struct {
	schedulercron.Schedule
	offset time.Duration
}

func (schedule jitteredSchedule) Next(after time.Time) time.Time {
	return schedule.Schedule.Next(after.Add(-schedule.offset)).Add(schedule.offset)
}

func deterministicJitter(identity string, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte(identity))
	value := binary.BigEndian.Uint64(digest[:8]) % uint64(maximum)
	// #nosec G115 -- modulo by a positive time.Duration bounds value to MaxInt64.
	return time.Duration(value)
}

// Schedules returns immutable copies sorted by schedule name.
func (registry *Registry) Schedules() []Schedule {
	result := make([]Schedule, 0, len(registry.names))
	for _, name := range registry.names {
		result = append(result, cloneSchedule(registry.entries[name].schedule))
	}
	return result
}

// Overview returns immutable schedule definitions and their next boundaries,
// sorted by schedule name. Callers supply the reference instant so inspection
// remains deterministic and can be exposed through any application surface.
func (registry *Registry) Overview(after time.Time) []ScheduleOverview {
	result := make([]ScheduleOverview, 0, len(registry.names))
	for _, name := range registry.names {
		entry := registry.entries[name]
		overview := ScheduleOverview{Schedule: cloneSchedule(entry.schedule)}
		if entry.schedule.Enabled {
			overview.Next = entry.cron.Next(after)
		}
		result = append(result, overview)
	}
	return result
}

// ClearCache removes every currently observed task lease for schedules using
// overlap prevention. Each removal is fenced by the token returned by Inspect.
// Callers must first isolate active owners because lease removal cannot stop
// unfenced side effects already in progress.
func (registry *Registry) ClearCache(ctx context.Context, store lease.Store) (int, error) {
	if store == nil {
		return 0, lease.ErrInvalid
	}
	cleared := 0
	var errs []error
	for _, name := range registry.names {
		if err := ctx.Err(); err != nil {
			return cleared, errors.Join(append(errs, err)...)
		}
		schedule := registry.entries[name].schedule
		if !schedule.WithoutOverlapping {
			continue
		}
		key := taskLeaseKey(schedule)
		owned, err := store.Inspect(ctx, key)
		if errors.Is(err, lease.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("scheduler: inspect overlap lease %q: %w", name, err))
			continue
		}
		if err := store.Recover(ctx, key, owned.FencingToken); err != nil {
			errs = append(errs, fmt.Errorf("scheduler: clear overlap lease %q: %w", name, err))
			continue
		}
		cleared++
	}
	return cleared, errors.Join(errs...)
}

func taskLeaseKey(schedule Schedule) string { return "task:" + schedule.CoordinationID }

// Next returns the first occurrence strictly after an instant.
func (registry *Registry) Next(name string, after time.Time) (time.Time, error) {
	entry, ok := registry.entries[name]
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s", ErrScheduleNotFound, name)
	}
	return entry.cron.Next(after), nil
}

// Due applies the schedule's bounded missed-run policy to an instant range.
func (registry *Registry) Due(name string, after, through time.Time) ([]Occurrence, error) {
	entry, ok := registry.entries[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrScheduleNotFound, name)
	}
	if !through.After(after) || !entry.schedule.Enabled {
		return nil, nil
	}

	limit := entry.schedule.MaxCatchUp
	switch entry.schedule.MissedRunPolicy {
	case MissedRunSkip:
		candidate := entry.cron.Next(through.Add(-time.Nanosecond))
		if !candidate.Equal(through) || !withinBounds(entry.schedule, candidate) {
			return nil, nil
		}
		return []Occurrence{newOccurrence(entry.schedule, candidate)}, nil
	case MissedRunOnce:
		limit = 1
	case MissedRunCatchUp:
	default:
		return nil, ErrInvalidMissedRuns
	}

	occurrences := make([]Occurrence, 0, limit)
	for scanned, next := 0, entry.cron.Next(after); !next.After(through); scanned, next = scanned+1, entry.cron.Next(next) {
		if scanned == MaxOccurrenceScan {
			return nil, ErrOccurrenceLimit
		}
		if !withinBounds(entry.schedule, next) {
			if !entry.schedule.EndAt.IsZero() && next.After(entry.schedule.EndAt) {
				return occurrences, nil
			}
			continue
		}
		occurrence := newOccurrence(entry.schedule, next)
		if entry.schedule.MissedRunPolicy == MissedRunOnce {
			if len(occurrences) == 0 {
				occurrences = append(occurrences, occurrence)
			} else {
				occurrences[0] = occurrence
			}
			continue
		}
		if len(occurrences) == limit {
			copy(occurrences, occurrences[1:])
			occurrences[len(occurrences)-1] = occurrence
		} else {
			occurrences = append(occurrences, occurrence)
		}
	}
	return occurrences, nil
}

func withinBounds(schedule Schedule, occurrence time.Time) bool {
	return (schedule.StartAt.IsZero() || !occurrence.Before(schedule.StartAt)) &&
		(schedule.EndAt.IsZero() || !occurrence.After(schedule.EndAt))
}

func newOccurrence(schedule Schedule, at time.Time) Occurrence {
	key := hashStrings(schedule.CoordinationID, at.UTC().Format(time.RFC3339Nano))
	return Occurrence{
		ScheduleID:     schedule.Identity,
		ScheduleName:   schedule.Name,
		Task:           schedule.Task,
		ScheduledAt:    at,
		Attempt:        1,
		IdempotencyKey: key,
	}
}
