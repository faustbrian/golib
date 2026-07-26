package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

var (
	// ErrScheduleNameRequired reports a blank schedule name.
	ErrScheduleNameRequired = errors.New("scheduler: schedule name is required")
	// ErrTaskNameRequired reports a blank task identity.
	ErrTaskNameRequired = errors.New("scheduler: task name is required")
	// ErrInvalidMissedRuns reports an invalid missed-run policy or bound.
	ErrInvalidMissedRuns = errors.New("scheduler: invalid missed-run policy")
	// ErrInvalidDateBounds reports an end instant before its start instant.
	ErrInvalidDateBounds = errors.New("scheduler: invalid date bounds")
	// ErrInvalidDuration reports a non-positive duration option.
	ErrInvalidDuration = errors.New("scheduler: duration must be positive")
	// ErrInvalidVersion reports a blank schedule version.
	ErrInvalidVersion = errors.New("scheduler: version is required")
	// ErrResourceLimit reports a definition beyond an exported safety budget.
	ErrResourceLimit = errors.New("scheduler: resource limit exceeded")
)

const (
	// MaxJitter is the largest allowed deterministic schedule offset.
	MaxJitter = 24 * time.Hour
	// MaxIdentityBytes bounds schedule identity component strings.
	MaxIdentityBytes = 255
	// MaxExpressionBytes bounds a cron expression before parser invocation.
	MaxExpressionBytes = 1_024
	// MaxParameterBytes bounds the JSON-encoded task parameter payload.
	MaxParameterBytes = 64 << 10
	// MaxMetadataEntries bounds diagnostic metadata cardinality.
	MaxMetadataEntries = 128
	// MaxMetadataBytes bounds combined metadata key and value bytes.
	MaxMetadataBytes = 64 << 10
	// MaxEnvironments bounds environment filters per schedule.
	MaxEnvironments = 64
	// MaxConditions bounds trusted application conditions per schedule.
	MaxConditions = 32
	// MaxCatchUp bounds retained delayed occurrences per decision.
	MaxCatchUp = 1_000
	// MaxSchedules bounds registry size before compilation.
	MaxSchedules = 10_000
)

// MissedRunPolicy controls decisions after one or more delayed boundaries.
type MissedRunPolicy uint8

const (
	// MissedRunSkip executes only a boundary observed exactly on time.
	MissedRunSkip MissedRunPolicy = iota
	// MissedRunOnce executes only the newest missed boundary.
	MissedRunOnce
	// MissedRunCatchUp executes a bounded tail of missed boundaries.
	MissedRunCatchUp
)

// OverlapPolicy controls a decision when the task lease is already held.
type OverlapPolicy uint8

const (
	// OverlapAllow permits concurrent execution and does not acquire a task lease.
	OverlapAllow OverlapPolicy = iota
	// OverlapSkip skips an occurrence while its task lease is held.
	OverlapSkip
	// OverlapReplace delegates safe cancellation and transfer to a replacement store.
	OverlapReplace
)

// MaintenancePolicy controls execution while application maintenance is active.
type MaintenancePolicy uint8

const (
	// MaintenanceSkip suppresses execution during maintenance.
	MaintenanceSkip MaintenancePolicy = iota
	// MaintenanceRun allows execution during maintenance.
	MaintenanceRun
)

// Interval contains the explicit cron expression used by a schedule.
type Interval struct {
	expression string
}

// Cron constructs an interval from a five-field expression or descriptor.
func Cron(expression string) Interval { return Interval{expression: expression} }

// EveryMinute returns the explicit every-minute cron interval.
func EveryMinute() Interval { return Cron("* * * * *") }

// Hourly returns the explicit hourly cron interval.
func Hourly() Interval { return Cron("0 * * * *") }

// Daily returns the explicit daily cron interval.
func Daily() Interval { return Cron("0 0 * * *") }

// Weekly returns the explicit Sunday-at-midnight cron interval.
func Weekly() Interval { return Cron("0 0 * * 0") }

// Monthly returns the explicit first-day-at-midnight cron interval.
func Monthly() Interval { return Cron("0 0 1 * *") }

// Expression returns the exact cron expression represented by the interval.
func (i Interval) Expression() string { return i.expression }

// Condition allows trusted application code to permit an occurrence.
type Condition func(Context) (bool, error)

// Hook consumes a schedule lifecycle event.
type Hook func(Event)

// Hooks configures optional callbacks for each lifecycle boundary.
type Hooks struct {
	Before    Hook
	Success   Hook
	Failure   Hook
	Skipped   Hook
	Overlap   Hook
	Completed Hook
}

// Context carries immutable schedule and ownership data to an Executor.
type Context struct {
	Schedule       Schedule
	Now            time.Time
	Due            time.Time
	Attempt        int
	Owner          string
	Fencing        uint64
	IdempotencyKey string
	Metadata       map[string]string
}

// Schedule is an immutable code-defined task timing and policy definition.
type Schedule struct {
	Name               string
	Version            string
	Task               string
	Expression         string
	Timezone           string
	Identity           string
	CoordinationID     string
	ParameterIdentity  string
	Parameters         map[string]any
	Enabled            bool
	Environments       []string
	MaintenancePolicy  MaintenancePolicy
	Conditions         []Condition
	StartAt            time.Time
	EndAt              time.Time
	Jitter             time.Duration
	Metadata           map[string]string
	MissedRunPolicy    MissedRunPolicy
	MaxCatchUp         int
	OverlapPolicy      OverlapPolicy
	OnOneServer        bool
	WithoutOverlapping bool
	LeaseTTL           time.Duration
	RunTimeout         time.Duration
	Hooks              Hooks
}

// Option configures a schedule before final validation and identity hashing.
type Option func(*Schedule) error

// NewSchedule constructs, validates, bounds, and identifies a schedule.
func NewSchedule(name, task string, interval Interval, options ...Option) (Schedule, error) {
	if strings.TrimSpace(name) == "" {
		return Schedule{}, ErrScheduleNameRequired
	}
	if strings.TrimSpace(task) == "" {
		return Schedule{}, ErrTaskNameRequired
	}

	schedule := Schedule{
		Name:            name,
		Version:         "1",
		Task:            task,
		Expression:      interval.Expression(),
		Timezone:        "UTC",
		Enabled:         true,
		MissedRunPolicy: MissedRunSkip,
		OverlapPolicy:   OverlapAllow,
		Metadata:        map[string]string{},
		Parameters:      map[string]any{},
		LeaseTTL:        time.Minute,
		RunTimeout:      time.Minute,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&schedule); err != nil {
			return Schedule{}, err
		}
	}
	if !schedule.EndAt.IsZero() && !schedule.StartAt.IsZero() && schedule.EndAt.Before(schedule.StartAt) {
		return Schedule{}, ErrInvalidDateBounds
	}
	if schedule.MissedRunPolicy == MissedRunCatchUp && schedule.MaxCatchUp < 1 {
		return Schedule{}, ErrInvalidMissedRuns
	}
	if err := validateResourceLimits(schedule); err != nil {
		return Schedule{}, err
	}

	schedule.ParameterIdentity = stableHash(schedule.Parameters)
	schedule.CoordinationID = hashStrings(
		schedule.Name,
		schedule.Task,
		schedule.ParameterIdentity,
	)
	schedule.Identity = hashStrings(
		schedule.CoordinationID,
		schedule.Version,
		schedule.Expression,
		schedule.Timezone,
		schedule.Jitter.String(),
	)
	return cloneSchedule(schedule), nil
}

func validateResourceLimits(schedule Schedule) error {
	stringsToValidate := []struct {
		name  string
		value string
		limit int
	}{
		{name: "name", value: schedule.Name, limit: MaxIdentityBytes},
		{name: "version", value: schedule.Version, limit: MaxIdentityBytes},
		{name: "task", value: schedule.Task, limit: MaxIdentityBytes},
		{name: "expression", value: schedule.Expression, limit: MaxExpressionBytes},
		{name: "timezone", value: schedule.Timezone, limit: MaxIdentityBytes},
	}
	for _, field := range stringsToValidate {
		if len(field.value) > field.limit {
			return fmt.Errorf("%w: %s exceeds %d bytes", ErrResourceLimit, field.name, field.limit)
		}
	}
	parameters, err := json.Marshal(schedule.Parameters)
	if err != nil {
		return fmt.Errorf("scheduler: encode parameters: %w", err)
	}
	if len(parameters) > MaxParameterBytes {
		return fmt.Errorf("%w: parameters exceed %d bytes", ErrResourceLimit, MaxParameterBytes)
	}
	if len(schedule.Metadata) > MaxMetadataEntries {
		return fmt.Errorf("%w: metadata exceeds %d entries", ErrResourceLimit, MaxMetadataEntries)
	}
	metadataBytes := 0
	for key, value := range schedule.Metadata {
		metadataBytes += len(key) + len(value)
	}
	if metadataBytes > MaxMetadataBytes {
		return fmt.Errorf("%w: metadata exceeds %d bytes", ErrResourceLimit, MaxMetadataBytes)
	}
	if len(schedule.Environments) > MaxEnvironments {
		return fmt.Errorf("%w: environments exceed %d entries", ErrResourceLimit, MaxEnvironments)
	}
	for _, environment := range schedule.Environments {
		if len(environment) > MaxIdentityBytes {
			return fmt.Errorf("%w: environment exceeds %d bytes", ErrResourceLimit, MaxIdentityBytes)
		}
	}
	if len(schedule.Conditions) > MaxConditions {
		return fmt.Errorf("%w: conditions exceed %d entries", ErrResourceLimit, MaxConditions)
	}
	if schedule.MaxCatchUp > MaxCatchUp {
		return fmt.Errorf("%w: catch-up exceeds %d occurrences", ErrResourceLimit, MaxCatchUp)
	}
	return nil
}

// WithTimezone sets the schedule's explicit IANA time-zone name.
func WithTimezone(timezone string) Option {
	return func(schedule *Schedule) error {
		schedule.Timezone = timezone
		return nil
	}
}

// WithVersion sets the semantic version included in schedule identity.
func WithVersion(version string) Option {
	return func(schedule *Schedule) error {
		if strings.TrimSpace(version) == "" {
			return ErrInvalidVersion
		}
		schedule.Version = version
		return nil
	}
}

// WithEnabled controls whether the compiled schedule produces occurrences.
func WithEnabled(enabled bool) Option {
	return func(schedule *Schedule) error {
		schedule.Enabled = enabled
		return nil
	}
}

// WithMaintenancePolicy controls execution during application maintenance.
func WithMaintenancePolicy(policy MaintenancePolicy) Option {
	return func(schedule *Schedule) error {
		if policy > MaintenanceRun {
			return fmt.Errorf("scheduler: invalid maintenance policy: %d", policy)
		}
		schedule.MaintenancePolicy = policy
		return nil
	}
}

// WithJitter sets the maximum deterministic per-schedule offset.
func WithJitter(jitter time.Duration) Option {
	return func(schedule *Schedule) error {
		if jitter < 0 || jitter > MaxJitter {
			return fmt.Errorf("scheduler: jitter must be between zero and %s", MaxJitter)
		}
		schedule.Jitter = jitter
		return nil
	}
}

// WithParameters copies JSON-compatible task parameters into the schedule.
func WithParameters(parameters map[string]any) Option {
	return func(schedule *Schedule) error {
		encoded, err := json.Marshal(parameters)
		if err != nil {
			return fmt.Errorf("scheduler: encode parameters: %w", err)
		}
		_ = json.Unmarshal(encoded, &schedule.Parameters)
		return nil
	}
}

// WithEnvironments restricts execution to named application environments.
func WithEnvironments(environments ...string) Option {
	return func(schedule *Schedule) error {
		schedule.Environments = slices.Clone(environments)
		return nil
	}
}

// WithDateBounds limits occurrences to inclusive start and end instants.
func WithDateBounds(start, end time.Time) Option {
	return func(schedule *Schedule) error {
		schedule.StartAt = start
		schedule.EndAt = end
		return nil
	}
}

// WithMissedRuns sets delayed-boundary behavior and its catch-up cap.
func WithMissedRuns(policy MissedRunPolicy, maxCatchUp int) Option {
	return func(schedule *Schedule) error {
		if policy > MissedRunCatchUp || maxCatchUp < 0 {
			return ErrInvalidMissedRuns
		}
		schedule.MissedRunPolicy = policy
		schedule.MaxCatchUp = maxCatchUp
		return nil
	}
}

// WithOverlap records overlap policy without enabling a task lease.
func WithOverlap(policy OverlapPolicy) Option {
	return func(schedule *Schedule) error {
		if policy > OverlapReplace {
			return fmt.Errorf("scheduler: invalid overlap policy: %d", policy)
		}
		schedule.OverlapPolicy = policy
		return nil
	}
}

// WithMetadata copies bounded diagnostic metadata into the schedule.
func WithMetadata(metadata map[string]string) Option {
	return func(schedule *Schedule) error {
		schedule.Metadata = maps.Clone(metadata)
		return nil
	}
}

// WithCondition appends a non-nil trusted application condition.
func WithCondition(condition Condition) Option {
	return func(schedule *Schedule) error {
		if condition != nil {
			schedule.Conditions = append(schedule.Conditions, condition)
		}
		return nil
	}
}

// WithOneServer enables a distributed occurrence lease with the given TTL.
func WithOneServer(ttl time.Duration) Option {
	return func(schedule *Schedule) error {
		if ttl <= 0 {
			return fmt.Errorf("scheduler: one-server lease: %w", ErrInvalidDuration)
		}
		schedule.OnOneServer = true
		schedule.LeaseTTL = ttl
		return nil
	}
}

// WithoutOverlap enables a renewable task lease and overlap policy.
func WithoutOverlap(policy OverlapPolicy, ttl time.Duration) Option {
	return func(schedule *Schedule) error {
		if policy == OverlapAllow || policy > OverlapReplace || ttl <= 0 {
			return fmt.Errorf("scheduler: overlap lease: %w", ErrInvalidDuration)
		}
		schedule.WithoutOverlapping = true
		schedule.OverlapPolicy = policy
		schedule.LeaseTTL = ttl
		return nil
	}
}

// WithRunTimeout sets the execution deadline observed by the runner.
func WithRunTimeout(timeout time.Duration) Option {
	return func(schedule *Schedule) error {
		if timeout <= 0 {
			return ErrInvalidDuration
		}
		schedule.RunTimeout = timeout
		return nil
	}
}

// WithHooks sets lifecycle callbacks managed by the runner's callback bounds.
func WithHooks(hooks Hooks) Option {
	return func(schedule *Schedule) error {
		schedule.Hooks = hooks
		return nil
	}
}

func stableHash(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func hashStrings(values ...string) string {
	digest := sha256.New()
	for _, value := range values {
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Environments = slices.Clone(schedule.Environments)
	schedule.Conditions = slices.Clone(schedule.Conditions)
	schedule.Metadata = maps.Clone(schedule.Metadata)
	if encoded, err := json.Marshal(schedule.Parameters); err == nil {
		_ = json.Unmarshal(encoded, &schedule.Parameters)
	}
	return schedule
}
