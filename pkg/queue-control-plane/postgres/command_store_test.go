package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCommandStoreListsNewestTenantCommandsWithOpaqueCursor(t *testing.T) {
	t.Parallel()

	newest := journalCommand(func(command *controlplane.Command) {
		command.IdempotencyKey = "request-3"
		command.RequestedAt = time.Unix(3, 0).UTC()
	})
	middle := journalCommand(func(command *controlplane.Command) {
		command.IdempotencyKey = "request-2"
		command.RequestedAt = time.Unix(2, 0).UTC()
	})
	oldest := journalCommand(func(command *controlplane.Command) {
		command.IdempotencyKey = "request-1"
		command.RequestedAt = time.Unix(1, 0).UTC()
	})
	resultFor := func(command controlplane.Command) controlplane.CommandResult {
		return controlplane.CommandResult{
			CommandID:      command.CommandID,
			IdempotencyKey: command.IdempotencyKey,
			TenantID:       command.TenantID,
			Status:         controlplane.CommandSucceeded,
			CompletedAt:    command.RequestedAt.Add(time.Second),
		}
	}
	rows := &rowsStub{records: [][]any{
		storedCommandRow(newest, resultFor(newest)),
		storedCommandRow(middle, resultFor(middle)),
		storedCommandRow(oldest, resultFor(oldest)),
	}}
	tx := &pgxTransactionStub{rowSets: []*rowsStub{rows}}
	store, err := NewCommandStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}

	page, err := store.ListTenant(context.Background(), "tenant-1", "", 2)
	if err != nil {
		t.Fatalf("ListTenant() error = %v", err)
	}
	want := []CommandRecord{
		{Command: newest, Result: resultFor(newest)},
		{Command: middle, Result: resultFor(middle)},
	}
	if !reflect.DeepEqual(page.Records, want) || page.NextCursor == "" {
		t.Fatalf("ListTenant() = %+v, want records %+v and a cursor", page, want)
	}
	if !rows.closed || tx.commits != 1 || tx.rollbacks != 0 || len(tx.queryCalls) != 1 {
		t.Fatalf(
			"calls = closed:%t commit:%d rollback:%d query:%d",
			rows.closed,
			tx.commits,
			tx.rollbacks,
			len(tx.queryCalls),
		)
	}
	if args := tx.queryCalls[0].args; len(args) != 4 || args[0] != "tenant-1" || args[3] != int64(3) {
		t.Fatalf("query args = %#v, want tenant and limit+1", args)
	}
}

func TestCommandStoreContinuesFromOpaqueCursor(t *testing.T) {
	t.Parallel()

	command := journalCommand(func(command *controlplane.Command) {
		command.IdempotencyKey = "request-2"
		command.RequestedAt = time.Unix(2, 123_000).UTC()
	})
	result := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandAccepted,
	}
	rows := &rowsStub{records: [][]any{storedCommandRow(command, result)}}
	tx := &pgxTransactionStub{rowSets: []*rowsStub{rows}}
	store, err := NewCommandStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	cursor := encodeCommandCursor(command.RequestedAt, command.IdempotencyKey)

	page, err := store.ListTenant(context.Background(), "tenant-1", cursor, 2)
	if err != nil || len(page.Records) != 1 || page.NextCursor != "" {
		t.Fatalf("ListTenant() = (%+v, %v), want one final record", page, err)
	}
	args := tx.queryCalls[0].args
	if args[1] != command.RequestedAt || args[2] != command.IdempotencyKey {
		t.Fatalf("cursor query args = (%v, %v)", args[1], args[2])
	}
}

func TestCommandStoreRejectsInvalidListRequests(t *testing.T) {
	t.Parallel()

	store, err := NewCommandStore(&beginnerStub{})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	encode := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	tests := []struct {
		tenant string
		cursor string
		limit  uint32
	}{
		{tenant: "", limit: 1},
		{tenant: "tenant-1", limit: 0},
		{tenant: "tenant-1", limit: MaxCommandPageSize + 1},
		{tenant: "tenant-1", cursor: strings.Repeat("x", MaxCommandCursorBytes+1), limit: 1},
		{tenant: "tenant-1", cursor: "!", limit: 1},
		{tenant: "tenant-1", cursor: encode("missing-separator"), limit: 1},
		{tenant: "tenant-1", cursor: encode("1:"), limit: 1},
		{tenant: "tenant-1", cursor: encode("1:" + strings.Repeat("x", controlplane.MaxIdentityBytes+1)), limit: 1},
		{tenant: "tenant-1", cursor: encode("not-a-time:request-1"), limit: 1},
	}
	for _, tt := range tests {
		if _, err := store.ListTenant(
			context.Background(), tt.tenant, tt.cursor, tt.limit,
		); !errors.Is(err, ErrInvalidCommandRequest) {
			t.Fatalf("ListTenant(%q, %q, %d) error = %v", tt.tenant, tt.cursor, tt.limit, err)
		}
	}
}

func TestCommandStoreFailsClosedOnInvalidCommandPages(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("query failed")
	readErr := errors.New("read failed")
	validCommand := journalCommand()
	validResult := journalResult()
	invalidCommand := validCommand
	invalidCommand.Actor = ""
	invalidResult := validResult
	invalidResult.Status = controlplane.CommandAccepted
	tests := map[string]struct {
		rows       *rowsStub
		wantErr    error
		wantText   string
		wantClosed bool
	}{
		"query": {
			rows:    &rowsStub{queryErr: queryErr},
			wantErr: queryErr,
		},
		"scan": {
			rows:       &rowsStub{records: [][]any{{"too-few-columns"}}},
			wantText:   "unexpected scan destination count",
			wantClosed: true,
		},
		"command": {
			rows:       &rowsStub{records: [][]any{storedCommandRow(invalidCommand, validResult)}},
			wantErr:    ErrInvalidCommandState,
			wantClosed: true,
		},
		"result": {
			rows:       &rowsStub{records: [][]any{storedCommandRow(validCommand, invalidResult)}},
			wantErr:    ErrInvalidCommandState,
			wantClosed: true,
		},
		"read": {
			rows:       &rowsStub{err: readErr},
			wantErr:    readErr,
			wantClosed: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tx := &pgxTransactionStub{rowSets: []*rowsStub{tt.rows}}
			store, err := NewCommandStore(&beginnerStub{tx: tx})
			if err != nil {
				t.Fatalf("NewCommandStore() error = %v", err)
			}
			_, err = store.ListTenant(context.Background(), "tenant-1", "", 1)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListTenant() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantText)) {
				t.Fatalf("ListTenant() error = %v, want text %q", err, tt.wantText)
			}
			if tx.commits != 0 || tx.rollbacks != 1 || tt.rows.closed != tt.wantClosed {
				t.Fatalf("transaction = commit:%d rollback:%d closed:%t", tx.commits, tx.rollbacks, tt.rows.closed)
			}
		})
	}
}

func TestCommandStoreRetainsOnlyBoundedEligibleTerminalCommands(t *testing.T) {
	t.Parallel()

	tx := &pgxTransactionStub{execSteps: []execStep{{
		tag: pgconn.NewCommandTag("DELETE 2"),
	}}}
	store, err := NewCommandStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	cutoff := time.Unix(100, 123_000).UTC()

	result, err := store.RetainCommandsBefore(
		context.Background(), "tenant-1", cutoff, 10,
	)
	if err != nil || result.Deleted != 2 {
		t.Fatalf("RetainCommandsBefore() = (%+v, %v), want 2", result, err)
	}
	if tx.commits != 1 || tx.rollbacks != 0 || len(tx.execCalls) != 1 {
		t.Fatalf("transaction = commit:%d rollback:%d exec:%d", tx.commits, tx.rollbacks, len(tx.execCalls))
	}
	if args := tx.execCalls[0].args; len(args) != 3 || args[0] != "tenant-1" ||
		args[1] != cutoff || args[2] != int64(10) {
		t.Fatalf("retention args = %#v", args)
	}
	query := tx.execCalls[0].query
	for _, required := range []string{
		"status <> 'accepted'",
		"completed_at < $2",
		"queue_control_audit_events",
		"queue_control_desired_states",
		"ORDER BY completed_at, idempotency_key",
		"LIMIT $3",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("retention query does not contain %q: %s", required, query)
		}
	}
}

func TestCommandStoreRejectsInvalidRetentionRequests(t *testing.T) {
	t.Parallel()

	store, err := NewCommandStore(&beginnerStub{})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	for _, request := range []struct {
		tenant string
		cutoff time.Time
		batch  uint32
	}{
		{tenant: "", cutoff: time.Unix(1, 0), batch: 1},
		{tenant: "tenant-1", batch: 1},
		{tenant: "tenant-1", cutoff: time.Unix(1, 0), batch: 0},
		{tenant: "tenant-1", cutoff: time.Unix(1, 0), batch: MaxCommandRetentionBatch + 1},
	} {
		if _, err := store.RetainCommandsBefore(
			context.Background(), request.tenant, request.cutoff, request.batch,
		); !errors.Is(err, ErrInvalidCommandRetentionRequest) {
			t.Fatalf("RetainCommandsBefore(%+v) error = %v", request, err)
		}
	}
}

func TestCommandStoreRetentionFailsClosed(t *testing.T) {
	t.Parallel()

	retainErr := errors.New("delete failed")
	for name, tt := range map[string]struct {
		step    execStep
		wantErr error
	}{
		"delete": {
			step: execStep{err: retainErr}, wantErr: retainErr,
		},
		"count": {
			step:    execStep{tag: pgconn.NewCommandTag("DELETE 2")},
			wantErr: ErrInvalidCommandRetentionState,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tx := &pgxTransactionStub{execSteps: []execStep{tt.step}}
			store, err := NewCommandStore(&beginnerStub{tx: tx})
			if err != nil {
				t.Fatalf("NewCommandStore() error = %v", err)
			}
			if _, err := store.RetainCommandsBefore(
				context.Background(), "tenant-1", time.Unix(1, 0), 1,
			); !errors.Is(err, tt.wantErr) {
				t.Fatalf("RetainCommandsBefore() error = %v, want %v", err, tt.wantErr)
			}
			if tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("transaction = commit:%d rollback:%d", tx.commits, tx.rollbacks)
			}
		})
	}
}

func TestCommandStoreGetsTenantScopedResult(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	result := journalResult()
	tx := &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, result)}}}
	store, err := NewCommandStore(&beginnerStub{tx: tx})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}

	stored, err := store.Get(context.Background(), command.TenantID, command.IdempotencyKey)
	if err != nil || stored != result {
		t.Fatalf("Get() = (%+v, %v), want %+v", stored, err, result)
	}
	if tx.commits != 1 || tx.rollbacks != 0 || len(tx.queryCalls) != 1 {
		t.Fatalf("calls = commit:%d rollback:%d query:%d", tx.commits, tx.rollbacks, len(tx.queryCalls))
	}
}

func TestCommandStoreRejectsInvalidMissingAndFailedReads(t *testing.T) {
	t.Parallel()

	if store, err := NewCommandStore(nil); store != nil || !errors.Is(err, ErrNilBeginner) {
		t.Fatalf("NewCommandStore(nil) = (%v, %v)", store, err)
	}
	store, err := NewCommandStore(&beginnerStub{})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	for _, scope := range [][2]string{
		{"", "request-1"},
		{"tenant-1", ""},
		{strings.Repeat("x", controlplane.MaxIdentityBytes+1), "request-1"},
		{"tenant-1", strings.Repeat("x", controlplane.MaxIdentityBytes+1)},
	} {
		if _, err := store.Get(context.Background(), scope[0], scope[1]); !errors.Is(err, ErrInvalidCommandRequest) {
			t.Fatalf("Get(%q, %q) error = %v", scope[0], scope[1], err)
		}
	}

	missing, err := NewCommandStore(&beginnerStub{tx: &pgxTransactionStub{rows: []*rowStub{{err: pgx.ErrNoRows}}}})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	if _, err := missing.Get(context.Background(), "tenant-1", "request-1"); !errors.Is(err, ErrCommandNotFound) {
		t.Fatalf("Get() error = %v, want ErrCommandNotFound", err)
	}

	queryErr := errors.New("query failed")
	failed, err := NewCommandStore(&beginnerStub{tx: &pgxTransactionStub{rows: []*rowStub{{err: queryErr}}}})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	if _, err := failed.Get(context.Background(), "tenant-1", "request-1"); !errors.Is(err, queryErr) {
		t.Fatalf("Get() error = %v, want %v", err, queryErr)
	}

	command := journalCommand()
	invalid := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandAccepted,
		CompletedAt:    command.RequestedAt,
	}
	corrupt, err := NewCommandStore(&beginnerStub{tx: &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, invalid)}}}})
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	if _, err := corrupt.Get(context.Background(), command.TenantID, command.IdempotencyKey); !errors.Is(err, ErrInvalidCommandState) {
		t.Fatalf("Get() error = %v, want ErrInvalidCommandState", err)
	}
}
