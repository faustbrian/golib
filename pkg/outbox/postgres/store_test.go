package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var allowReplayForTest = ReplayAuthorizeFunc(func(context.Context, ReplayRequest) error {
	return nil
})

func TestNewStoreAndClaimRejectInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil, StoreConfig{}); !errors.Is(err, ErrPoolRequired) {
		t.Fatalf("nil pool error = %v", err)
	}
	pool := &pgxpool.Pool{}
	if _, err := NewStore(pool, StoreConfig{MaxClaimBatch: -1}); !errors.Is(err, ErrInvalidClaimLimit) {
		t.Fatalf("negative batch error = %v", err)
	}
	if _, err := NewStore(pool, StoreConfig{MaxAdminBatch: -1}); !errors.Is(err, ErrInvalidAdminLimit) {
		t.Fatalf("negative admin batch error = %v", err)
	}
	if _, err := NewStore(pool, StoreConfig{MaxLeaseDuration: -1}); !errors.Is(err, ErrInvalidLeaseDuration) {
		t.Fatalf("negative lease error = %v", err)
	}
	if _, err := NewStore(pool, StoreConfig{MaxClaimBatch: 1001}); !errors.Is(err, ErrInvalidClaimLimit) {
		t.Fatalf("unbounded claim batch error = %v", err)
	}
	if _, err := NewStore(pool, StoreConfig{MaxAdminBatch: 1001}); !errors.Is(err, ErrInvalidAdminLimit) {
		t.Fatalf("unbounded admin batch error = %v", err)
	}
	if _, err := NewStore(pool, StoreConfig{MaxLeaseDuration: 24*time.Hour + time.Nanosecond}); !errors.Is(err, ErrInvalidLeaseDuration) {
		t.Fatalf("unbounded lease duration error = %v", err)
	}

	store, err := NewStore(pool, StoreConfig{MaxClaimBatch: 2, MaxLeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	requests := []struct {
		request ClaimRequest
		want    error
	}{
		{request: ClaimRequest{Limit: 1, LeaseDuration: time.Second}, want: ErrClaimOwnerRequired},
		{request: ClaimRequest{Owner: "relay", LeaseDuration: time.Second}, want: ErrInvalidClaimLimit},
		{request: ClaimRequest{Owner: "relay", Limit: 3, LeaseDuration: time.Second}, want: ErrInvalidClaimLimit},
		{request: ClaimRequest{Owner: "relay", Limit: 1}, want: ErrInvalidLeaseDuration},
		{request: ClaimRequest{Owner: "relay", Limit: 1, LeaseDuration: 2 * time.Minute}, want: ErrInvalidLeaseDuration},
		{request: ClaimRequest{Owner: "relay", Limit: 1, LeaseDuration: time.Second, Serialization: 255}, want: ErrInvalidSerialization},
	}
	for _, test := range requests {
		if _, err := store.Claim(context.Background(), test.request); !errors.Is(err, test.want) {
			t.Fatalf("request %#v error = %v, want %v", test.request, err, test.want)
		}
	}
	if _, err := store.ExtendLease(context.Background(), LeaseRef{}, 0); !errors.Is(err, ErrInvalidLeaseDuration) {
		t.Fatalf("invalid extension error = %v", err)
	}
	for _, delay := range []time.Duration{-1, time.Minute + time.Nanosecond} {
		if err := store.Retry(context.Background(), LeaseRef{}, delay, nil); !errors.Is(err, ErrInvalidRetryDelay) {
			t.Fatalf("retry delay %s error = %v, want %v", delay, err, ErrInvalidRetryDelay)
		}
	}
}

func TestInspectRejectsUnboundedOrInvalidRequests(t *testing.T) {
	t.Parallel()

	store, err := newStore(&faultDatabase{}, StoreConfig{MaxAdminBatch: 2})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	requests := []InspectRequest{
		{},
		{Limit: 3},
		{Limit: 1, State: MessageState("unknown")},
	}
	for _, request := range requests {
		if _, err := store.Inspect(context.Background(), request); err == nil {
			t.Fatalf("request %#v unexpectedly succeeded", request)
		}
	}
}

func TestInspectReturnsPayloadFreeSummariesAndPreservesFailures(t *testing.T) {
	t.Parallel()

	now := time.Now()
	rows := &faultRows{next: true, scan: func(destinations []any) error {
		*destinations[0].(*string) = "message-id"
		*destinations[1].(*string) = "topic"
		*destinations[2].(*string) = "ordering-key"
		*destinations[3].(*string) = "idempotency-key"
		*destinations[4].(*int) = 2
		*destinations[5].(*time.Time) = now
		*destinations[6].(*time.Time) = now
		*destinations[7].(*time.Time) = now
		*destinations[8].(*MessageState) = MessageStateLeased

		return nil
	}}
	store, err := newStore(&faultDatabase{rows: rows}, StoreConfig{})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	summaries, err := store.Inspect(context.Background(), InspectRequest{
		State: MessageStateLeased, Topic: "topic", Before: now.Add(time.Hour), Limit: 1,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "message-id" || summaries[0].State != MessageStateLeased {
		t.Fatalf("summaries = %#v", summaries)
	}

	failure := errors.New("injected failure")
	for name, database := range map[string]database{
		"query": &faultDatabase{queryErr: failure},
		"scan":  &faultDatabase{rows: &faultRows{next: true, scanErr: failure}},
		"rows":  &faultDatabase{rows: &faultRows{rowsErr: failure}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store, err := newStore(database, StoreConfig{})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			if _, err := store.Inspect(context.Background(), InspectRequest{Limit: 1}); !errors.Is(err, failure) {
				t.Fatalf("inspect error = %v, want %v", err, failure)
			}
		})
	}
}

func TestAdministrativeOperationsEmitPayloadSafeEvents(t *testing.T) {
	t.Parallel()

	observer := &storeObserver{}
	now := time.Now()
	clockCalls := 0
	store, err := newStore(&faultDatabase{queryErr: errors.New("secret database error")}, StoreConfig{
		Observer: observer,
		Clock: func() time.Time {
			clockCalls++
			if clockCalls == 1 {
				return now
			}

			return now.Add(-time.Second)
		},
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	_, _ = store.PruneDelivered(context.Background(), time.Now(), 1)
	if len(observer.events) != 1 || observer.events[0].Operation != outbox.OperationPrune ||
		observer.events[0].Outcome != outbox.OutcomeFailure {
		t.Fatalf("events = %#v", observer.events)
	}
	replayStore, err := newStore(&faultDatabase{tx: &faultTx{rows: replayRows()}}, StoreConfig{
		Observer: observer, LeaseTokenGenerator: func() (string, error) { return "replay-id", nil },
		ReplayAuthorizer: allowReplayForTest,
	})
	if err != nil {
		t.Fatalf("create replay store: %v", err)
	}
	if _, err := replayStore.Replay(context.Background(), ReplayRequest{
		IDs: []string{"message-id"}, RequestedBy: "operator", Reason: "incident",
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	archiveStore, err := newStore(&faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`))}}, StoreConfig{Observer: observer})
	if err != nil {
		t.Fatalf("create archive store: %v", err)
	}
	if _, err := archiveStore.ArchiveAndPruneDelivered(
		context.Background(), time.Now(), 1,
		ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil }),
	); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(observer.events) != 3 || observer.events[1].Operation != outbox.OperationReplay ||
		observer.events[1].Outcome != outbox.OutcomeSuccess || observer.events[1].Count != 1 ||
		observer.events[2].Operation != outbox.OperationArchive || observer.events[2].Count != 1 {
		t.Fatalf("events = %#v", observer.events)
	}
}

func TestStoreContainsObserverPanic(t *testing.T) {
	t.Parallel()

	store, err := newStore(&faultDatabase{}, StoreConfig{
		Observer: outbox.ObserverFunc(func(context.Context, outbox.Event) {
			panic("diagnostic failure")
		}),
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.Replay(context.Background(), ReplayRequest{}); !errors.Is(err, ErrReplayIDsRequired) {
		t.Fatalf("error = %v, want %v", err, ErrReplayIDsRequired)
	}
}

func TestStoreContainsDiagnosticClockPanic(t *testing.T) {
	t.Parallel()

	store, err := newStore(&faultDatabase{}, StoreConfig{
		Clock: func() time.Time { panic("diagnostic clock failure") },
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.Replay(context.Background(), ReplayRequest{}); !errors.Is(err, ErrReplayIDsRequired) {
		t.Fatalf("replay error = %v, want %v", err, ErrReplayIDsRequired)
	}
}

func TestReplayAndPruneRejectInvalidRequests(t *testing.T) {
	t.Parallel()

	store, err := newStore(&faultDatabase{}, StoreConfig{
		MaxAdminBatch:       2,
		LeaseTokenGenerator: func() (string, error) { return "token", nil },
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	replays := []struct {
		request ReplayRequest
		want    error
	}{
		{request: ReplayRequest{}, want: ErrReplayIDsRequired},
		{request: ReplayRequest{IDs: []string{"1", "2", "3"}}, want: ErrInvalidAdminLimit},
		{request: ReplayRequest{IDs: []string{"1"}}, want: ErrReplayRequestedBy},
		{request: ReplayRequest{IDs: []string{"1"}, RequestedBy: "operator"}, want: ErrReplayReasonRequired},
		{request: ReplayRequest{IDs: []string{""}, RequestedBy: "operator", Reason: "incident"}, want: ErrReplayIDsRequired},
		{request: ReplayRequest{IDs: []string{"1", "1"}, RequestedBy: "operator", Reason: "incident"}, want: ErrReplayDuplicateID},
	}
	for _, test := range replays {
		if _, err := store.Replay(context.Background(), test.request); !errors.Is(err, test.want) {
			t.Fatalf("replay %#v error = %v, want %v", test.request, err, test.want)
		}
	}
	if _, err := store.PruneDelivered(context.Background(), time.Time{}, 1); !errors.Is(err, ErrPruneCutoffRequired) {
		t.Fatalf("zero cutoff error = %v", err)
	}
	if _, err := store.PruneDelivered(context.Background(), time.Now(), 0); !errors.Is(err, ErrInvalidAdminLimit) {
		t.Fatalf("zero prune limit error = %v", err)
	}
	if _, err := store.PruneDelivered(context.Background(), time.Now(), 3); !errors.Is(err, ErrInvalidAdminLimit) {
		t.Fatalf("large prune limit error = %v", err)
	}
	if _, err := store.PruneDead(context.Background(), time.Time{}, 1); !errors.Is(err, ErrPruneCutoffRequired) {
		t.Fatalf("zero dead cutoff error = %v", err)
	}
	if _, err := store.ArchiveAndPruneDead(context.Background(), time.Now(), 1, nil); !errors.Is(err, ErrArchiverRequired) {
		t.Fatalf("nil dead archiver error = %v", err)
	}
	if _, err := store.ArchiveAndPruneDelivered(context.Background(), time.Time{}, 1, ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil })); !errors.Is(err, ErrPruneCutoffRequired) {
		t.Fatalf("zero archive cutoff error = %v", err)
	}
	if _, err := store.ArchiveAndPruneDelivered(context.Background(), time.Now(), 3, ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil })); !errors.Is(err, ErrInvalidAdminLimit) {
		t.Fatalf("large archive limit error = %v", err)
	}
	if _, err := store.ArchiveAndPruneDelivered(context.Background(), time.Now(), 1, nil); !errors.Is(err, ErrArchiverRequired) {
		t.Fatalf("nil archiver error = %v", err)
	}
}

func TestReplayRequiresExplicitAuthorization(t *testing.T) {
	t.Parallel()

	request := ReplayRequest{
		IDs: []string{"message-id"}, RequestedBy: "operator", Reason: "incident",
	}
	store, err := newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return "replay-id", nil },
	})
	if err != nil {
		t.Fatalf("create default-deny store: %v", err)
	}
	if _, err := store.Replay(context.Background(), request); !errors.Is(err, ErrReplayUnauthorized) {
		t.Fatalf("default replay error = %v, want %v", err, ErrReplayUnauthorized)
	}

	authorizationFailure := errors.New("secret policy detail")
	var authorizedRequest ReplayRequest
	store, err = newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return "replay-id", nil },
		ReplayAuthorizer: ReplayAuthorizeFunc(func(_ context.Context, got ReplayRequest) error {
			authorizedRequest = got

			return authorizationFailure
		}),
	})
	if err != nil {
		t.Fatalf("create authorizing store: %v", err)
	}
	_, err = store.Replay(context.Background(), request)
	if !errors.Is(err, ErrReplayUnauthorized) || errors.Is(err, authorizationFailure) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("authorization error = %v, want fixed denial", err)
	}
	if len(authorizedRequest.IDs) != 1 || authorizedRequest.IDs[0] != "message-id" {
		t.Fatalf("authorized request = %#v", authorizedRequest)
	}

	store, err = newStore(&faultDatabase{}, StoreConfig{
		ReplayAuthorizer: ReplayAuthorizeFunc(func(context.Context, ReplayRequest) error {
			panic("secret panic detail")
		}),
	})
	if err != nil {
		t.Fatalf("create panic authorizer store: %v", err)
	}
	if _, err := store.Replay(context.Background(), request); !errors.Is(err, ErrReplayUnauthorized) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic authorization error = %v, want fixed denial", err)
	}

	beginFailure := errors.New("begin after authorization")
	store, err = newStore(&faultDatabase{beginErr: beginFailure}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return "replay-id", nil },
		ReplayAuthorizer: ReplayAuthorizeFunc(func(_ context.Context, got ReplayRequest) error {
			got.IDs[0] = "mutated-by-authorizer"

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("create mutating authorizer store: %v", err)
	}
	if _, err := store.Replay(context.Background(), request); !errors.Is(err, beginFailure) {
		t.Fatalf("authorized replay error = %v, want %v", err, beginFailure)
	}
	if request.IDs[0] != "message-id" {
		t.Fatalf("authorizer mutated request IDs: %#v", request.IDs)
	}
}

func TestReplayRejectsTimestampOutsideEnvelopeRangeBeforeAuthorization(t *testing.T) {
	t.Parallel()

	for name, availableAt := range map[string]time.Time{
		"before year zero": time.Date(-1, time.January, 1, 0, 0, 0, 0, time.UTC),
		"after year 9999":  time.Date(10_000, time.January, 1, 0, 0, 0, 0, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			authorized := false
			store, err := newStore(&faultDatabase{}, StoreConfig{
				ReplayAuthorizer: ReplayAuthorizeFunc(
					func(context.Context, ReplayRequest) error {
						authorized = true

						return nil
					},
				),
			})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			_, err = store.Replay(context.Background(), ReplayRequest{
				IDs:         []string{"message-id"},
				RequestedBy: "operator",
				Reason:      "invalid schedule",
				AvailableAt: availableAt,
			})
			if !errors.Is(err, outbox.ErrTimestampOutOfRange) || authorized {
				t.Fatalf("error/authorized = %v/%t, want range error before policy", err, authorized)
			}
		})
	}
}

func TestArchiveFuncForwardsDeliveredMessages(t *testing.T) {
	t.Parallel()

	want := []DeliveredMessage{{Envelope: outbox.Envelope{ID: "message-id"}}}
	var got []DeliveredMessage
	archiver := ArchiveFunc(func(_ context.Context, messages []DeliveredMessage) error {
		got = messages

		return nil
	})
	if err := archiver.Archive(context.Background(), want); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(got) != 1 || got[0].Envelope.ID != want[0].Envelope.ID {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestDeadArchiveFuncForwardsMessages(t *testing.T) {
	t.Parallel()

	want := []DeadMessage{{Envelope: outbox.Envelope{ID: "dead-id"}, LastError: "failure"}}
	var got []DeadMessage
	archiver := DeadArchiveFunc(func(_ context.Context, messages []DeadMessage) error {
		got = messages

		return nil
	})
	if err := archiver.ArchiveDead(context.Background(), want); err != nil {
		t.Fatalf("archive dead: %v", err)
	}
	if len(got) != 1 || got[0].Envelope.ID != "dead-id" || got[0].LastError != "failure" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDeadRetentionArchivesOrPrunesBoundedBatches(t *testing.T) {
	t.Parallel()

	rows := deliveredRows([]byte(`{}`)).(*faultRows)
	scan := rows.scan
	rows.scan = func(destinations []any) error {
		if err := scan(destinations); err != nil {
			return err
		}
		lastError := "publisher failure"
		*destinations[11].(**string) = &lastError

		return nil
	}
	archiveStore, err := newStore(&faultDatabase{tx: &faultTx{rows: rows}}, StoreConfig{})
	if err != nil {
		t.Fatalf("create archive store: %v", err)
	}
	var archived []DeadMessage
	ids, err := archiveStore.ArchiveAndPruneDead(
		context.Background(), time.Now(), 1,
		DeadArchiveFunc(func(_ context.Context, messages []DeadMessage) error {
			archived = messages

			return nil
		}),
	)
	if err != nil || len(ids) != 1 || len(archived) != 1 || archived[0].Envelope.ID != "message-id" ||
		archived[0].LastError != "publisher failure" {
		t.Fatalf("archive IDs/messages/error = %#v/%#v/%v", ids, archived, err)
	}
	nilErrorStore, err := newStore(&faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`))}}, StoreConfig{})
	if err != nil {
		t.Fatalf("create nil-error archive store: %v", err)
	}
	if _, err := nilErrorStore.ArchiveAndPruneDead(
		context.Background(), time.Now(), 1,
		DeadArchiveFunc(func(_ context.Context, messages []DeadMessage) error {
			if len(messages) != 1 || messages[0].LastError != "" {
				t.Fatalf("nil-error messages = %#v", messages)
			}

			return nil
		}),
	); err != nil {
		t.Fatalf("archive nil-error message: %v", err)
	}
	pruneStore, err := newStore(&faultDatabase{rows: idRows("dead-id")}, StoreConfig{})
	if err != nil {
		t.Fatalf("create prune store: %v", err)
	}
	ids, err = pruneStore.PruneDead(context.Background(), time.Now(), 1)
	if err != nil || len(ids) != 1 || ids[0] != "dead-id" {
		t.Fatalf("prune IDs/error = %#v/%v", ids, err)
	}
}

func TestArchiveAndPruneReturnsEmptyBatchWithoutCallingArchiver(t *testing.T) {
	t.Parallel()

	store, err := newStore(&faultDatabase{tx: &faultTx{rows: &faultRows{}}}, StoreConfig{})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	called := false
	ids, err := store.ArchiveAndPruneDelivered(
		context.Background(),
		time.Now(),
		1,
		ArchiveFunc(func(context.Context, []DeliveredMessage) error {
			called = true

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("archive empty batch: %v", err)
	}
	if ids == nil || len(ids) != 0 || called {
		t.Fatalf("IDs/called = %#v/%t", ids, called)
	}
}

func TestReplayPreservesTransactionFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected failure")
	valid := ReplayRequest{IDs: []string{"message-id"}, RequestedBy: "operator", Reason: "incident"}
	tests := map[string]struct {
		database database
		token    func() (string, error)
	}{
		"token": {
			database: &faultDatabase{},
			token:    func() (string, error) { return "", failure },
		},
		"begin": {
			database: &faultDatabase{beginErr: failure},
		},
		"query": {
			database: &faultDatabase{tx: &faultTx{queryErr: failure}},
		},
		"scan": {
			database: &faultDatabase{tx: &faultTx{rows: &faultRows{next: true, scanErr: failure}}},
		},
		"rows": {
			database: &faultDatabase{tx: &faultTx{rows: &faultRows{rowsErr: failure}}},
		},
		"audit": {
			database: &faultDatabase{tx: &faultTx{rows: replayRows(), execErr: failure}},
		},
		"commit": {
			database: &faultDatabase{tx: &faultTx{rows: replayRows(), commitErr: failure}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			token := test.token
			if token == nil {
				token = func() (string, error) { return "replay-id", nil }
			}
			store, err := newStore(test.database, StoreConfig{
				LeaseTokenGenerator: token, ReplayAuthorizer: allowReplayForTest,
			})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			_, err = store.Replay(context.Background(), valid)
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
		})
	}

	store, err := newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return "", nil },
		ReplayAuthorizer:    allowReplayForTest,
	})
	if err != nil {
		t.Fatalf("create empty-token store: %v", err)
	}
	if _, err := store.Replay(context.Background(), valid); err == nil {
		t.Fatal("expected empty replay ID error")
	}
}

func TestInternalTransactionsBoundRollbackCleanup(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected transaction failure")
	for name, operation := range map[string]func(*faultTx) error{
		"replay": func(tx *faultTx) error {
			store, err := newStore(&faultDatabase{tx: tx}, StoreConfig{
				LeaseTokenGenerator: func() (string, error) { return "replay-id", nil },
				ReplayAuthorizer:    allowReplayForTest,
			})
			if err != nil {
				return err
			}
			_, err = store.Replay(context.Background(), ReplayRequest{
				IDs: []string{"message-id"}, RequestedBy: "operator", Reason: "incident",
			})

			return err
		},
		"archive": func(tx *faultTx) error {
			store, err := newStore(&faultDatabase{tx: tx}, StoreConfig{})
			if err != nil {
				return err
			}
			_, err = store.ArchiveAndPruneDelivered(
				context.Background(), time.Now(), 1,
				ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil }),
			)

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var deadline time.Time
			tx := &faultTx{
				queryErr: failure,
				rollback: func(ctx context.Context) {
					deadline, _ = ctx.Deadline()
				},
			}
			if err := operation(tx); !errors.Is(err, failure) {
				t.Fatalf("operation error = %v, want %v", err, failure)
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > 5*time.Second {
				t.Fatalf("rollback deadline remaining = %s, want bounded cleanup", remaining)
			}
		})
	}
}

func TestPrunePreservesQueryAndRowFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected failure")
	tests := map[string]database{
		"query": &faultDatabase{queryErr: failure},
		"scan":  &faultDatabase{rows: &faultRows{next: true, scanErr: failure}},
		"rows":  &faultDatabase{rows: &faultRows{rowsErr: failure}},
	}
	for name, database := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := newStore(database, StoreConfig{})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			_, err = store.PruneDelivered(context.Background(), time.Now(), 1)
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
		})
	}
}

func TestArchiveAndPrunePreservesFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected failure")
	tests := map[string]struct {
		database database
		archiver Archiver
	}{
		"begin":        {database: &faultDatabase{beginErr: failure}},
		"query":        {database: &faultDatabase{tx: &faultTx{queryErr: failure}}},
		"scan":         {database: &faultDatabase{tx: &faultTx{rows: &faultRows{next: true, scanErr: failure}}}},
		"metadata":     {database: &faultDatabase{tx: &faultTx{rows: deliveredRows([]byte("{"))}}},
		"rows":         {database: &faultDatabase{tx: &faultTx{rows: &faultRows{rowsErr: failure}}}},
		"empty commit": {database: &faultDatabase{tx: &faultTx{rows: &faultRows{}, commitErr: failure}}},
		"archive": {
			database: &faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`))}},
			archiver: ArchiveFunc(func(context.Context, []DeliveredMessage) error { return failure }),
		},
		"delete": {database: &faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`)), execErr: failure}}},
		"commit": {database: &faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`)), commitErr: failure}}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			archiver := test.archiver
			if archiver == nil {
				archiver = ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil })
			}
			store, err := newStore(test.database, StoreConfig{})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			_, err = store.ArchiveAndPruneDelivered(context.Background(), time.Now(), 1, archiver)
			if name == "metadata" && err != nil {
				return
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
		})
	}
}

func TestArchiveAndPruneContainsArchiverPanic(t *testing.T) {
	t.Parallel()

	store, err := newStore(
		&faultDatabase{tx: &faultTx{rows: deliveredRows([]byte(`{}`))}},
		StoreConfig{},
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	_, err = store.ArchiveAndPruneDelivered(
		context.Background(),
		time.Now(),
		1,
		ArchiveFunc(func(context.Context, []DeliveredMessage) error {
			panic("payload=secret-value")
		}),
	)
	if !errors.Is(err, ErrArchiverPanic) || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error = %v, want payload-safe archiver panic", err)
	}
}

func TestStoreDiagnosticsPreserveDatabaseFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected failure")
	store, err := newStore(&faultDatabase{pingErr: failure, row: faultRow{err: failure}}, StoreConfig{})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Ping(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("ping error = %v, want %v", err, failure)
	}
	if _, err := store.Backlog(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("backlog error = %v, want %v", err, failure)
	}
	writabilityStore, err := newStore(&faultDatabase{row: faultRow{err: failure}}, StoreConfig{})
	if err != nil {
		t.Fatalf("create writability store: %v", err)
	}
	if err := writabilityStore.Ping(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("writability error = %v, want %v", err, failure)
	}
	readOnlyStore, err := newStore(&faultDatabase{row: boolRow(false)}, StoreConfig{})
	if err != nil {
		t.Fatalf("create read-only store: %v", err)
	}
	if err := readOnlyStore.Ping(context.Background()); !errors.Is(err, ErrNotWritable) {
		t.Fatalf("read-only error = %v, want %v", err, ErrNotWritable)
	}
	writableStore, err := newStore(&faultDatabase{row: boolRow(true)}, StoreConfig{})
	if err != nil {
		t.Fatalf("create writable store: %v", err)
	}
	if err := writableStore.Ping(context.Background()); err != nil {
		t.Fatalf("writable ping: %v", err)
	}
}

func TestClaimPreservesLeaseTokenFailure(t *testing.T) {
	t.Parallel()

	tokenErr := errors.New("entropy unavailable")
	pool := &pgxpool.Pool{}
	store, err := NewStore(pool, StoreConfig{LeaseTokenGenerator: func() (string, error) { return "", tokenErr }})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	_, err = store.Claim(context.Background(), ClaimRequest{Owner: "relay", Limit: 1, LeaseDuration: time.Second})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("token error = %v, want %v", err, tokenErr)
	}

	store, err = NewStore(pool, StoreConfig{LeaseTokenGenerator: func() (string, error) { return "", nil }})
	if err != nil {
		t.Fatalf("create empty-token store: %v", err)
	}
	if _, err := store.Claim(context.Background(), ClaimRequest{Owner: "relay", Limit: 1, LeaseDuration: time.Second}); err == nil {
		t.Fatal("expected empty token error")
	}
}

func TestStorePreservesPoolFailures(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(context.Background(), "postgres://outbox:outbox@127.0.0.1:1/outbox")
	if err != nil {
		t.Fatalf("create lazy pool: %v", err)
	}
	defer pool.Close()
	store, err := NewStore(pool, StoreConfig{LeaseTokenGenerator: func() (string, error) { return "token", nil }})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Claim(ctx, ClaimRequest{Owner: "relay", Limit: 1, LeaseDuration: time.Second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("claim error = %v", err)
	}
	if _, err := store.ExtendLease(ctx, LeaseRef{ID: "id", Token: "token"}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("extend error = %v", err)
	}
	if err := store.MarkDelivered(ctx, LeaseRef{ID: "id", Token: "token"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("delivery error = %v", err)
	}
}

func TestLeaseTokenAndErrorTextHelpers(t *testing.T) {
	t.Parallel()

	token, err := leaseTokenFromReader(bytes.NewReader(make([]byte, 16)))
	if err != nil || token != "00000000000000000000000000000000" {
		t.Fatalf("token/error = %q/%v", token, err)
	}
	readErr := errors.New("read failed")
	if _, err := leaseTokenFromReader(errorReader{err: readErr}); !errors.Is(err, readErr) {
		t.Fatalf("reader error = %v", err)
	}
	if token, err := randomLeaseToken(); err != nil || len(token) != 32 {
		t.Fatalf("random token/error = %q/%v", token, err)
	}
	if errorText(nil) != "" || errorText(readErr) != "operation failed" {
		t.Fatal("error text did not redact arbitrary failure details")
	}
}

func TestScanClaimsPreservesRowFailures(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("scan failed")
	if _, err := scanClaims(&faultRows{next: true, scanErr: scanErr}, 1); !errors.Is(err, scanErr) {
		t.Fatalf("scan error = %v", err)
	}
	if _, err := scanClaims(&faultRows{next: true, metadata: []byte("[")}, 1); err == nil {
		t.Fatal("expected malformed metadata error")
	}
	rowsErr := errors.New("stream failed")
	if _, err := scanClaims(&faultRows{rowsErr: rowsErr}, 1); !errors.Is(err, rowsErr) {
		t.Fatalf("rows error = %v", err)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type faultRows struct {
	pgx.Rows
	next     bool
	scanErr  error
	metadata []byte
	rowsErr  error
	scan     func([]any) error
}

type storeObserver struct {
	events []outbox.Event
}

func (observer *storeObserver) Observe(_ context.Context, event outbox.Event) {
	observer.events = append(observer.events, event)
}

func (rows *faultRows) Next() bool {
	if !rows.next {
		return false
	}
	rows.next = false

	return true
}

func (rows *faultRows) Scan(destinations ...any) error {
	if rows.scan != nil {
		return rows.scan(destinations)
	}
	if rows.scanErr != nil {
		return rows.scanErr
	}
	*destinations[0].(*string) = "message-id"
	*destinations[4].(*[]byte) = rows.metadata

	return nil
}

func (rows *faultRows) Err() error {
	return rows.rowsErr
}

func (*faultRows) Close() {}

type faultDatabase struct {
	tx       pgx.Tx
	beginErr error
	rows     pgx.Rows
	queryErr error
	pingErr  error
	row      pgx.Row
}

func (database *faultDatabase) Ping(context.Context) error {
	return database.pingErr
}

func (database *faultDatabase) Begin(context.Context) (pgx.Tx, error) {
	return database.tx, database.beginErr
}

func (*faultDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (database *faultDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return database.rows, database.queryErr
}

func (database *faultDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	if database.row != nil {
		return database.row
	}

	return faultRow{err: errors.New("unexpected QueryRow")}
}

type faultRow struct {
	err error
}

type boolRow bool

func (row boolRow) Scan(destinations ...any) error {
	*destinations[0].(*bool) = bool(row)

	return nil
}

func (row faultRow) Scan(...any) error {
	return row.err
}

type faultTx struct {
	pgx.Tx
	rows      pgx.Rows
	queryErr  error
	execErr   error
	commitErr error
	rollback  func(context.Context)
}

func (tx *faultTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.rows, tx.queryErr
}

func (tx *faultTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.execErr
}

func (tx *faultTx) Commit(context.Context) error {
	return tx.commitErr
}

func (tx *faultTx) Rollback(ctx context.Context) error {
	if tx.rollback != nil {
		tx.rollback(ctx)
	}

	return nil
}

func replayRows() pgx.Rows {
	return &faultRows{
		next: true,
		scan: func(destinations []any) error {
			*destinations[0].(*string) = "message-id"
			*destinations[1].(*string) = "delivered"
			*destinations[2].(*time.Time) = time.Now()

			return nil
		},
	}
}

func deliveredRows(metadata []byte) pgx.Rows {
	return &faultRows{
		next: true,
		scan: func(destinations []any) error {
			*destinations[0].(*string) = "message-id"
			*destinations[1].(*string) = "topic"
			*destinations[2].(*[]byte) = []byte(`{}`)
			*destinations[3].(*uint16) = 1
			*destinations[4].(*[]byte) = metadata
			*destinations[5].(*string) = "ordering-key"
			*destinations[6].(*string) = "idempotency-key"
			*destinations[7].(*int) = 1
			*destinations[8].(*time.Time) = time.Now()
			*destinations[9].(*time.Time) = time.Now()
			*destinations[10].(*time.Time) = time.Now()

			return nil
		},
	}
}

func idRows(id string) pgx.Rows {
	return &faultRows{next: true, scan: func(destinations []any) error {
		*destinations[0].(*string) = id

		return nil
	}}
}
