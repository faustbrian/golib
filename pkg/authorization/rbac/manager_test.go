package rbac_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func TestManagerAssignInspectRevokeAndSnapshot(t *testing.T) {
	t.Parallel()

	role := rbac.Role{ID: "reader", Tenant: "tenant-1"}
	permission := rbac.Permission{
		ID:           "read",
		RoleID:       role.ID,
		Tenant:       role.Tenant,
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}
	manager, err := rbac.NewManager([]rbac.Role{role}, []rbac.Permission{permission}, nil)
	if err != nil {
		t.Fatalf("rbac.NewManager() error = %v", err)
	}

	assignment := rbac.Assignment{
		ID:      "alice-reader",
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		RoleID:  role.ID,
		Tenant:  role.Tenant,
	}
	if err := manager.Assign(assignment); err != nil {
		t.Fatalf("Manager.Assign() error = %v", err)
	}
	if manager.Revision() != 2 {
		t.Errorf("Manager.Revision() = %d, want 2", manager.Revision())
	}

	assignments := manager.Assignments(assignment.Subject, assignment.Tenant)
	if len(assignments) != 1 || assignments[0].ID != assignment.ID {
		t.Errorf("Manager.Assignments() = %+v, want alice-reader", assignments)
	}

	if err := manager.Assign(assignment); !errors.Is(err, rbac.ErrDuplicateAssignment) {
		t.Errorf("duplicate Manager.Assign() error = %v, want ErrDuplicateAssignment", err)
	}
	if manager.Revision() != 2 {
		t.Errorf("revision changed after rejected assignment: %d", manager.Revision())
	}

	evaluator, err := manager.Evaluator()
	if err != nil {
		t.Fatalf("Manager.Evaluator() error = %v", err)
	}
	decision, err := evaluator.Evaluate(
		context.Background(),
		request("document-1", "tenant-1"),
	)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Allow {
		t.Errorf("Decision.Outcome = %v, want Allow", decision.Outcome)
	}

	if !manager.RevokeAssignment(assignment.ID) {
		t.Error("Manager.RevokeAssignment() = false, want true")
	}
	if manager.RevokeAssignment(assignment.ID) {
		t.Error("second Manager.RevokeAssignment() = true, want false")
	}
	if manager.Revision() != 3 {
		t.Errorf("Manager.Revision() = %d, want 3", manager.Revision())
	}

	evaluator, err = manager.Evaluator()
	if err != nil {
		t.Fatalf("Manager.Evaluator() error = %v", err)
	}
	decision, err = evaluator.Evaluate(
		context.Background(),
		request("document-1", "tenant-1"),
	)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.NotApplicable {
		t.Errorf("revoked Decision.Outcome = %v, want NotApplicable", decision.Outcome)
	}
}

func TestManagerRejectsInvalidInitialStateAndRevokesByID(t *testing.T) {
	t.Parallel()

	if _, err := rbac.NewManager([]rbac.Role{{}}, nil, nil); !errors.Is(err, rbac.ErrInvalidRole) {
		t.Errorf("rbac.NewManager() error = %v, want ErrInvalidRole", err)
	}

	role := rbac.Role{ID: "reader"}
	first := rbac.Assignment{
		ID:      "first",
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		RoleID:  role.ID,
	}
	second := rbac.Assignment{
		ID:      "second",
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "bob"},
		RoleID:  role.ID,
	}
	manager, err := rbac.NewManager([]rbac.Role{role}, nil, []rbac.Assignment{first, second})
	if err != nil {
		t.Fatalf("rbac.NewManager() error = %v", err)
	}
	if !manager.RevokeAssignment(second.ID) {
		t.Error("Manager.RevokeAssignment(second) = false, want true")
	}
	if assignments := manager.Assignments(second.Subject, ""); len(assignments) != 0 {
		t.Errorf("revoked assignments = %+v, want empty", assignments)
	}
}

func TestManagerSupportsConcurrentAdministrationAndInspection(t *testing.T) {
	t.Parallel()

	role := rbac.Role{ID: "reader", Tenant: "tenant-1"}
	manager, err := rbac.NewManager([]rbac.Role{role}, nil, nil)
	if err != nil {
		t.Fatalf("rbac.NewManager() error = %v", err)
	}

	const readers = 4
	const updates = 100
	start := make(chan struct{})
	errorsByOperation := make(chan error, readers+1)
	var ready sync.WaitGroup
	var finished sync.WaitGroup
	ready.Add(readers + 1)
	finished.Add(readers + 1)

	go func() {
		defer finished.Done()
		ready.Done()
		<-start
		for index := range updates {
			assignment := rbac.Assignment{
				ID:      authorization.PolicyID(fmt.Sprintf("assignment-%d", index)),
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
				RoleID:  role.ID,
				Tenant:  role.Tenant,
			}
			if err := manager.Assign(assignment); err != nil {
				errorsByOperation <- err
				return
			}
			if !manager.RevokeAssignment(assignment.ID) {
				errorsByOperation <- errors.New("concurrent assignment was not revoked")
				return
			}
		}
	}()

	for range readers {
		go func() {
			defer finished.Done()
			ready.Done()
			<-start
			for range updates * 2 {
				if _, err := manager.Evaluator(); err != nil {
					errorsByOperation <- err
					return
				}
				_ = manager.Assignments(
					authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
					"tenant-1",
				)
				_ = manager.Revision()
			}
		}()
	}

	ready.Wait()
	close(start)
	finished.Wait()
	close(errorsByOperation)
	for operationErr := range errorsByOperation {
		t.Error(operationErr)
	}
	if manager.Revision() != 1+updates*2 {
		t.Errorf("Manager.Revision() = %d, want %d", manager.Revision(), 1+updates*2)
	}
}
