// Package authz integrates control-plane mutations with authorization.
package authz

import (
	"context"
	"errors"
	"reflect"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/authn"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

var (
	// ErrInvalidConfiguration reports an unusable authorization dependency.
	ErrInvalidConfiguration = errors.New("control-plane authorization: invalid configuration")
	// ErrUnauthenticated reports a missing authenticated principal.
	ErrUnauthenticated = errors.New("control-plane authorization: unauthenticated")
	// ErrActorMismatch reports an actor that differs from the authenticated ID.
	ErrActorMismatch = errors.New("control-plane authorization: actor mismatch")
	// ErrDenied reports a fail-closed non-allow decision.
	ErrDenied = errors.New("control-plane authorization: denied")
)

// DecisionMaker is the stable authorization evaluation seam.
type DecisionMaker interface {
	Decide(context.Context, authorization.Request) (authorization.Decision, error)
}

// Authorizer maps authenticated identities into tenant-scoped decisions.
type Authorizer struct {
	evaluator   DecisionMaker
	subjectKind authorization.SubjectKind
	mapSubject  func(authn.Principal, authn.Config) (authorization.Subject, error)
}

// New creates a fail-closed control-plane authorizer.
func New(evaluator DecisionMaker, subjectKind authorization.SubjectKind) (*Authorizer, error) {
	if nilInterface(evaluator) || !validSubjectKind(subjectKind) {
		return nil, ErrInvalidConfiguration
	}

	return &Authorizer{
		evaluator:   evaluator,
		subjectKind: subjectKind,
		mapSubject:  authn.Subject,
	}, nil
}

// Authorize verifies actor attribution and evaluates the requested permission.
func (a *Authorizer) Authorize(
	ctx context.Context,
	tenant string,
	actor string,
	permission controlplane.Permission,
	target controlplane.Target,
) error {
	principal, ok := authentication.PrincipalFromContext(ctx)
	if !ok || principal.IsAnonymous() {
		return ErrUnauthenticated
	}
	if principal.Subject() != actor {
		return ErrActorMismatch
	}

	subject, err := a.mapSubject(principal, authn.Config{Kind: a.subjectKind})
	if err != nil {
		return err
	}
	decision, err := a.evaluator.Decide(ctx, authorization.Request{
		Subject: subject,
		Action:  authorization.Action(permission),
		Resource: authorization.Resource{
			Type: authorization.ResourceType(target.Kind),
			ID:   authorization.ResourceID(target.Name),
		},
		Tenant: authorization.TenantID(tenant),
	})
	if err != nil {
		return err
	}
	if decision.Outcome != authorization.Allow {
		return ErrDenied
	}

	return nil
}

func validSubjectKind(kind authorization.SubjectKind) bool {
	switch kind {
	case authorization.SubjectUser,
		authorization.SubjectServiceAccount,
		authorization.SubjectAPIKey,
		authorization.SubjectGroup:
		return true
	default:
		return false
	}
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		return reflected.IsNil()
	}

	return false
}
