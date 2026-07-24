// Package rbac provides typed roles, permissions, assignments, and bounded
// role inheritance.
package rbac

import (
	"context"
	"errors"
	"fmt"
	"sort"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var (
	ErrRoleCycle                = errors.New("RBAC role inheritance cycle")
	ErrInheritanceDepthExceeded = errors.New("RBAC inheritance depth exceeded")
	ErrUnknownParentRole        = errors.New("RBAC parent role does not exist")
	ErrCrossTenantInheritance   = errors.New("RBAC inheritance crosses tenants")
	ErrInvalidRole              = errors.New("invalid RBAC role")
	ErrDuplicateRole            = errors.New("duplicate RBAC role")
	ErrInvalidPermission        = errors.New("invalid RBAC permission")
	ErrDuplicatePermission      = errors.New("duplicate RBAC permission")
	ErrInvalidAssignment        = errors.New("invalid RBAC assignment")
	ErrDuplicateAssignment      = errors.New("duplicate RBAC assignment")
	ErrUnknownRole              = errors.New("RBAC role does not exist")
	ErrRoleTenantMismatch       = errors.New("RBAC role tenant mismatch")
	ErrRoleLimitExceeded        = errors.New("RBAC role limit exceeded")
	ErrPermissionLimitExceeded  = errors.New("RBAC permission limit exceeded")
	ErrAssignmentLimitExceeded  = errors.New("RBAC assignment limit exceeded")
	ErrGroupLimitExceeded       = errors.New("RBAC group limit exceeded")
	ErrMatchLimitExceeded       = errors.New("RBAC match limit exceeded")
	ErrBatchLimitExceeded       = errors.New("RBAC batch limit exceeded")
)

const (
	defaultMaxInheritanceDepth = 32
	defaultMaxRoles            = 1000
	defaultMaxPermissions      = 10_000
	defaultMaxAssignments      = 10_000
	defaultMaxGroups           = 100
	defaultMaxMatches          = 100
	defaultMaxBatchSize        = 1000
)

const (
	ReasonAllow         authorization.ReasonCode = "rbac-allow"
	ReasonExplicitDeny  authorization.ReasonCode = "rbac-explicit-deny"
	ReasonLimitExceeded authorization.ReasonCode = "rbac-limit-exceeded"
)

type Limits struct {
	MaxInheritanceDepth int `json:"max_inheritance_depth,omitempty"`
	MaxRoles            int `json:"max_roles,omitempty"`
	MaxPermissions      int `json:"max_permissions,omitempty"`
	MaxAssignments      int `json:"max_assignments,omitempty"`
	MaxGroups           int `json:"max_groups,omitempty"`
	MaxMatches          int `json:"max_matches,omitempty"`
	MaxBatchSize        int `json:"max_batch_size,omitempty"`
}

type Option func(*Evaluator)

func WithLimits(limits Limits) Option {
	return func(evaluator *Evaluator) {
		if limits.MaxInheritanceDepth > 0 {
			evaluator.limits.MaxInheritanceDepth = limits.MaxInheritanceDepth
		}
		if limits.MaxRoles > 0 {
			evaluator.limits.MaxRoles = limits.MaxRoles
		}
		if limits.MaxPermissions > 0 {
			evaluator.limits.MaxPermissions = limits.MaxPermissions
		}
		if limits.MaxAssignments > 0 {
			evaluator.limits.MaxAssignments = limits.MaxAssignments
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

func WithGlobalInheritance() Option {
	return func(evaluator *Evaluator) {
		evaluator.inheritGlobal = true
	}
}

type RoleID string
type Role struct {
	ID      RoleID
	Tenant  authorization.TenantID
	Parents []RoleID
}

type Permission struct {
	ID           authorization.PolicyID
	RoleID       RoleID
	Tenant       authorization.TenantID
	Action       authorization.Action
	ResourceType authorization.ResourceType
	ResourceID   authorization.ResourceID
	Effect       authorization.Outcome
	Priority     int
}

type Assignment struct {
	ID      authorization.PolicyID
	Subject authorization.Subject
	RoleID  RoleID
	Tenant  authorization.TenantID
}

type subjectKey struct {
	kind authorization.SubjectKind
	id   authorization.SubjectID
}

type Evaluator struct {
	roles         map[RoleID]Role
	permissions   map[RoleID][]Permission
	assignments   map[subjectKey][]Assignment
	limits        Limits
	inheritGlobal bool
}

func New(
	roles []Role,
	permissions []Permission,
	assignments []Assignment,
	options ...Option,
) (*Evaluator, error) {
	evaluator := &Evaluator{
		roles:       make(map[RoleID]Role, len(roles)),
		permissions: make(map[RoleID][]Permission, len(permissions)),
		assignments: make(map[subjectKey][]Assignment, len(assignments)),
		limits: Limits{
			MaxInheritanceDepth: defaultMaxInheritanceDepth,
			MaxRoles:            defaultMaxRoles,
			MaxPermissions:      defaultMaxPermissions,
			MaxAssignments:      defaultMaxAssignments,
			MaxGroups:           defaultMaxGroups,
			MaxMatches:          defaultMaxMatches,
			MaxBatchSize:        defaultMaxBatchSize,
		},
	}
	for _, option := range options {
		option(evaluator)
	}
	if len(roles) > evaluator.limits.MaxRoles {
		return nil, ErrRoleLimitExceeded
	}
	if len(permissions) > evaluator.limits.MaxPermissions {
		return nil, ErrPermissionLimitExceeded
	}
	if len(assignments) > evaluator.limits.MaxAssignments {
		return nil, ErrAssignmentLimitExceeded
	}

	for index, role := range roles {
		if role.ID == "" {
			return nil, fmt.Errorf("role %d: %w", index, ErrInvalidRole)
		}
		if _, exists := evaluator.roles[role.ID]; exists {
			return nil, fmt.Errorf("role %q: %w", role.ID, ErrDuplicateRole)
		}
		role.Parents = append([]RoleID(nil), role.Parents...)
		evaluator.roles[role.ID] = role
	}
	if err := evaluator.validateInheritance(); err != nil {
		return nil, err
	}
	permissionIDs := make(map[authorization.PolicyID]struct{}, len(permissions))
	for index, permission := range permissions {
		if permission.ID == "" || permission.RoleID == "" ||
			permission.Action == "" || permission.ResourceType == "" ||
			(permission.Effect != authorization.Allow && permission.Effect != authorization.Deny) {
			return nil, fmt.Errorf("permission %d: %w", index, ErrInvalidPermission)
		}
		if _, exists := permissionIDs[permission.ID]; exists {
			return nil, fmt.Errorf("permission %q: %w", permission.ID, ErrDuplicatePermission)
		}
		role, exists := evaluator.roles[permission.RoleID]
		if !exists {
			return nil, fmt.Errorf("permission %q role %q: %w", permission.ID, permission.RoleID, ErrUnknownRole)
		}
		if role.Tenant != permission.Tenant {
			return nil, fmt.Errorf("permission %q: %w", permission.ID, ErrRoleTenantMismatch)
		}
		permissionIDs[permission.ID] = struct{}{}
		evaluator.permissions[permission.RoleID] = append(
			evaluator.permissions[permission.RoleID],
			permission,
		)
	}
	assignmentIDs := make(map[authorization.PolicyID]struct{}, len(assignments))
	for index, assignment := range assignments {
		if assignment.ID == "" || assignment.Subject.Kind == "" ||
			assignment.Subject.ID == "" || assignment.RoleID == "" {
			return nil, fmt.Errorf("assignment %d: %w", index, ErrInvalidAssignment)
		}
		if _, exists := assignmentIDs[assignment.ID]; exists {
			return nil, fmt.Errorf("assignment %q: %w", assignment.ID, ErrDuplicateAssignment)
		}
		role, exists := evaluator.roles[assignment.RoleID]
		if !exists {
			return nil, fmt.Errorf("assignment %q role %q: %w", assignment.ID, assignment.RoleID, ErrUnknownRole)
		}
		if role.Tenant != assignment.Tenant {
			return nil, fmt.Errorf("assignment %q: %w", assignment.ID, ErrRoleTenantMismatch)
		}
		assignment.Subject.Groups = append([]authorization.SubjectID(nil), assignment.Subject.Groups...)
		assignmentIDs[assignment.ID] = struct{}{}
		key := subjectKey{kind: assignment.Subject.Kind, id: assignment.Subject.ID}
		evaluator.assignments[key] = append(evaluator.assignments[key], assignment)
	}

	return evaluator, nil
}

func (evaluator *Evaluator) Evaluate(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	decision := authorization.Decision{Outcome: authorization.NotApplicable}
	permissions, err := evaluator.EffectivePermissions(ctx, request.Subject, request.Tenant)
	if err != nil {
		reason := ReasonLimitExceeded
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reason = authorization.ReasonContextCanceled
		}
		return authorization.Decision{Outcome: authorization.Deny, Reason: reason}, err
	}

	for _, permission := range permissions {
		if permission.Action != request.Action ||
			permission.ResourceType != request.Resource.Type ||
			(permission.ResourceID != "" && permission.ResourceID != request.Resource.ID) {
			continue
		}
		if len(decision.MatchedPolicyIDs) >= evaluator.limits.MaxMatches {
			return authorization.Decision{
				Outcome: authorization.Deny,
				Reason:  ReasonLimitExceeded,
			}, ErrMatchLimitExceeded
		}

		decision.MatchedPolicyIDs = append(decision.MatchedPolicyIDs, permission.ID)
		if permission.Effect == authorization.Deny {
			decision.Outcome = authorization.Deny
			decision.Reason = ReasonExplicitDeny
		} else if decision.Outcome == authorization.NotApplicable {
			decision.Outcome = authorization.Allow
			decision.Reason = ReasonAllow
		}
	}

	return decision, nil
}

func (evaluator *Evaluator) EffectivePermissions(
	ctx context.Context,
	subject authorization.Subject,
	tenant authorization.TenantID,
) ([]Permission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(subject.Groups) > evaluator.limits.MaxGroups {
		return nil, ErrGroupLimitExceeded
	}

	principals := []authorization.Subject{subject}
	for _, groupID := range subject.Groups {
		principals = append(principals, authorization.Subject{
			Kind: authorization.SubjectGroup,
			ID:   groupID,
		})
	}

	roleIDs := make(map[RoleID]struct{})
	for _, principal := range principals {
		key := subjectKey{kind: principal.Kind, id: principal.ID}
		for _, assignment := range evaluator.assignments[key] {
			if evaluator.matchesTenant(assignment.Tenant, tenant) {
				evaluator.addRoleClosure(assignment.RoleID, roleIDs)
			}
		}
	}

	permissions := make([]Permission, 0)
	for roleID := range roleIDs {
		permissions = append(permissions, evaluator.permissions[roleID]...)
	}
	sort.Slice(permissions, func(left, right int) bool {
		if permissions[left].Priority == permissions[right].Priority {
			return permissions[left].ID < permissions[right].ID
		}

		return permissions[left].Priority > permissions[right].Priority
	})

	return permissions, nil
}

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

func (evaluator *Evaluator) matchesTenant(
	policyTenant authorization.TenantID,
	requestTenant authorization.TenantID,
) bool {
	if policyTenant == requestTenant {
		return true
	}

	return evaluator.inheritGlobal && policyTenant == "" && requestTenant != ""
}

func (evaluator *Evaluator) addRoleClosure(roleID RoleID, roles map[RoleID]struct{}) {
	if _, seen := roles[roleID]; seen {
		return
	}
	roles[roleID] = struct{}{}
	for _, parentID := range evaluator.roles[roleID].Parents {
		evaluator.addRoleClosure(parentID, roles)
	}
}

func (evaluator *Evaluator) validateInheritance() error {
	for _, role := range evaluator.roles {
		for _, parentID := range role.Parents {
			parent, exists := evaluator.roles[parentID]
			if !exists {
				return fmt.Errorf("role %q parent %q: %w", role.ID, parentID, ErrUnknownParentRole)
			}
			if parent.Tenant != role.Tenant {
				return fmt.Errorf("role %q parent %q: %w", role.ID, parentID, ErrCrossTenantInheritance)
			}
		}
	}

	states := make(map[RoleID]uint8, len(evaluator.roles))
	var visit func(RoleID) error
	visit = func(roleID RoleID) error {
		if states[roleID] == 1 {
			return ErrRoleCycle
		}
		if states[roleID] == 2 {
			return nil
		}
		states[roleID] = 1
		for _, parentID := range evaluator.roles[roleID].Parents {
			if err := visit(parentID); err != nil {
				return err
			}
		}
		states[roleID] = 2
		return nil
	}
	for roleID := range evaluator.roles {
		if err := visit(roleID); err != nil {
			return err
		}
	}

	depths := make(map[RoleID]int, len(evaluator.roles))
	var depth func(RoleID) int
	depth = func(roleID RoleID) int {
		if known, exists := depths[roleID]; exists {
			return known
		}
		maximum := 0
		for _, parentID := range evaluator.roles[roleID].Parents {
			maximum = max(maximum, 1+depth(parentID))
		}
		depths[roleID] = maximum
		return maximum
	}
	for roleID := range evaluator.roles {
		if depth(roleID) > evaluator.limits.MaxInheritanceDepth {
			return fmt.Errorf("role %q: %w", roleID, ErrInheritanceDepthExceeded)
		}
	}

	return nil
}
