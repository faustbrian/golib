// Package scheduler defines code-based schedules and distributed execution.
package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"

	schedulercron "github.com/faustbrian/golib/pkg/scheduler/cron"
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
			continue
		}
		parsed, err := schedulercron.Compile(schedule.Expression, schedule.Timezone)
		if err != nil {
			classification := ErrInvalidExpression
			if errors.Is(err, schedulercron.ErrInvalidTimezone) {
				classification = ErrInvalidTimezone
			}
			errs = append(errs, fmt.Errorf("%w: %s: %w", classification, schedule.Name, err))
			continue
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
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	slices.Sort(registry.names)
	return registry, nil
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
		candidate := entry.cron.Next(through.Add(-time.Minute))
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
				break
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
