package eventsourcing

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"
	"time"
)

const (
	// MaxUpcastSteps is the maximum transformations in one logical event path.
	MaxUpcastSteps = 64
	maxUpcastWork  = MaxUpcastSegments * 4
)

// UpcastEvent is immutable encoded event data plus application metadata at the
// event-store read boundary.
type UpcastEvent struct {
	event    EncodedEvent
	metadata map[string]string
}

// NewUpcastEvent validates and owns encoded event data and metadata.
func NewUpcastEvent(
	event EncodedEvent,
	metadata map[string]string,
) (UpcastEvent, error) {
	if event.IsZero() {
		return UpcastEvent{}, invalid("event", "must be assigned")
	}
	owned, err := copyMetadata(metadata)
	if err != nil {
		return UpcastEvent{}, err
	}

	return UpcastEvent{event: cloneEvent(event), metadata: owned}, nil
}

// Event returns a defensive encoded-event copy.
func (event UpcastEvent) Event() EncodedEvent {
	return cloneEvent(event.event)
}

// Metadata returns a defensive application-metadata copy.
func (event UpcastEvent) Metadata() map[string]string {
	return cloneMetadata(event.metadata)
}

// IsZero reports whether the upcast event is unassigned.
func (event UpcastEvent) IsZero() bool {
	return event.event.IsZero()
}

// UpcasterFunc transforms one encoded event into zero, one, or many events.
type UpcasterFunc func(UpcastEvent) ([]UpcastEvent, error)

// Upcaster transforms one encoded event into ordered logical events.
//
// Implementations must be deterministic, bounded, safe for concurrent use,
// return independently owned events, and report stored input failures without
// panicking. UpcasterChain is the reference implementation.
type Upcaster interface {
	Upcast(UpcastEvent) ([]UpcastEvent, error)
}

// ContextUpcaster optionally exposes caller context to an upcaster.
//
// Repository operations prefer this extension when implemented. Upcasters
// must not retain the context or use it to make evolution nondeterministic.
type ContextUpcaster interface {
	Upcaster
	UpcastContext(context.Context, UpcastEvent) ([]UpcastEvent, error)
}

// ReviewedDropPolicy records explicit human review for removing one obsolete
// logical event during reads. Stored history remains unchanged.
type ReviewedDropPolicy struct {
	rationale  string
	reviewer   string
	reviewedAt time.Time
}

// NewReviewedDropPolicy validates one auditable event-drop decision.
func NewReviewedDropPolicy(
	rationale string,
	reviewer string,
	reviewedAt time.Time,
) (ReviewedDropPolicy, error) {
	if strings.TrimSpace(rationale) == "" ||
		!validText(rationale, MaxMetadataValueBytes, true) {
		return ReviewedDropPolicy{}, invalid("rationale", "must be assigned and bounded")
	}
	if strings.TrimSpace(reviewer) == "" ||
		!validText(reviewer, MaxMetadataValueBytes, true) {
		return ReviewedDropPolicy{}, invalid("reviewer", "must be assigned and bounded")
	}
	if reviewedAt.IsZero() {
		return ReviewedDropPolicy{}, invalid("reviewed_at", "must be assigned")
	}

	return ReviewedDropPolicy{
		rationale:  rationale,
		reviewer:   reviewer,
		reviewedAt: normalizeTime(reviewedAt),
	}, nil
}

// Rationale returns the reviewed reason for dropping the logical event.
func (policy ReviewedDropPolicy) Rationale() string {
	return policy.rationale
}

// Reviewer returns the recorded human reviewer identity.
func (policy ReviewedDropPolicy) Reviewer() string {
	return policy.reviewer
}

// ReviewedAt returns the canonical UTC review time.
func (policy ReviewedDropPolicy) ReviewedAt() time.Time {
	return policy.reviewedAt
}

// UpcastRuleOption configures one immutable exact-identity rule.
type UpcastRuleOption interface {
	configureUpcastRule(*UpcastRule) error
}

type allowUpcastDropOption struct {
	policy ReviewedDropPolicy
}

// AllowUpcastDrop permits an empty result under an explicit reviewed policy.
func AllowUpcastDrop(policy ReviewedDropPolicy) UpcastRuleOption {
	return allowUpcastDropOption{policy: policy}
}

// UpcastRule applies one function to one exact persisted event identity.
type UpcastRule struct {
	name      EventName
	version   SchemaVersion
	upcaster  UpcasterFunc
	drop      ReviewedDropPolicy
	allowDrop bool
}

// NewUpcastRule validates one exact event-name and schema-version rule.
func NewUpcastRule(
	name string,
	version SchemaVersion,
	upcaster UpcasterFunc,
	options ...UpcastRuleOption,
) (UpcastRule, error) {
	if !validName(name, MaxEventNameBytes) {
		return UpcastRule{}, invalid("event_name", "must be a non-empty canonical name")
	}
	if version == 0 {
		return UpcastRule{}, invalid("schema_version", "must be greater than zero")
	}
	if upcaster == nil {
		return UpcastRule{}, invalid("upcaster", "must be assigned")
	}

	rule := UpcastRule{
		name:     EventName{value: name},
		version:  version,
		upcaster: upcaster,
	}
	for _, option := range options {
		if option == nil {
			return UpcastRule{}, invalid("option", "must be assigned")
		}
		if err := option.configureUpcastRule(&rule); err != nil {
			return UpcastRule{}, fmt.Errorf("configure upcast rule: %w", err)
		}
	}

	return rule, nil
}

func (option allowUpcastDropOption) configureUpcastRule(
	rule *UpcastRule,
) error {
	if option.policy.reviewedAt.IsZero() {
		return invalid("drop_policy", "must be constructed")
	}
	if rule.allowDrop {
		return invalid("drop_policy", "must not be configured more than once")
	}
	rule.drop = option.policy
	rule.allowDrop = true

	return nil
}

// UpcasterChain applies exact-identity rules until every path reaches an
// unmatched event or an explicitly reviewed drop.
type UpcasterChain struct {
	rules map[upcastIdentity]UpcastRule
}

// NewUpcasterChain validates unique exact rule identities.
func NewUpcasterChain(rules ...UpcastRule) (*UpcasterChain, error) {
	registered := make(map[upcastIdentity]UpcastRule, len(rules))
	for _, rule := range rules {
		if rule.name.value == "" || rule.version == 0 || rule.upcaster == nil {
			return nil, invalid("rule", "must be constructed")
		}
		identity := upcastIdentity{name: rule.name.value, version: rule.version}
		if _, duplicate := registered[identity]; duplicate {
			return nil, fmt.Errorf(
				"%w: %s/%d",
				ErrDuplicateRegistration,
				rule.name.String(),
				rule.version,
			)
		}
		registered[identity] = rule
	}

	return &UpcasterChain{rules: registered}, nil
}

// Upcast returns deterministic ordered logical events without modifying stored
// history.
func (chain *UpcasterChain) Upcast(
	event UpcastEvent,
) ([]UpcastEvent, error) {
	if chain == nil || event.IsZero() {
		return nil, ErrInvalidArgument
	}

	identity := identityOf(event)
	path := map[upcastIdentity]struct{}{identity: {}}
	work := 0
	output, err := chain.upcastOne(event, path, 0, &work)
	if err != nil {
		return nil, err
	}

	return cloneUpcastEvents(output), nil
}

// UpcastContext applies the deterministic chain after checking cancellation.
func (chain *UpcasterChain) UpcastContext(
	ctx context.Context,
	event UpcastEvent,
) ([]UpcastEvent, error) {
	if ctx == nil {
		return nil, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return chain.Upcast(event)
}

func upcastWithContext(
	ctx context.Context,
	upcaster Upcaster,
	event UpcastEvent,
) ([]UpcastEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contextual, ok := upcaster.(ContextUpcaster); ok {
		return contextual.UpcastContext(ctx, event)
	}

	return upcaster.Upcast(event)
}

func (chain *UpcasterChain) upcastOne(
	event UpcastEvent,
	path map[upcastIdentity]struct{},
	steps int,
	work *int,
) ([]UpcastEvent, error) {
	identity := identityOf(event)
	rule, matched := chain.rules[identity]
	if !matched {
		return []UpcastEvent{cloneUpcastEvent(event)}, nil
	}
	if steps >= MaxUpcastSteps || *work >= maxUpcastWork {
		return nil, newUpcastError(event, ErrUpcastLimit)
	}

	output, err := deterministicUpcast(rule, event)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		if !rule.allowDrop {
			return nil, newUpcastError(event, ErrUpcastDropNotAllowed)
		}

		return nil, nil
	}
	if len(output) > MaxUpcastSegments {
		return nil, newUpcastError(event, ErrUpcastLimit)
	}

	var result []UpcastEvent
	for _, next := range output {
		*work++
		if next.IsZero() {
			return nil, newUpcastError(event, ErrInvalidArgument)
		}

		nextIdentity := identityOf(next)
		if nextIdentity.name == identity.name &&
			nextIdentity.version <= identity.version {
			return nil, newUpcastError(event, ErrUpcastNonProgress)
		}
		if _, repeated := path[nextIdentity]; repeated {
			return nil, newUpcastError(event, ErrUpcastCycle)
		}

		nextPath := maps.Clone(path)
		nextPath[nextIdentity] = struct{}{}
		expanded, err := chain.upcastOne(next, nextPath, steps+1, work)
		if err != nil {
			return nil, err
		}
		if len(result)+len(expanded) > MaxUpcastSegments {
			return nil, newUpcastError(event, ErrUpcastLimit)
		}
		result = append(result, expanded...)
	}

	return result, nil
}

func deterministicUpcast(
	rule UpcastRule,
	event UpcastEvent,
) ([]UpcastEvent, error) {
	first, err := callUpcaster(rule, event)
	if err != nil {
		return nil, err
	}
	second, err := callUpcaster(rule, event)
	if err != nil || !upcastEventsEqual(first, second) {
		return nil, newUpcastError(event, ErrNonDeterministicUpcast)
	}

	return cloneUpcastEvents(first), nil
}

func callUpcaster(
	rule UpcastRule,
	event UpcastEvent,
) (output []UpcastEvent, err error) {
	defer func() {
		if recover() != nil {
			output = nil
			err = newUpcastError(event, ErrUpcasterPanic)
		}
	}()

	output, err = rule.upcaster(cloneUpcastEvent(event))
	if err != nil {
		return nil, newUpcastError(event, err)
	}

	return cloneUpcastEvents(output), nil
}

type upcastIdentity struct {
	name    string
	version SchemaVersion
}

func identityOf(event UpcastEvent) upcastIdentity {
	return upcastIdentity{
		name:    event.event.name.value,
		version: event.event.version,
	}
}

func cloneUpcastEvent(event UpcastEvent) UpcastEvent {
	return UpcastEvent{
		event:    cloneEvent(event.event),
		metadata: cloneMetadata(event.metadata),
	}
}

func cloneUpcastEvents(events []UpcastEvent) []UpcastEvent {
	output := make([]UpcastEvent, len(events))
	for index, event := range events {
		output[index] = cloneUpcastEvent(event)
	}

	return output
}

func upcastEventsEqual(left, right []UpcastEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].event.name != right[index].event.name ||
			left[index].event.version != right[index].event.version ||
			left[index].event.contentType != right[index].event.contentType ||
			!bytes.Equal(left[index].event.payload, right[index].event.payload) ||
			!maps.Equal(left[index].metadata, right[index].metadata) {
			return false
		}
	}

	return true
}

func newUpcastError(event UpcastEvent, cause error) *UpcastError {
	return &UpcastError{
		EventName:     event.event.name,
		SchemaVersion: event.event.version,
		Cause:         cause,
	}
}

// UpcastError identifies the stored event identity that failed without
// printing payload, metadata, panic values, or wrapped diagnostics.
type UpcastError struct {
	EventName     EventName
	SchemaVersion SchemaVersion
	Cause         error
}

// Error implements error with redacted diagnostics.
func (*UpcastError) Error() string {
	return "event upcast failed"
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (err *UpcastError) Unwrap() error {
	return err.Cause
}

var _ error = (*UpcastError)(nil)
