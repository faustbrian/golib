package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestStaticAccessRejectsInvalidKeysAndACL(t *testing.T) {
	t.Parallel()

	validKeys := []apikey.Entry{{
		ID: "key-1", Key: "secret-1",
		Principal: authentication.PrincipalSpec{Subject: "operator-1"},
	}}
	for _, input := range []struct {
		keys    []apikey.Entry
		entries []acl.Entry
	}{
		{},
		{keys: validKeys, entries: []acl.Entry{{}}},
	} {
		access, err := NewStaticAccess(input.keys, input.entries)
		if access != nil || err == nil {
			t.Fatalf("NewStaticAccess() = (%v, %v), want configuration error", access, err)
		}
	}
}

func TestMustPanicsOnBrokenProgrammerInvariant(t *testing.T) {
	t.Parallel()

	want := errors.New("invariant failed")
	defer func() {
		if recovered := recover(); !errors.Is(recovered.(error), want) {
			t.Fatalf("must() panic = %v", recovered)
		}
	}()
	_ = must(0, want)
}

func TestStaticAccessAuthenticatesAndAuthorizesTenantACL(t *testing.T) {
	t.Parallel()

	access, err := NewStaticAccess(
		[]apikey.Entry{{
			ID: "key-1", Key: "secret-1",
			Principal: authentication.PrincipalSpec{Subject: "operator-1"},
		}},
		[]acl.Entry{{
			ID: "view-fleet",
			Subject: authorization.Subject{
				Kind: authorization.SubjectAPIKey,
				ID:   "operator-1",
			},
			Action:       authorization.Action(controlplane.PermissionView),
			ResourceType: authorization.ResourceType(controlplane.TargetWorkload),
			ResourceID:   "fleet",
			Tenant:       "tenant-1",
			Effect:       authorization.Allow,
		}},
	)
	if err != nil {
		t.Fatalf("NewStaticAccess() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(APIKeyIDHeader, "key-1")
	request.Header.Set(APIKeySecretHeader, "secret-1")
	credential, err := access.Extractor.Extract(request)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	result, err := access.Authenticator.Authenticate(context.Background(), credential)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok {
		t.Fatal("Authenticate() result has no principal")
	}
	ctx := authentication.ContextWithPrincipal(context.Background(), principal)
	if err := access.Authorizer.Authorize(
		ctx,
		"tenant-1",
		"operator-1",
		controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "fleet"},
	); err != nil {
		t.Fatalf("Authorize(view) error = %v", err)
	}
	if err := access.Authorizer.Authorize(
		ctx,
		"tenant-1",
		"operator-1",
		controlplane.PermissionPause,
		controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
	); err == nil {
		t.Fatal("Authorize(pause) error = nil, want deny")
	}
}

func TestStaticAccessEnforcesEveryPermissionTenantAndObjectScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		permission controlplane.Permission
		target     controlplane.Target
	}{
		{controlplane.PermissionView, controlplane.Target{Kind: controlplane.TargetWorkload, Name: "fleet"}},
		{controlplane.PermissionPause, controlplane.Target{Kind: controlplane.TargetQueue, Name: "pause-target"}},
		{controlplane.PermissionResume, controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "resume-target"}},
		{controlplane.PermissionDrain, controlplane.Target{Kind: controlplane.TargetWorker, Name: "drain-target"}},
		{controlplane.PermissionTerminate, controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "terminate-target"}},
		{controlplane.PermissionRetry, controlplane.Target{Kind: controlplane.TargetFailure, Name: "retry-target"}},
		{controlplane.PermissionBulkRetry, controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: "bulk-retry-target"}},
		{controlplane.PermissionDelete, controlplane.Target{Kind: controlplane.TargetFailure, Name: "delete-target"}},
		{controlplane.PermissionPurge, controlplane.Target{Kind: controlplane.TargetQueue, Name: "purge-target"}},
		{controlplane.PermissionReplay, controlplane.Target{Kind: controlplane.TargetFailure, Name: "replay-source"}},
		{controlplane.PermissionReplay, controlplane.Target{Kind: controlplane.TargetQueue, Name: "replay-destination"}},
		{controlplane.PermissionScale, controlplane.Target{Kind: controlplane.TargetWorkload, Name: "scale-target"}},
		{controlplane.PermissionRecordList, controlplane.Target{Kind: controlplane.TargetFailure, Name: "failures"}},
		{controlplane.PermissionRecordInspect, controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: "inspect-target"}},
		{controlplane.PermissionPayloadView, controlplane.Target{Kind: controlplane.TargetFailure, Name: "payload-target"}},
		{controlplane.PermissionDiagnosticsView, controlplane.Target{Kind: controlplane.TargetFailure, Name: "diagnostics-target"}},
		{controlplane.PermissionAuditView, controlplane.Target{Kind: controlplane.TargetWorkload, Name: "audit"}},
		{controlplane.PermissionRetentionConfigure, controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}},
	}
	entries := make([]acl.Entry, 0, len(tests))
	for index, test := range tests {
		entries = append(entries, acl.Entry{
			ID: acl.EntryID(fmt.Sprintf("permission-%d", index)),
			Subject: authorization.Subject{
				Kind: authorization.SubjectAPIKey,
				ID:   "operator-1",
			},
			Action:       authorization.Action(test.permission),
			ResourceType: authorization.ResourceType(test.target.Kind),
			ResourceID:   authorization.ResourceID(test.target.Name),
			Tenant:       "tenant-1",
			Effect:       authorization.Allow,
		})
	}
	access, err := NewStaticAccess(
		[]apikey.Entry{{
			ID: "key-1", Key: "secret-1",
			Principal: authentication.PrincipalSpec{Subject: "operator-1"},
		}},
		entries,
	)
	if err != nil {
		t.Fatalf("NewStaticAccess() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(APIKeyIDHeader, "key-1")
	request.Header.Set(APIKeySecretHeader, "secret-1")
	credential, err := access.Extractor.Extract(request)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	result, err := access.Authenticator.Authenticate(context.Background(), credential)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok {
		t.Fatal("Authenticate() result has no principal")
	}
	ctx := authentication.ContextWithPrincipal(context.Background(), principal)

	for _, test := range tests {
		if err := access.Authorizer.Authorize(
			ctx, "tenant-1", "operator-1", test.permission, test.target,
		); err != nil {
			t.Fatalf("Authorize(%q, %+v) error = %v", test.permission, test.target, err)
		}
		for name, denied := range map[string]struct {
			tenant string
			target controlplane.Target
		}{
			"tenant": {tenant: "tenant-2", target: test.target},
			"object": {
				tenant: "tenant-1",
				target: controlplane.Target{Kind: test.target.Kind, Name: "another-object"},
			},
		} {
			t.Run(string(test.permission)+"/"+string(test.target.Kind)+"/"+name, func(t *testing.T) {
				t.Parallel()

				if err := access.Authorizer.Authorize(
					ctx, denied.tenant, "operator-1", test.permission, denied.target,
				); err == nil {
					t.Fatal("Authorize() error = nil, want scoped denial")
				}
			})
		}
	}
}
