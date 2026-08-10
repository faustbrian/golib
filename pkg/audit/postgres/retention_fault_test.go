package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/jackc/pgx/v5"
)

func TestRetentionAdminValidationAndEventFaults(t *testing.T) {
	t.Parallel()

	if _, err := NewRetentionAdmin(nil, Config{}); !errors.Is(err, ErrPoolRequired) {
		t.Fatalf("NewRetentionAdmin(nil) error = %v", err)
	}
	event := faultRetentionEvent(t)
	var nilAdmin *RetentionAdmin
	if _, err := nilAdmin.AppendRetentionEvent(context.Background(), event); audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("nil admin event error = %v", err)
	}
	admin := &RetentionAdmin{pool: &fakeDatabase{}, limits: audit.DefaultLimits()}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := admin.AppendRetentionEvent(nil, event); audit.AppendOutcomeOf(err) != audit.AppendRejected { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context event error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := admin.AppendRetentionEvent(canceled, event); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled event error = %v", err)
	}
	beginFailure := errors.New("begin failed")
	admin.pool = &fakeDatabase{beginErr: beginFailure}
	if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, beginFailure) {
		t.Fatalf("begin-failed event error = %v", err)
	}
	insertFailure := errors.New("insert failed")
	tx := &fakeTx{rows: []pgx.Row{fakeRow{scan: func(...any) error { return insertFailure }}}}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, insertFailure) || !tx.rollbackCalled {
		t.Fatalf("insert-failed event = %v, rollback=%t", err, tx.rollbackCalled)
	}
	reconcileFailure := errors.New("reconcile failed")
	tx = &fakeTx{rows: []pgx.Row{
		fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
		fakeRow{scan: func(...any) error { return reconcileFailure }},
	}}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, reconcileFailure) {
		t.Fatalf("reconcile-failed event error = %v", err)
	}
	tx = &fakeTx{rows: []pgx.Row{
		fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
		retentionEventRow("different", "hold", "legal_case", event.OccurredAt()),
	}}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, audit.ErrDuplicateConflict) {
		t.Fatalf("conflicting event error = %v", err)
	}
	for name, row := range map[string]pgx.Row{
		"kind":   retentionEventRow(event.RecordID(), "release", event.ReasonCode(), event.OccurredAt()),
		"reason": retentionEventRow(event.RecordID(), "hold", "different", event.OccurredAt()),
		"time":   retentionEventRow(event.RecordID(), "hold", event.ReasonCode(), event.OccurredAt().Add(time.Microsecond)),
	} {
		tx = &fakeTx{rows: []pgx.Row{
			fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
			row,
		}}
		admin.pool = &fakeDatabase{tx: tx}
		if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, audit.ErrDuplicateConflict) {
			t.Fatalf("conflicting %s event error = %v", name, err)
		}
	}
	tx = &fakeTx{rows: []pgx.Row{
		fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
		retentionEventRow(event.RecordID(), "hold", event.ReasonCode(), event.OccurredAt().Truncate(time.Microsecond)),
	}}
	admin.pool = &fakeDatabase{tx: tx}
	result, err := admin.AppendRetentionEvent(context.Background(), event)
	if err != nil || result.Status != audit.AppendDuplicate {
		t.Fatalf("duplicate event = %#v, %v", result, err)
	}
	commitFailure := errors.New("commit failed")
	tx = &fakeTx{rows: []pgx.Row{acceptedRow(event.ID())}, commitErr: commitFailure}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.AppendRetentionEvent(context.Background(), event); !errors.Is(err, commitFailure) || audit.AppendOutcomeOf(err) != audit.AppendUnknown {
		t.Fatalf("commit-failed event error = %v", err)
	}
}

func TestRetentionPlanningFaultsAndTenantScopes(t *testing.T) {
	t.Parallel()

	request := faultRetentionRequest(t, audit.AllTenants())
	var nilAdmin *RetentionAdmin
	if _, err := nilAdmin.PlanRetention(context.Background(), request); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil PlanRetention() error = %v", err)
	}
	admin := &RetentionAdmin{pool: &fakeDatabase{}, limits: audit.DefaultLimits()}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := admin.PlanRetention(nil, request); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context PlanRetention() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	admin.pool = &fakeDatabase{rows: &fakeRows{}}
	if _, err := admin.PlanRetention(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled PlanRetention() error = %v", err)
	}
	queryFailure := errors.New("query failed")
	admin.pool = &fakeDatabase{queryErr: queryFailure}
	if _, err := admin.PlanRetention(context.Background(), request); !errors.Is(err, queryFailure) {
		t.Fatalf("query-failed PlanRetention() error = %v", err)
	}

	record := faultRecord(t, "record-1")
	canonical, _ := audit.CanonicalJSON(record)
	digest := sha256.Sum256(canonical)
	for _, test := range []struct {
		name string
		rows *fakeRows
		want error
	}{
		{"scan", &fakeRows{values: [][]byte{{1}}, scanErr: errors.New("scan failed")}, errors.New("scan failed")},
		{"canonical", planRows([]byte("invalid"), digest[:]), audit.ErrInvalidArgument},
		{"digest", planRows(canonical, []byte{1}), audit.ErrInvalidArgument},
		{"iterate", &fakeRows{err: errors.New("iterate failed")}, errors.New("iterate failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			admin.pool = &fakeDatabase{rows: test.rows}
			_, err := admin.PlanRetention(context.Background(), request)
			if err == nil || (errors.Is(test.want, audit.ErrInvalidArgument) && !errors.Is(err, audit.ErrInvalidArgument)) {
				t.Fatalf("PlanRetention() error = %v", err)
			}
		})
	}

	for _, scope := range []audit.TenantScope{audit.NoTenant(), audit.AllTenants()} {
		database := &fakeDatabase{rows: &fakeRows{}}
		admin.pool = database
		if _, err := admin.PlanRetention(context.Background(), faultRetentionRequest(t, scope)); err != nil {
			t.Fatal(err)
		}
	}
	tenant, _ := audit.Tenant("tenant-1")
	database := &fakeDatabase{rows: planRows(canonical, digest[:])}
	admin.pool = database
	plan, err := admin.PlanRetention(context.Background(), faultRetentionRequest(t, tenant))
	if err != nil || len(plan.Candidates()) != 1 {
		t.Fatalf("exact-tenant plan = %#v, %v", plan, err)
	}
}

func TestRetentionApplyReconcilesEveryBoundary(t *testing.T) {
	t.Parallel()

	var nilAdmin *RetentionAdmin
	if _, err := nilAdmin.ApplyRetention(context.Background(), audit.RetentionPlan{}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil ApplyRetention() error = %v", err)
	}
	admin := &RetentionAdmin{pool: &fakeDatabase{}, limits: audit.DefaultLimits()}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := admin.ApplyRetention(nil, audit.RetentionPlan{}); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context ApplyRetention() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := admin.ApplyRetention(canceled, audit.RetentionPlan{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled empty ApplyRetention() error = %v", err)
	}
	if result, err := admin.ApplyRetention(context.Background(), audit.RetentionPlan{}); err != nil || result != (audit.RetentionApplyResult{}) {
		t.Fatalf("empty ApplyRetention() = %#v, %v", result, err)
	}
	plan, _ := faultRetentionPlan(t, 1)
	beginFailure := errors.New("begin failed")
	admin.pool = &fakeDatabase{beginErr: beginFailure}
	if _, err := admin.ApplyRetention(context.Background(), plan); !errors.Is(err, beginFailure) {
		t.Fatalf("begin-failed ApplyRetention() error = %v", err)
	}
	pruneFailure := errors.New("prune failed")
	tx := &fakeTx{rows: []pgx.Row{fakeRow{scan: func(...any) error { return pruneFailure }}}}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.ApplyRetention(context.Background(), plan); !errors.Is(err, pruneFailure) {
		t.Fatalf("prune-failed ApplyRetention() error = %v", err)
	}

	plan, digest := faultRetentionPlan(t, 5)
	reconcileFailure := errors.New("reconcile failed")
	tx = &fakeTx{rows: []pgx.Row{
		boolRow(true),
		boolRow(false), fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
		boolRow(false), digestStateRow(digest, "hold"),
		boolRow(false), digestStateRow([]byte("different"), "release"),
		boolRow(false), fakeRow{scan: func(...any) error { return reconcileFailure }},
	}}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.ApplyRetention(context.Background(), plan); !errors.Is(err, reconcileFailure) {
		t.Fatalf("reconcile-failed ApplyRetention() error = %v", err)
	}

	plan, digest = faultRetentionPlan(t, 5)
	tx = &fakeTx{rows: []pgx.Row{
		boolRow(true),
		boolRow(false), fakeRow{scan: func(...any) error { return pgx.ErrNoRows }},
		boolRow(false), digestStateRow(digest, "hold"),
		boolRow(false), digestStateRow([]byte("different"), "release"),
		boolRow(false), digestStateRow(digest, "release"),
	}}
	admin.pool = &fakeDatabase{tx: tx}
	result, err := admin.ApplyRetention(context.Background(), plan)
	if err != nil || result.Deleted != 1 || result.Changed != 3 || result.Held != 1 {
		t.Fatalf("ApplyRetention() = %#v, %v", result, err)
	}
	commitFailure := errors.New("commit failed")
	plan, _ = faultRetentionPlan(t, 1)
	tx = &fakeTx{rows: []pgx.Row{boolRow(true)}, commitErr: commitFailure}
	admin.pool = &fakeDatabase{tx: tx}
	if _, err := admin.ApplyRetention(context.Background(), plan); !errors.Is(err, commitFailure) || audit.AppendOutcomeOf(err) != audit.AppendUnknown {
		t.Fatalf("commit-failed ApplyRetention() error = %v", err)
	}
}

func faultRetentionEvent(t *testing.T) audit.RetentionEvent {
	t.Helper()
	event, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "hold-1", RecordID: "record-1", ReasonCode: "legal_case",
		Kind: audit.RetentionHold, OccurredAt: time.Date(2026, time.August, 9, 12, 0, 0, 123, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func faultRetentionRequest(t *testing.T, scope audit.TenantScope) audit.RetentionRequest {
	t.Helper()
	request, err := audit.NewRetentionRequest(audit.RetentionRequestInput{Tenant: scope, Before: time.Now(), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func faultRetentionPlan(t *testing.T, count int) (audit.RetentionPlan, []byte) {
	t.Helper()
	candidates := make([]audit.RetentionCandidate, count)
	var digest [sha256.Size]byte
	for index := range candidates {
		record := faultRecord(t, "record-"+string(rune('a'+index)))
		canonical, err := audit.CanonicalJSON(record)
		if err != nil {
			t.Fatal(err)
		}
		digest = sha256.Sum256(canonical)
		candidate, err := audit.NewRetentionCandidate(record, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		candidates[index] = candidate
	}
	plan, err := audit.NewRetentionPlan(candidates)
	if err != nil {
		t.Fatal(err)
	}
	return plan, digest[:]
}

func retentionEventRow(recordID, kind, reason string, occurredAt time.Time) pgx.Row {
	return fakeRow{scan: func(destinations ...any) error {
		*(destinations[0].(*string)) = recordID
		*(destinations[1].(*string)) = kind
		*(destinations[2].(*string)) = reason
		*(destinations[3].(*time.Time)) = occurredAt
		return nil
	}}
}

func planRows(canonical, digest []byte) *fakeRows {
	return &fakeRows{values: [][]byte{{1}}, scan: func(destinations ...any) error {
		*(destinations[0].(*[]byte)) = append([]byte(nil), canonical...)
		*(destinations[1].(*[]byte)) = append([]byte(nil), digest...)
		return nil
	}}
}

func boolRow(value bool) pgx.Row {
	return fakeRow{scan: func(destinations ...any) error { *(destinations[0].(*bool)) = value; return nil }}
}

func digestStateRow(digest []byte, state string) pgx.Row {
	return fakeRow{scan: func(destinations ...any) error {
		*(destinations[0].(*[]byte)) = append([]byte(nil), digest...)
		*(destinations[1].(*string)) = state
		return nil
	}}
}
