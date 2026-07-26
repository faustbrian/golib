// Package acl provides typed subject-to-resource access control lists.
package acl

import (
	"context"
	"errors"
	"fmt"
	"sort"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var (
	ErrInvalidEntry         = errors.New("invalid ACL entry")
	ErrDuplicateEntry       = errors.New("duplicate ACL entry")
	ErrEntryLimitExceeded   = errors.New("ACL entry limit exceeded")
	ErrGroupLimitExceeded   = errors.New("ACL group limit exceeded")
	ErrMatchLimitExceeded   = errors.New("ACL match limit exceeded")
	ErrBatchLimitExceeded   = errors.New("ACL batch limit exceeded")
	ErrUnboundedResourceSet = errors.New("ACL resource set is unbounded")
)

type EntryID = authorization.PolicyID

const (
	ReasonAllow         authorization.ReasonCode = "acl-allow"
	ReasonExplicitDeny  authorization.ReasonCode = "acl-explicit-deny"
	ReasonLimitExceeded authorization.ReasonCode = "acl-limit-exceeded"
)

const (
	defaultMaxEntries   = 10_000
	defaultMaxGroups    = 100
	defaultMaxMatches   = 100
	defaultMaxBatchSize = 1000
)

// Limits bounds ACL construction and per-request evaluation work.
type Limits struct {
	MaxEntries   int `json:"max_entries,omitempty"`
	MaxGroups    int `json:"max_groups,omitempty"`
	MaxMatches   int `json:"max_matches,omitempty"`
	MaxBatchSize int `json:"max_batch_size,omitempty"`
}

// Entry grants or denies one subject an action on a resource type or instance.
type Entry struct {
	ID           EntryID
	Subject      authorization.Subject
	Action       authorization.Action
	ResourceType authorization.ResourceType
	ResourceID   authorization.ResourceID
	Tenant       authorization.TenantID
	Effect       authorization.Outcome
}

type entryKey struct {
	subjectKind  authorization.SubjectKind
	subjectID    authorization.SubjectID
	action       authorization.Action
	resourceType authorization.ResourceType
}

// Evaluator resolves immutable ACL entries without external I/O.
type Evaluator struct {
	entries       map[entryKey][]Entry
	inheritGlobal bool
	limits        Limits
}

type Option func(*Evaluator)

// WithGlobalInheritance makes global entries apply to tenant-scoped requests.
// Without this option, global and tenant scopes are isolated.
func WithGlobalInheritance() Option {
	return func(evaluator *Evaluator) {
		evaluator.inheritGlobal = true
	}
}

// WithLimits configures positive bounds while retaining safe defaults for
// zero-valued fields.
func WithLimits(limits Limits) Option {
	return func(evaluator *Evaluator) {
		if limits.MaxEntries > 0 {
			evaluator.limits.MaxEntries = limits.MaxEntries
		}
		if limits.MaxGroups > 0 {
			evaluator.limits.MaxGroups = limits.MaxGroups
		}
		if limits.MaxMatches > 0 {
			evaluator.limits.MaxMatches = limits.MaxMatches
		}
		if limits.MaxBatchSize > 0 {
			evaluator.limits.MaxBatchSize = limits.MaxBatchSize
		}
	}
}

// New validates and indexes ACL entries.
func New(entries []Entry, options ...Option) (*Evaluator, error) {
	evaluator := &Evaluator{
		limits: Limits{
			MaxEntries:   defaultMaxEntries,
			MaxGroups:    defaultMaxGroups,
			MaxMatches:   defaultMaxMatches,
			MaxBatchSize: defaultMaxBatchSize,
		},
	}
	for _, option := range options {
		option(evaluator)
	}
	if len(entries) > evaluator.limits.MaxEntries {
		return nil, ErrEntryLimitExceeded
	}

	indexed := make(map[entryKey][]Entry, len(entries))
	entryIDs := make(map[EntryID]struct{}, len(entries))
	for index, entry := range entries {
		if entry.ID == "" || entry.Subject.Kind == "" || entry.Subject.ID == "" ||
			entry.Action == "" || entry.ResourceType == "" ||
			(entry.Effect != authorization.Allow && entry.Effect != authorization.Deny) {
			return nil, fmt.Errorf("entry %d: %w", index, ErrInvalidEntry)
		}

		if _, exists := entryIDs[entry.ID]; exists {
			return nil, fmt.Errorf("entry %q: %w", entry.ID, ErrDuplicateEntry)
		}
		entryIDs[entry.ID] = struct{}{}

		key := entryKey{
			subjectKind:  entry.Subject.Kind,
			subjectID:    entry.Subject.ID,
			action:       entry.Action,
			resourceType: entry.ResourceType,
		}
		indexed[key] = append(indexed[key], entry)
	}

	evaluator.entries = indexed

	return evaluator, nil
}

// Evaluate checks matching type and instance entries for one request.
func (evaluator *Evaluator) Evaluate(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	if err := ctx.Err(); err != nil {
		return authorization.Decision{
			Outcome: authorization.Deny,
			Reason:  authorization.ReasonContextCanceled,
		}, err
	}
	if len(request.Subject.Groups) > evaluator.limits.MaxGroups {
		return limitDecision(), ErrGroupLimitExceeded
	}

	principals := make([]authorization.Subject, 0, len(request.Subject.Groups)+1)
	principals = append(principals, request.Subject)
	for _, groupID := range request.Subject.Groups {
		principals = append(principals, authorization.Subject{
			Kind: authorization.SubjectGroup,
			ID:   groupID,
		})
	}

	decision := authorization.Decision{Outcome: authorization.NotApplicable}
	seenPrincipals := make(map[entryKey]struct{}, len(principals))
	for _, principal := range principals {
		if err := ctx.Err(); err != nil {
			return authorization.Decision{
				Outcome: authorization.Deny,
				Reason:  authorization.ReasonContextCanceled,
			}, err
		}

		key := entryKey{
			subjectKind:  principal.Kind,
			subjectID:    principal.ID,
			action:       request.Action,
			resourceType: request.Resource.Type,
		}
		if _, seen := seenPrincipals[key]; seen {
			continue
		}
		seenPrincipals[key] = struct{}{}

		for _, entry := range evaluator.entries[key] {
			if !evaluator.matchesTenant(entry.Tenant, request.Tenant) ||
				(entry.ResourceID != "" && entry.ResourceID != request.Resource.ID) {
				continue
			}
			if len(decision.MatchedPolicyIDs) >= evaluator.limits.MaxMatches {
				return limitDecision(), ErrMatchLimitExceeded
			}

			decision.MatchedPolicyIDs = append(decision.MatchedPolicyIDs, entry.ID)
			if entry.Effect == authorization.Deny {
				decision.Outcome = authorization.Deny
				decision.Reason = ReasonExplicitDeny
			} else if decision.Outcome == authorization.NotApplicable {
				decision.Outcome = authorization.Allow
				decision.Reason = ReasonAllow
			}
		}
	}

	return decision, nil
}

// EvaluateBatch evaluates a bounded request set against the evaluator's
// immutable ACL view.
func (evaluator *Evaluator) EvaluateBatch(
	ctx context.Context,
	requests []authorization.Request,
) ([]authorization.Decision, error) {
	if len(requests) > evaluator.limits.MaxBatchSize {
		return nil, ErrBatchLimitExceeded
	}

	decisions := make([]authorization.Decision, len(requests))
	evaluationErrors := make([]error, 0)
	for index, request := range requests {
		decision, err := evaluator.Evaluate(ctx, request)
		decisions[index] = decision
		if err != nil {
			evaluationErrors = append(evaluationErrors, err)
		}
	}

	return decisions, errors.Join(evaluationErrors...)
}

// ListResourceIDs returns only explicitly enumerable instance grants. A
// resource-type allow is reported as unbounded instead of triggering a scan.
func (evaluator *Evaluator) ListResourceIDs(
	ctx context.Context,
	subject authorization.Subject,
	action authorization.Action,
	resourceType authorization.ResourceType,
	tenant authorization.TenantID,
) ([]authorization.ResourceID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(subject.Groups) > evaluator.limits.MaxGroups {
		return nil, ErrGroupLimitExceeded
	}

	resourceEffects := make(map[authorization.ResourceID]authorization.Outcome)
	typeWideAllow := false
	matches := 0
	for _, key := range principalKeys(subject, action, resourceType) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		for _, entry := range evaluator.entries[key] {
			if !evaluator.matchesTenant(entry.Tenant, tenant) {
				continue
			}
			if matches >= evaluator.limits.MaxMatches {
				return nil, ErrMatchLimitExceeded
			}
			matches++

			if entry.ResourceID == "" {
				if entry.Effect == authorization.Deny {
					return []authorization.ResourceID{}, nil
				}
				typeWideAllow = true
				continue
			}

			if entry.Effect == authorization.Deny ||
				resourceEffects[entry.ResourceID] == authorization.NotApplicable {
				resourceEffects[entry.ResourceID] = entry.Effect
			}
		}
	}

	if typeWideAllow {
		return nil, ErrUnboundedResourceSet
	}

	resourceIDs := make([]authorization.ResourceID, 0, len(resourceEffects))
	for resourceID, effect := range resourceEffects {
		if effect == authorization.Allow {
			resourceIDs = append(resourceIDs, resourceID)
		}
	}
	sort.Slice(resourceIDs, func(left, right int) bool {
		return resourceIDs[left] < resourceIDs[right]
	})

	return resourceIDs, nil
}

func principalKeys(
	subject authorization.Subject,
	action authorization.Action,
	resourceType authorization.ResourceType,
) []entryKey {
	keys := make([]entryKey, 0, len(subject.Groups)+1)
	seen := make(map[entryKey]struct{}, len(subject.Groups)+1)
	principals := make([]authorization.Subject, 0, len(subject.Groups)+1)
	principals = append(principals, subject)
	for _, groupID := range subject.Groups {
		principals = append(principals, authorization.Subject{
			Kind: authorization.SubjectGroup,
			ID:   groupID,
		})
	}

	for _, principal := range principals {
		key := entryKey{
			subjectKind:  principal.Kind,
			subjectID:    principal.ID,
			action:       action,
			resourceType: resourceType,
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	return keys
}

func limitDecision() authorization.Decision {
	return authorization.Decision{
		Outcome: authorization.Deny,
		Reason:  ReasonLimitExceeded,
	}
}

func (evaluator *Evaluator) matchesTenant(
	entryTenant authorization.TenantID,
	requestTenant authorization.TenantID,
) bool {
	if entryTenant == requestTenant {
		return true
	}

	return requestTenant != "" && entryTenant == "" && evaluator.inheritGlobal
}
