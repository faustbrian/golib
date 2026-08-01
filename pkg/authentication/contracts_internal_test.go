package authentication

import "testing"

func TestResultPrincipalRequiresAuthenticatedConcreteIdentity(t *testing.T) {
	t.Parallel()

	principal, err := NewPrincipal(PrincipalSpec{Subject: "service", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}

	tests := map[string]Result{
		"authenticated anonymous": {state: ResultAuthenticated, principal: AnonymousPrincipal()},
		"anonymous concrete":      {state: ResultAnonymous, principal: principal},
	}
	for name, result := range tests {
		result := result
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, ok := result.Principal(); ok {
				t.Fatalf("Principal() = (%v, true), want unauthenticated", got)
			}
		})
	}
}
