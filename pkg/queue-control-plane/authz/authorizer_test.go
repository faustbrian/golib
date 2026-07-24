package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/authn"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestAuthorizerMapsAuthenticatedPrincipalToTenantDecision(t *testing.T) {
	t.Parallel()

	principal := authenticatedPrincipal(t)
	ctx := authentication.ContextWithPrincipal(context.Background(), principal)
	evaluator := &decisionMakerStub{decision: authorization.Decision{Outcome: authorization.Allow}}
	authorizer, err := New(evaluator, authorization.SubjectUser)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	target := controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"}

	if err := authorizer.Authorize(
		ctx,
		"tenant-1",
		principal.Subject(),
		controlplane.PermissionDrain,
		target,
	); err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	want := authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   authorization.SubjectID(principal.Subject()),
		},
		Action:   authorization.Action(controlplane.PermissionDrain),
		Resource: authorization.Resource{Type: "worker_group", ID: "payments"},
		Tenant:   "tenant-1",
	}
	if evaluator.request.Subject.Kind != want.Subject.Kind ||
		evaluator.request.Subject.ID != want.Subject.ID ||
		evaluator.request.Action != want.Action ||
		evaluator.request.Resource.Type != want.Resource.Type ||
		evaluator.request.Resource.ID != want.Resource.ID ||
		evaluator.request.Tenant != want.Tenant {
		t.Fatalf("decision request = %+v, want %+v", evaluator.request, want)
	}
}

func TestAuthorizerFailsClosed(t *testing.T) {
	t.Parallel()

	principal := authenticatedPrincipal(t)
	authenticated := authentication.ContextWithPrincipal(context.Background(), principal)
	evaluationErr := errors.New("policy unavailable")
	tests := map[string]struct {
		ctx       context.Context
		actor     string
		evaluator *decisionMakerStub
		wantErr   error
		wantCalls int
	}{
		"missing principal": {
			ctx:       context.Background(),
			actor:     principal.Subject(),
			evaluator: &decisionMakerStub{},
			wantErr:   ErrUnauthenticated,
		},
		"spoofed actor": {
			ctx:       authenticated,
			actor:     "another-operator",
			evaluator: &decisionMakerStub{},
			wantErr:   ErrActorMismatch,
		},
		"denied": {
			ctx:       authenticated,
			actor:     principal.Subject(),
			evaluator: &decisionMakerStub{decision: authorization.Decision{Outcome: authorization.Deny}},
			wantErr:   ErrDenied,
			wantCalls: 1,
		},
		"not applicable": {
			ctx:       authenticated,
			actor:     principal.Subject(),
			evaluator: &decisionMakerStub{},
			wantErr:   ErrDenied,
			wantCalls: 1,
		},
		"evaluation error": {
			ctx:       authenticated,
			actor:     principal.Subject(),
			evaluator: &decisionMakerStub{err: evaluationErr},
			wantErr:   evaluationErr,
			wantCalls: 1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			authorizer, err := New(tt.evaluator, authorization.SubjectUser)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = authorizer.Authorize(
				tt.ctx,
				"tenant-1",
				tt.actor,
				controlplane.PermissionPause,
				controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authorize() error = %v, want %v", err, tt.wantErr)
			}
			if tt.evaluator.calls != tt.wantCalls {
				t.Fatalf("Decide() calls = %d, want %d", tt.evaluator.calls, tt.wantCalls)
			}
		})
	}
}

func TestNewAuthorizerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		evaluator DecisionMaker
		kind      authorization.SubjectKind
	}{
		"evaluator": {kind: authorization.SubjectUser},
		"subject kind": {
			evaluator: &decisionMakerStub{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(tt.evaluator, tt.kind)
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("New() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestAuthorizerFailsClosedWhenPrincipalMappingFails(t *testing.T) {
	t.Parallel()

	mappingErr := errors.New("principal mapping failed")
	authorizer, err := New(decisionMakerValue{}, authorization.SubjectUser)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	authorizer.mapSubject = func(
		authn.Principal,
		authn.Config,
	) (authorization.Subject, error) {
		return authorization.Subject{}, mappingErr
	}
	principal := authenticatedPrincipal(t)
	ctx := authentication.ContextWithPrincipal(context.Background(), principal)

	err = authorizer.Authorize(
		ctx,
		"tenant-1",
		principal.Subject(),
		controlplane.PermissionPause,
		controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
	)
	if !errors.Is(err, mappingErr) {
		t.Fatalf("Authorize() error = %v, want %v", err, mappingErr)
	}
}

func TestNewAuthorizerRejectsTypedNilEvaluator(t *testing.T) {
	t.Parallel()

	var evaluator *decisionMakerStub
	_, err := New(evaluator, authorization.SubjectUser)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("New() error = %v, want ErrInvalidConfiguration", err)
	}
}

func authenticatedPrincipal(t *testing.T) authentication.Principal {
	t.Helper()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject:         "operator-1",
		Method:          "bearer",
		AuthenticatedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}

	return principal
}

type decisionMakerStub struct {
	decision authorization.Decision
	err      error
	request  authorization.Request
	calls    int
}

type decisionMakerValue struct{}

func (decisionMakerValue) Decide(
	context.Context,
	authorization.Request,
) (authorization.Decision, error) {
	return authorization.Decision{Outcome: authorization.Allow}, nil
}

func (s *decisionMakerStub) Decide(
	_ context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	s.calls++
	s.request = request

	return s.decision, s.err
}
