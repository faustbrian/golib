package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuditStorePersistsSensitiveAccessBeforeRecordRead(t *testing.T) {
	t.Parallel()

	zero := history.Hash{}
	tx := &pgxTransactionStub{
		execSteps: []execStep{
			{tag: pgconn.NewCommandTag("INSERT 0 1")},
			{tag: pgconn.NewCommandTag("INSERT 0 1")},
		},
		rows: []*rowStub{{values: []any{int64(0), zero[:]}}},
	}
	store := &AuditStore{beginner: &beginnerStub{tx: tx}}
	access := controlplane.SensitiveAccess{
		CommandID: "b0348d3e-f07b-4c22-986b-dce4e3a79021",
		TenantID:  "tenant-1", Actor: "operator-1",
		Permission: controlplane.PermissionPayloadView,
		Target:     controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"},
		OccurredAt: time.Unix(1, 0).UTC(),
	}
	if err := store.AuditSensitiveAccess(context.Background(), access); err != nil {
		t.Fatalf("AuditSensitiveAccess() error = %v", err)
	}
	if tx.commits != 1 || len(tx.execCalls) != 2 {
		t.Fatalf("transaction = commits %d execs %d", tx.commits, len(tx.execCalls))
	}
	args := tx.execCalls[1].args
	if args[0] != access.TenantID || args[3] != access.CommandID || args[4] != nil ||
		args[6] != access.Actor || args[7] != string(access.Permission) ||
		args[8] != "failure:failure-1" || args[9] != "authorized" {
		t.Fatalf("sensitive audit args = %#v", args)
	}
}

func TestNewAuditStoreRequiresTransactionBeginner(t *testing.T) {
	t.Parallel()

	store, err := NewAuditStore(nil)
	if !errors.Is(err, ErrNilBeginner) || store != nil {
		t.Fatalf("NewAuditStore(nil) = (%v, %v), want nil and ErrNilBeginner", store, err)
	}
}

func TestAuditStoreRejectsInvalidSensitiveAccessBeforeTransaction(t *testing.T) {
	t.Parallel()

	beginner := &beginnerStub{}
	store := &AuditStore{beginner: beginner}
	if err := store.AuditSensitiveAccess(
		context.Background(), controlplane.SensitiveAccess{},
	); err == nil {
		t.Fatal("AuditSensitiveAccess() error = nil")
	}
}

func TestAuditStoreListsBoundedTenantPage(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	first := history.Seal(anchor, auditEventForStore(5, time.Unix(5, 0)))
	second := history.Seal(first.Hash, auditEventForStore(6, time.Unix(6, 0)))
	tx := &pgxTransactionStub{rowSets: []*rowsStub{{records: [][]any{
		auditEntryRow(first), auditEntryRow(second),
	}}}}
	store, err := NewAuditStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	page, err := store.ListTenant(context.Background(), "tenant-1", 4, 1)
	if err != nil {
		t.Fatalf("ListTenant() error = %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0] != first || page.NextSequence != 5 {
		t.Fatalf("ListTenant() = %+v, want first entry and cursor 5", page)
	}
	if tx.commits != 1 || tx.rollbacks != 0 || len(tx.queryCalls) != 1 {
		t.Fatalf("calls = commit:%d rollback:%d query:%d", tx.commits, tx.rollbacks, len(tx.queryCalls))
	}
}

func TestAuditStoreListValidatesRequestAndChain(t *testing.T) {
	t.Parallel()

	store, err := NewAuditStore(&beginnerStub{})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	for _, input := range []struct {
		tenant string
		limit  uint32
	}{
		{tenant: "", limit: 1},
		{tenant: "tenant-1", limit: 0},
		{tenant: "tenant-1", limit: MaxAuditBatch + 1},
	} {
		if _, err := store.ListTenant(context.Background(), input.tenant, 0, input.limit); !errors.Is(err, ErrInvalidAuditRequest) {
			t.Fatalf("ListTenant(%q, %d) error = %v", input.tenant, input.limit, err)
		}
	}

	entry := history.Seal(history.HashBytes([]byte("retained")), auditEventForStore(2, time.Unix(2, 0)))
	entry.Event.Actor = "tampered"
	tampered, err := NewAuditStore(&beginnerStub{tx: &pgxTransactionStub{rowSets: []*rowsStub{{records: [][]any{auditEntryRow(entry)}}}}})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	if _, err := tampered.ListTenant(context.Background(), "tenant-1", 0, 1); !errors.Is(err, history.ErrAuditTampered) {
		t.Fatalf("ListTenant() error = %v, want ErrAuditTampered", err)
	}
}

func TestAuditStoreListHandlesEmptyAndFailedPages(t *testing.T) {
	t.Parallel()

	empty, err := NewAuditStore(&beginnerStub{tx: &pgxTransactionStub{rowSets: []*rowsStub{{}}}})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	page, err := empty.ListTenant(context.Background(), "tenant-1", 0, 10)
	if err != nil || len(page.Entries) != 0 || page.NextSequence != 0 {
		t.Fatalf("ListTenant() = (%+v, %v), want empty page", page, err)
	}

	queryErr := errors.New("query failed")
	failed, err := NewAuditStore(&beginnerStub{tx: &pgxTransactionStub{rowSets: []*rowsStub{{queryErr: queryErr}}}})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	if _, err := failed.ListTenant(context.Background(), "tenant-1", 0, 10); !errors.Is(err, queryErr) {
		t.Fatalf("ListTenant() error = %v, want %v", err, queryErr)
	}
}

func TestAuditStoreVerifiesTenantHistoryInBoundedPages(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	first := history.Seal(anchor, auditEventForStore(5, time.Unix(5, 0)))
	second := history.Seal(first.Hash, auditEventForStore(6, time.Unix(6, 0)))
	firstPage := &rowsStub{records: [][]any{auditEntryRow(first), auditEntryRow(second)}}
	finalPage := &rowsStub{}
	tx := &pgxTransactionStub{
		rows:    []*rowStub{{values: []any{int64(4), anchor[:], time.Unix(4, 0)}}},
		rowSets: []*rowsStub{firstPage, finalPage},
	}
	store, err := NewAuditStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	report, err := store.VerifyTenant(context.Background(), "tenant-1", 2)
	if err != nil {
		t.Fatalf("VerifyTenant() error = %v", err)
	}
	if report.Events != 2 || report.HeadSequence != 6 || report.HeadHash != second.Hash {
		t.Fatalf("VerifyTenant() = %+v, want 2 events through sequence 6", report)
	}
	if tx.commits != 1 || tx.rollbacks != 0 || len(tx.queryCalls) != 3 {
		t.Fatalf("calls = commit:%d rollback:%d queries:%d, want 1:0:3", tx.commits, tx.rollbacks, len(tx.queryCalls))
	}
	if !finalPage.closed {
		t.Fatal("final audit page was not closed")
	}
}

func TestAuditStoreVerificationDetectsTampering(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	entry := history.Seal(anchor, auditEventForStore(5, time.Unix(5, 0)))
	entry.Event.Actor = "tampered"
	tx := &pgxTransactionStub{
		rows:    []*rowStub{{values: []any{int64(4), anchor[:], time.Unix(4, 0)}}},
		rowSets: []*rowsStub{{records: [][]any{auditEntryRow(entry)}}},
	}
	store, err := NewAuditStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	_, err = store.VerifyTenant(context.Background(), "tenant-1", 10)
	if !errors.Is(err, history.ErrAuditTampered) {
		t.Fatalf("VerifyTenant() error = %v, want ErrAuditTampered", err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("finalization = commit:%d rollback:%d, want 0:1", tx.commits, tx.rollbacks)
	}
}

func TestAuditStoreVerificationAcceptsFinalPartialPage(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	entry := history.Seal(anchor, auditEventForStore(5, time.Unix(5, 0)))
	tx := &pgxTransactionStub{
		rows:    []*rowStub{{values: []any{int64(4), anchor[:], time.Unix(4, 0)}}},
		rowSets: []*rowsStub{{records: [][]any{auditEntryRow(entry)}}},
	}
	store, err := NewAuditStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	report, err := store.VerifyTenant(context.Background(), "tenant-1", 10)
	if err != nil || report.Events != 1 || report.HeadSequence != 5 {
		t.Fatalf("VerifyTenant() = (%+v, %v), want one verified event", report, err)
	}
}

func TestAuditStoreVerificationPropagatesTransactionFailure(t *testing.T) {
	t.Parallel()

	beginErr := errors.New("begin failed")
	store, err := NewAuditStore(&beginnerStub{err: beginErr})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	_, err = store.VerifyTenant(context.Background(), "tenant-1", 10)
	if !errors.Is(err, beginErr) {
		t.Fatalf("VerifyTenant() error = %v, want %v", err, beginErr)
	}
}

func TestAuditStoreVerificationFailsOnAnchorAndPageErrors(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	loadErr := errors.New("load failed")
	tests := map[string]*pgxTransactionStub{
		"anchor": {rows: []*rowStub{{err: loadErr}}},
		"page": {
			rows:    []*rowStub{{values: []any{int64(4), anchor[:], time.Unix(4, 0)}}},
			rowSets: []*rowsStub{{queryErr: loadErr}},
		},
	}

	for name, tx := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := NewAuditStore(&beginnerStub{tx: tx})
			if err != nil {
				t.Fatalf("NewAuditStore() error = %v", err)
			}
			_, err = store.VerifyTenant(context.Background(), "tenant-1", 10)
			if !errors.Is(err, loadErr) {
				t.Fatalf("VerifyTenant() error = %v, want %v", err, loadErr)
			}
		})
	}
}

func TestAuditStoreRetainsContiguousPrefixAndAdvancesAnchor(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	next := history.HashBytes([]byte("next-anchor"))
	retainedThrough := time.Unix(8, 0)
	tx := &pgxTransactionStub{
		execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
		rows: []*rowStub{
			{values: []any{int64(4), anchor[:], time.Unix(4, 0)}},
			{values: []any{int64(8), next[:], retainedThrough, int64(4)}},
		},
	}
	store, err := NewAuditStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}

	result, err := store.RetainBefore(context.Background(), "tenant-1", time.Unix(10, 0), 100)
	if err != nil {
		t.Fatalf("RetainBefore() error = %v", err)
	}
	want := RetentionResult{
		Deleted:         4,
		AnchorSequence:  8,
		AnchorHash:      next,
		RetainedThrough: retainedThrough.UTC(),
	}
	if result != want {
		t.Fatalf("RetainBefore() = %+v, want %+v", result, want)
	}
	if tx.commits != 1 || len(tx.execCalls) != 1 || len(tx.queryCalls) != 2 {
		t.Fatalf("calls = commit:%d exec:%d query:%d, want 1:1:2", tx.commits, len(tx.execCalls), len(tx.queryCalls))
	}
}

func TestAuditStoreRetentionFailsClosed(t *testing.T) {
	t.Parallel()

	anchor := history.HashBytes([]byte("retained"))
	storeErr := errors.New("store failed")
	loadErr := errors.New("load failed")
	cutoff := time.Unix(10, 0)
	tests := map[string]struct {
		beginner *beginnerStub
		wantErr  error
	}{
		"begin": {
			beginner: &beginnerStub{err: loadErr},
			wantErr:  loadErr,
		},
		"ensure anchor": {
			beginner: &beginnerStub{tx: &pgxTransactionStub{
				execSteps: []execStep{{err: storeErr}},
			}},
			wantErr: storeErr,
		},
		"load anchor": {
			beginner: &beginnerStub{tx: &pgxTransactionStub{
				execSteps: []execStep{{}},
				rows:      []*rowStub{{err: loadErr}},
			}},
			wantErr: loadErr,
		},
		"retention query": {
			beginner: &beginnerStub{tx: &pgxTransactionStub{
				execSteps: []execStep{{}},
				rows: []*rowStub{
					{values: []any{int64(4), anchor[:], time.Unix(4, 0)}},
					{err: storeErr},
				},
			}},
			wantErr: storeErr,
		},
		"negative sequence": {
			beginner: retentionBeginner(anchor, []any{int64(-1), anchor[:], time.Unix(4, 0), int64(0)}),
			wantErr:  ErrInvalidAuditState,
		},
		"negative count": {
			beginner: retentionBeginner(anchor, []any{int64(4), anchor[:], time.Unix(4, 0), int64(-1)}),
			wantErr:  ErrInvalidAuditState,
		},
		"oversized count": {
			beginner: retentionBeginner(anchor, []any{int64(4), anchor[:], time.Unix(4, 0), int64(11)}),
			wantErr:  ErrInvalidAuditState,
		},
		"invalid hash": {
			beginner: retentionBeginner(anchor, []any{int64(4), []byte("short"), time.Unix(4, 0), int64(0)}),
			wantErr:  ErrAuditHashInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := NewAuditStore(tt.beginner)
			if err != nil {
				t.Fatalf("NewAuditStore() error = %v", err)
			}
			_, err = store.RetainBefore(context.Background(), "tenant-1", cutoff, 10)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RetainBefore() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuditStoreValidatesBoundedRequests(t *testing.T) {
	t.Parallel()

	store, err := NewAuditStore(&beginnerStub{})
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	tests := map[string]func() error{
		"verify tenant": func() error {
			_, err := store.VerifyTenant(context.Background(), "", 10)
			return err
		},
		"verify page": func() error {
			_, err := store.VerifyTenant(context.Background(), "tenant-1", MaxAuditBatch+1)
			return err
		},
		"retention cutoff": func() error {
			_, err := store.RetainBefore(context.Background(), "tenant-1", time.Time{}, 10)
			return err
		},
		"retention batch": func() error {
			_, err := store.RetainBefore(context.Background(), "tenant-1", time.Now(), 0)
			return err
		},
	}

	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := operation(); !errors.Is(err, ErrInvalidAuditRequest) {
				t.Fatalf("operation error = %v, want ErrInvalidAuditRequest", err)
			}
		})
	}
}

func TestLoadAuditAnchorRejectsMissingAndMalformedState(t *testing.T) {
	t.Parallel()

	databaseErr := errors.New("database unavailable")
	validHash := make([]byte, len(history.Hash{}))
	tests := map[string]struct {
		row     *rowStub
		wantErr error
	}{
		"missing": {
			row:     &rowStub{err: pgx.ErrNoRows},
			wantErr: ErrAuditAnchorNotFound,
		},
		"database": {
			row:     &rowStub{err: databaseErr},
			wantErr: databaseErr,
		},
		"negative sequence": {
			row:     &rowStub{values: []any{int64(-1), validHash, time.Unix(1, 0)}},
			wantErr: ErrInvalidAuditState,
		},
		"invalid hash": {
			row:     &rowStub{values: []any{int64(0), []byte("short"), time.Unix(1, 0)}},
			wantErr: ErrAuditHashInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tx := &pgxTransactionStub{rows: []*rowStub{tt.row}}
			_, _, _, err := loadAuditAnchor(context.Background(), tx, "tenant-1", "query")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("loadAuditAnchor() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadAuditPageRejectsQueryAndPersistedStateFailures(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("query failed")
	readErr := errors.New("read failed")
	entry := history.Seal(history.Hash{}, auditEventForStore(1, time.Unix(1, 0)))
	invalidSequence := auditEntryRow(entry)
	invalidSequence[0] = int64(0)
	invalidVersion := auditEntryRow(entry)
	invalidVersion[1] = int16(-1)
	invalidPrevious := auditEntryRow(entry)
	invalidPrevious[9] = []byte("short")
	invalidHash := auditEntryRow(entry)
	invalidHash[10] = []byte("short")
	tests := map[string]struct {
		rows    *rowsStub
		wantErr error
	}{
		"query": {
			rows:    &rowsStub{queryErr: queryErr},
			wantErr: queryErr,
		},
		"scan": {
			rows:    &rowsStub{records: [][]any{{int64(1)}}},
			wantErr: errors.New("unexpected scan destination count"),
		},
		"read": {
			rows:    &rowsStub{err: readErr},
			wantErr: readErr,
		},
		"sequence": {
			rows:    &rowsStub{records: [][]any{invalidSequence}},
			wantErr: ErrInvalidAuditState,
		},
		"hash version": {
			rows:    &rowsStub{records: [][]any{invalidVersion}},
			wantErr: ErrInvalidAuditState,
		},
		"previous hash": {
			rows:    &rowsStub{records: [][]any{invalidPrevious}},
			wantErr: ErrAuditHashInvalid,
		},
		"event hash": {
			rows:    &rowsStub{records: [][]any{invalidHash}},
			wantErr: ErrAuditHashInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tx := &pgxTransactionStub{rowSets: []*rowsStub{tt.rows}}
			_, err := loadAuditPage(context.Background(), tx, "tenant-1", 0, 10)
			if name == "scan" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("loadAuditPage() error = %v, want scan error", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("loadAuditPage() error = %v, want %v", err, tt.wantErr)
			}
			if !tt.rows.closed && tt.rows.queryErr == nil {
				t.Fatal("audit rows were not closed")
			}
		})
	}
	if _, err := loadAuditPage(
		context.Background(),
		&pgxTransactionStub{},
		"tenant-1",
		math.MaxUint64,
		10,
	); !errors.Is(err, ErrInvalidAuditRequest) {
		t.Fatalf("loadAuditPage(overflow) error = %v", err)
	}
}

func auditEventForStore(sequence uint64, occurredAt time.Time) history.Event {
	return history.Event{
		Sequence:       sequence,
		OccurredAt:     occurredAt.UTC(),
		CommandID:      "command-123",
		IdempotencyKey: "request-123",
		Actor:          "operator@example.test",
		Action:         "drain",
		Target:         "worker_group:payments",
		Result:         "succeeded",
	}
}

func auditEntryRow(entry history.Entry) []any {
	return []any{
		int64(entry.Event.Sequence),
		int16(entry.Event.HashVersion),
		entry.Event.OccurredAt.In(time.FixedZone("database", 2*60*60)),
		entry.Event.CommandID,
		sql.NullString{String: entry.Event.IdempotencyKey, Valid: entry.Event.IdempotencyKey != ""},
		entry.Event.Actor,
		entry.Event.Action,
		entry.Event.Target,
		entry.Event.Result,
		entry.PreviousHash[:],
		entry.Hash[:],
	}
}

func retentionBeginner(anchor history.Hash, result []any) *beginnerStub {
	return &beginnerStub{tx: &pgxTransactionStub{
		execSteps: []execStep{{}},
		rows: []*rowStub{
			{values: []any{int64(4), anchor[:], time.Unix(4, 0)}},
			{values: result},
		},
	}}
}
