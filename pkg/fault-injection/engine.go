package faultinject

import (
	"cmp"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

// Config is copied and validated by New.
type Config struct {
	Rules                []Rule
	Clock                Clock
	Sleeper              Sleeper
	Observer             Observer
	MaxRules             int
	MaxFaultsPerDecision int
	MaxLatency           time.Duration
	MaxBytes             int
}

// Rule declares all activation, scope, bound, ordering, terminal, schedule,
// predicate, observation, and fault composition behavior.
type Rule struct {
	ID          string
	Scope       Boundary
	Activation  Activation
	Maximum     uint64
	Terminal    Terminal
	Order       int
	Observation Observation
	Schedule    Schedule
	Predicate   Predicate
	Faults      []Fault
}

type runtimeRule struct {
	rule       Rule
	calls      uint64
	injections uint64
	seed       uint64
}

// Injector owns bounded rule state. Its zero value is disabled and safe for
// concurrent use.
type Injector struct {
	mu          sync.Mutex
	enabled     bool
	rules       []runtimeRule
	clock       Clock
	sleeper     Sleeper
	observer    Observer
	maxFaults   int
	generation  uint64
	evaluations uint64
	injections  uint64
}

// New copies and validates configuration before returning an active injector.
func New(config Config) (*Injector, error) {
	maxRules := config.MaxRules
	if maxRules == 0 {
		maxRules = defaultMaxRules
	}
	maxFaults := config.MaxFaultsPerDecision
	if maxFaults == 0 {
		maxFaults = defaultMaxFaults
	}
	maxLatency := config.MaxLatency
	if maxLatency == 0 {
		maxLatency = defaultMaxLatency
	}
	if maxRules < 1 || maxRules > 1024 {
		return nil, invalid("MaxRules", "must be between 1 and 1024")
	}
	if maxFaults < 1 || maxFaults > 256 {
		return nil, invalid("MaxFaultsPerDecision", "must be between 1 and 256")
	}
	if maxLatency < time.Nanosecond {
		return nil, invalid("MaxLatency", "must be positive")
	}
	maxBytes := config.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes < 1 || maxBytes > 16*1024*1024 {
		return nil, invalid("MaxBytes", "must be between 1 and 16777216")
	}
	if len(config.Rules) > maxRules {
		return nil, invalid("Rules", "exceeds MaxRules")
	}

	if nilInterface(config.Clock) && config.Clock != nil {
		return nil, invalid("Clock", "must not be typed nil")
	}
	if nilInterface(config.Sleeper) && config.Sleeper != nil {
		return nil, invalid("Sleeper", "must not be typed nil")
	}
	if nilInterface(config.Observer) && config.Observer != nil {
		return nil, invalid("Observer", "must not be typed nil")
	}
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	sleeper := config.Sleeper
	if sleeper == nil {
		sleeper = systemSleeper{}
	}
	rules := make([]runtimeRule, len(config.Rules))
	identities := make(map[string]struct{}, len(config.Rules))
	for index := range config.Rules {
		rule, err := normalizeRule(config.Rules[index], maxFaults, maxLatency, maxBytes)
		if err != nil {
			return nil, err
		}
		if _, duplicate := identities[rule.ID]; duplicate {
			return nil, invalid("Rules.ID", "must be unique")
		}
		identities[rule.ID] = struct{}{}
		rules[index] = runtimeRule{rule: rule, seed: scheduleSeed(rule.Schedule)}
	}
	slices.SortFunc(rules, func(left, right runtimeRule) int {
		return cmp.Or(
			cmp.Compare(left.rule.Order, right.rule.Order),
			strings.Compare(left.rule.ID, right.rule.ID),
		)
	})

	return &Injector{
		enabled: true, rules: rules, clock: clock, sleeper: sleeper, observer: config.Observer,
		maxFaults: maxFaults, generation: 1,
	}, nil
}

func normalizeRule(input Rule, maxFaults int, maxLatency time.Duration, maxBytes int) (Rule, error) {
	if !validIdentity(input.ID) {
		return Rule{}, invalid("Rules.ID", "must be a bounded safe identity")
	}
	if !validIdentity(string(input.Scope)) {
		return Rule{}, invalid("Rules.Scope", "must be a bounded safe identity")
	}
	if input.Activation != Inactive && input.Activation != Active {
		return Rule{}, invalid("Rules.Activation", "must be declared")
	}
	if input.Maximum == 0 || input.Maximum > 1_000_000_000 {
		return Rule{}, invalid("Rules.Maximum", "must be between 1 and 1000000000")
	}
	if input.Terminal != Continue && input.Terminal != Stop {
		return Rule{}, invalid("Rules.Terminal", "must be declared")
	}
	if input.Observation != Suppress && input.Observation != Observe {
		return Rule{}, invalid("Rules.Observation", "must be declared")
	}
	schedule, err := cloneSchedule(input.Schedule)
	if err != nil {
		return Rule{}, err
	}
	if len(input.Faults) == 0 || len(input.Faults) > maxFaults {
		return Rule{}, invalid("Rules.Faults", "must be non-empty and within MaxFaultsPerDecision")
	}
	faults := append([]Fault(nil), input.Faults...)
	for index := range faults {
		if err := validateFault("Rules.Faults", faults[index], maxLatency, maxBytes); err != nil {
			return Rule{}, err
		}
	}
	input.Schedule = schedule
	input.Faults = faults
	return input, nil
}

func validIdentity(value string) bool {
	if len(value) == 0 || len(value) > maxIdentityLength {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

// Decide atomically orders one evaluation. Lock acquisition defines ordering;
// identical configurations and acquisition order produce identical decisions.
func (i *Injector) Decide(metadata Metadata) Decision {
	if i == nil || !i.enabled {
		return Decision{}
	}

	eligible := make([]bool, len(i.rules))
	for index := range i.rules {
		rule := &i.rules[index].rule
		eligible[index] = rule.Activation == Active && rule.Scope == metadata.Boundary &&
			(rule.Predicate == nil || safePredicate(rule.Predicate, metadata))
	}

	i.mu.Lock()
	i.evaluations = saturatingIncrement(i.evaluations)
	sequence := i.evaluations
	generation := i.generation
	var faults []Fault
	var events []Event
	for index := 0; index < len(i.rules) && len(faults) < i.maxFaults; index++ {
		if !eligible[index] {
			continue
		}
		rule := &i.rules[index]
		rule.calls = saturatingIncrement(rule.calls)
		if rule.injections >= rule.rule.Maximum || !scheduleMatches(rule.rule.Schedule, rule.calls) {
			continue
		}
		rule.injections = saturatingIncrement(rule.injections)
		i.injections = saturatingIncrement(i.injections)
		remaining := i.maxFaults - len(faults)
		selected := rule.rule.Faults[:min(len(rule.rule.Faults), remaining)]
		faults = append(faults, selected...)
		if rule.rule.Observation == Observe && i.observer != nil {
			for _, fault := range selected {
				events = append(events, Event{
					RuleID: rule.rule.ID, Boundary: metadata.Boundary,
					Kind: fault.Kind, Sequence: sequence,
					Injection: rule.injections, SeedIdentity: rule.seed,
					Generation: generation,
				})
			}
		}
		if rule.rule.Terminal == Stop {
			break
		}
	}
	i.mu.Unlock()

	if len(events) != 0 {
		at := safeNow(i.clock)
		for index := range events {
			events[index].At = at
			safeObserve(i.observer, events[index])
		}
	}
	return Decision{sequence: sequence, generation: generation, faults: faults}
}

func safeObserve(observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.Observe(event)
}

func safePredicate(predicate Predicate, metadata Metadata) (matched bool) {
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	return predicate(metadata)
}

func safeNow(clock Clock) (now time.Time) {
	defer func() { _ = recover() }()
	return clock.Now()
}

func saturatingIncrement(value uint64) uint64 {
	if value == math.MaxUint64 {
		return value
	}
	return value + 1
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Reset begins a new activation generation and clears bounded counters. Any
// previously returned Decision retains its original generation and faults.
func (i *Injector) Reset() uint64 {
	if i == nil || !i.enabled {
		return 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.generation == math.MaxUint64 {
		return i.generation
	}
	i.generation++
	i.evaluations = 0
	i.injections = 0
	for index := range i.rules {
		i.rules[index].calls = 0
		i.rules[index].injections = 0
	}
	return i.generation
}

// RuleSnapshot is bounded per-rule accounting in precedence order.
type RuleSnapshot struct {
	ID         string
	Calls      uint64
	Injections uint64
}

// Snapshot is a point-in-time copy of bounded Injector state.
type Snapshot struct {
	Generation  uint64
	Evaluations uint64
	Injections  uint64
	Rules       []RuleSnapshot
}

// Snapshot returns current state without exposing mutable configuration.
func (i *Injector) Snapshot() Snapshot {
	if i == nil || !i.enabled {
		return Snapshot{}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	rules := make([]RuleSnapshot, len(i.rules))
	for index := range i.rules {
		rules[index] = RuleSnapshot{
			ID: i.rules[index].rule.ID, Calls: i.rules[index].calls,
			Injections: i.rules[index].injections,
		}
	}
	return Snapshot{
		Generation: i.generation, Evaluations: i.evaluations,
		Injections: i.injections, Rules: rules,
	}
}

// Decision is an immutable selection attributed to one generation.
type Decision struct {
	sequence   uint64
	generation uint64
	faults     []Fault
}

// Injected reports whether any fault was selected.
func (d Decision) Injected() bool { return len(d.faults) != 0 }

// Sequence returns the injector-local evaluation sequence.
func (d Decision) Sequence() uint64 { return d.sequence }

// Generation returns the activation generation captured during selection.
func (d Decision) Generation() uint64 { return d.generation }

// Faults returns a copy of selected faults in composition order.
func (d Decision) Faults() []Fault { return append([]Fault(nil), d.faults...) }
