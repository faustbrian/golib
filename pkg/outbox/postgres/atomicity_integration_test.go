//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	outboxrelay "github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestApplicationWriteAndOutboxRecordAreAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	version := os.Getenv("OUTBOX_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(ctx, "postgres:"+version+"-alpine",
		tcpostgres.WithDatabase("outbox"),
		tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)

	migration := migrationUpSQL(t)
	if _, err := pool.Exec(ctx, migration); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE orders (id bigint PRIMARY KEY)"); err != nil {
		t.Fatalf("create application table: %v", err)
	}

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "00000000-0000-4000-8000-000000000001", Topic: "orders.created",
		Payload: []byte(`{"id":1}`), PayloadVersion: 1,
		AvailableAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	}

	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback transaction: %v", err)
	}
	if _, err := rollbackTx.Exec(ctx, "INSERT INTO orders (id) VALUES (1)"); err != nil {
		t.Fatalf("insert rollback application row: %v", err)
	}
	if err := writer.Insert(ctx, rollbackTx, envelope); err != nil {
		t.Fatalf("insert rollback outbox row: %v", err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}
	assertCounts(t, ctx, pool, 0, 0)

	commitTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin commit transaction: %v", err)
	}
	if _, err := commitTx.Exec(ctx, "INSERT INTO orders (id) VALUES (1)"); err != nil {
		t.Fatalf("insert committed application row: %v", err)
	}
	if err := writer.Insert(ctx, commitTx, envelope); err != nil {
		t.Fatalf("insert committed outbox row: %v", err)
	}
	if err := commitTx.Commit(ctx); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	assertCounts(t, ctx, pool, 1, 1)

	claimEnvelopes := make([]outbox.Envelope, 0, 6)
	for index := 2; index <= 7; index++ {
		claimEnvelopes = append(claimEnvelopes, outbox.Envelope{
			ID:             "00000000-0000-4000-8000-" + string(rune('0'+index)) + "00000000000",
			Topic:          "orders.created",
			Payload:        []byte(`{}`),
			PayloadVersion: 1,
			AvailableAt:    time.Now().Add(-time.Minute),
			CreatedAt:      time.Now().Add(time.Duration(index) * time.Microsecond),
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin claim fixture transaction: %v", err)
	}
	if err := writer.InsertBatch(ctx, tx, claimEnvelopes); err != nil {
		t.Fatalf("insert claim fixtures: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit claim fixtures: %v", err)
	}

	store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
		MaxClaimBatch: 4,
		ReplayAuthorizer: outboxpostgres.ReplayAuthorizeFunc(
			func(context.Context, outboxpostgres.ReplayRequest) error { return nil },
		),
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	requests := []outboxpostgres.ClaimRequest{
		{Owner: "relay-a", Limit: 4, LeaseDuration: time.Minute},
		{Owner: "relay-b", Limit: 4, LeaseDuration: time.Minute},
	}
	results := make([][]outboxpostgres.Claim, len(requests))
	claimErrors := make([]error, len(requests))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], claimErrors[index] = store.Claim(ctx, requests[index])
		}(index)
	}
	close(start)
	wait.Wait()

	claimed := make(map[string]string)
	for index, claims := range results {
		if claimErrors[index] != nil {
			t.Fatalf("claim for %s: %v", requests[index].Owner, claimErrors[index])
		}
		if len(claims) > requests[index].Limit {
			t.Fatalf("claim count = %d, limit %d", len(claims), requests[index].Limit)
		}
		for _, claim := range claims {
			if previousOwner, exists := claimed[claim.Envelope.ID]; exists {
				t.Fatalf("message %s claimed by %s and %s", claim.Envelope.ID, previousOwner, claim.Owner)
			}
			if claim.Owner != requests[index].Owner || claim.LeaseToken == "" || claim.Envelope.Attempts != 1 {
				t.Fatalf("invalid claim: %#v", claim)
			}
			claimed[claim.Envelope.ID] = claim.Owner
		}
	}
	if len(claimed) != 7 {
		t.Fatalf("claimed %d messages, want all 7", len(claimed))
	}

	allClaims := append(append([]outboxpostgres.Claim(nil), results[0]...), results[1]...)
	first := allClaims[0]
	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: first.Envelope.ID, Token: first.LeaseToken}); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: first.Envelope.ID, Token: first.LeaseToken}); !errors.Is(err, outboxpostgres.ErrLeaseLost) {
		t.Fatalf("second delivery error = %v, want %v", err, outboxpostgres.ErrLeaseLost)
	}

	second := allClaims[1]
	extendedUntil, err := store.ExtendLease(ctx, outboxpostgres.LeaseRef{ID: second.Envelope.ID, Token: second.LeaseToken}, 2*time.Minute)
	if err != nil {
		t.Fatalf("extend lease: %v", err)
	}
	if !extendedUntil.After(second.LeasedUntil) {
		t.Fatalf("extended lease %s is not after %s", extendedUntil, second.LeasedUntil)
	}

	third := allClaims[2]
	if err := store.Retry(ctx, outboxpostgres.LeaseRef{ID: third.Envelope.ID, Token: third.LeaseToken}, time.Minute, errors.New("publisher unavailable")); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}

	fourth := allClaims[3]
	if err := store.DeadLetter(ctx, outboxpostgres.LeaseRef{ID: fourth.Envelope.ID, Token: fourth.LeaseToken}, errors.New("invalid routing key")); err != nil {
		t.Fatalf("dead letter: %v", err)
	}

	fifth := allClaims[4]
	if _, err := pool.Exec(ctx, "UPDATE outbox_messages SET leased_until = clock_timestamp() - interval '1 second' WHERE id = $1", fifth.Envelope.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	reclaimed, err := store.Claim(ctx, outboxpostgres.ClaimRequest{Owner: "relay-c", Limit: 1, LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("reclaim expired lease: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].Envelope.ID != fifth.Envelope.ID ||
		reclaimed[0].LeaseToken == fifth.LeaseToken || reclaimed[0].Envelope.Attempts != 2 {
		t.Fatalf("unexpected reclaimed lease: %#v", reclaimed)
	}
	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: fifth.Envelope.ID, Token: fifth.LeaseToken}); !errors.Is(err, outboxpostgres.ErrLeaseLost) {
		t.Fatalf("stale delivery error = %v, want %v", err, outboxpostgres.ErrLeaseLost)
	}
	if _, err := store.ExtendLease(ctx, outboxpostgres.LeaseRef{ID: fifth.Envelope.ID, Token: fifth.LeaseToken}, time.Second); !errors.Is(err, outboxpostgres.ErrLeaseLost) {
		t.Fatalf("stale extension error = %v, want %v", err, outboxpostgres.ErrLeaseLost)
	}

	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: reclaimed[0].Envelope.ID, Token: reclaimed[0].LeaseToken}); err != nil {
		t.Fatalf("deliver reclaimed message: %v", err)
	}
	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: second.Envelope.ID, Token: second.LeaseToken}); err != nil {
		t.Fatalf("deliver extended message: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE outbox_messages SET delivered_at = clock_timestamp() - interval '48 hours' WHERE id = $1", reclaimed[0].Envelope.ID); err != nil {
		t.Fatalf("age delivered message: %v", err)
	}

	replayed, err := store.Replay(ctx, outboxpostgres.ReplayRequest{
		IDs:         []string{first.Envelope.ID, fourth.Envelope.ID},
		RequestedBy: "operator@example.com",
		Reason:      "incident-42 broker recovery",
		AvailableAt: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("replay terminal messages: %v", err)
	}
	if len(replayed) != 2 {
		t.Fatalf("replayed %d messages, want 2", len(replayed))
	}
	var auditRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_replay_audit WHERE requested_by = $1", "operator@example.com").Scan(&auditRows); err != nil {
		t.Fatalf("count replay audit: %v", err)
	}
	if auditRows != 2 {
		t.Fatalf("audit rows = %d, want 2", auditRows)
	}

	_, err = store.Replay(ctx, outboxpostgres.ReplayRequest{
		IDs:         []string{second.Envelope.ID, "missing-id"},
		RequestedBy: "operator@example.com",
		Reason:      "must remain atomic",
	})
	if !errors.Is(err, outboxpostgres.ErrReplayConflict) {
		t.Fatalf("partial replay error = %v, want %v", err, outboxpostgres.ErrReplayConflict)
	}
	var secondState string
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_messages WHERE id = $1", second.Envelope.ID).Scan(&secondState); err != nil {
		t.Fatalf("read conflicted replay state: %v", err)
	}
	if secondState != "delivered" {
		t.Fatalf("conflicted replay changed state to %q", secondState)
	}

	archiveFailure := errors.New("archive unavailable")
	if _, err := store.ArchiveAndPruneDelivered(
		ctx,
		time.Now().Add(-24*time.Hour),
		10,
		outboxpostgres.ArchiveFunc(func(context.Context, []outboxpostgres.DeliveredMessage) error {
			return archiveFailure
		}),
	); !errors.Is(err, archiveFailure) {
		t.Fatalf("archive failure = %v, want %v", err, archiveFailure)
	}
	var retainedAfterArchiveFailure int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE id = $1", reclaimed[0].Envelope.ID).Scan(&retainedAfterArchiveFailure); err != nil {
		t.Fatalf("check failed archive retention: %v", err)
	}
	if retainedAfterArchiveFailure != 1 {
		t.Fatal("archive failure deleted the delivered message")
	}

	var archived []outboxpostgres.DeliveredMessage
	archivedIDs, err := store.ArchiveAndPruneDelivered(
		ctx,
		time.Now().Add(-24*time.Hour),
		10,
		outboxpostgres.ArchiveFunc(func(_ context.Context, messages []outboxpostgres.DeliveredMessage) error {
			archived = append(archived, messages...)

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("archive delivered: %v", err)
	}
	if len(archivedIDs) != 1 || archivedIDs[0] != reclaimed[0].Envelope.ID ||
		len(archived) != 1 || archived[0].Envelope.ID != reclaimed[0].Envelope.ID ||
		archived[0].DeliveredAt.IsZero() {
		t.Fatalf("archived records/IDs = %#v/%#v", archived, archivedIDs)
	}
	var recentDelivered int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE id = $1 AND state = 'delivered'", second.Envelope.ID).Scan(&recentDelivered); err != nil {
		t.Fatalf("check recent delivered message: %v", err)
	}
	if recentDelivered != 1 {
		t.Fatal("archival removed a recent delivered message")
	}
	if _, err := pool.Exec(ctx, "UPDATE outbox_messages SET delivered_at = clock_timestamp() - interval '48 hours' WHERE id = $1", second.Envelope.ID); err != nil {
		t.Fatalf("age second delivered message: %v", err)
	}
	pruned, err := store.PruneDelivered(ctx, time.Now().Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("prune delivered: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != second.Envelope.ID {
		t.Fatalf("pruned IDs = %#v", pruned)
	}

	sixth := allClaims[5]
	if err := store.ReleaseLease(ctx, outboxpostgres.LeaseRef{ID: sixth.Envelope.ID, Token: sixth.LeaseToken}); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	var releasedState string
	var releasedToken *string
	if err := pool.QueryRow(ctx, "SELECT state, lease_token FROM outbox_messages WHERE id = $1", sixth.Envelope.ID).Scan(&releasedState, &releasedToken); err != nil {
		t.Fatalf("read released lease: %v", err)
	}
	if releasedState != "pending" || releasedToken != nil {
		t.Fatalf("released state/token = %q/%v", releasedState, releasedToken)
	}
	if err := store.ReleaseLease(ctx, outboxpostgres.LeaseRef{ID: sixth.Envelope.ID, Token: sixth.LeaseToken}); !errors.Is(err, outboxpostgres.ErrLeaseLost) {
		t.Fatalf("stale release error = %v, want %v", err, outboxpostgres.ErrLeaseLost)
	}

	orderingFixtures := []outbox.Envelope{
		{ID: "order-a-1", Topic: "ordered", Payload: []byte{}, OrderingKey: "account-a", PayloadVersion: 1, AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0)},
		{ID: "order-a-2", Topic: "ordered", Payload: []byte{}, OrderingKey: "account-a", PayloadVersion: 1, AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(2, 0)},
		{ID: "order-b-1", Topic: "ordered", Payload: []byte{}, OrderingKey: "account-b", PayloadVersion: 1, AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0)},
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ordering fixtures: %v", err)
	}
	if err := writer.InsertBatch(ctx, tx, orderingFixtures); err != nil {
		t.Fatalf("insert ordering fixtures: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ordering fixtures: %v", err)
	}
	orderedClaims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "ordered-relay", Limit: 2, LeaseDuration: time.Minute,
		Serialization: outboxpostgres.SerializeByOrderingKey,
	})
	if err != nil {
		t.Fatalf("claim ordering keys: %v", err)
	}
	assertClaimedIDs(t, orderedClaims, "order-a-1", "order-b-1")
	if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: orderedClaims[0].Envelope.ID, Token: orderedClaims[0].LeaseToken}); err != nil {
		t.Fatalf("deliver first ordered claim: %v", err)
	}
	nextOrdered, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "ordered-relay", Limit: 1, LeaseDuration: time.Minute,
		Serialization: outboxpostgres.SerializeByOrderingKey,
	})
	if err != nil {
		t.Fatalf("claim next ordering key: %v", err)
	}
	if len(nextOrdered) != 1 || nextOrdered[0].Envelope.ID != "order-a-2" {
		t.Fatalf("next ordered claim = %#v", nextOrdered)
	}

	topicFixtures := []outbox.Envelope{
		{ID: "topic-a-1", Topic: "topic-a", Payload: []byte{}, PayloadVersion: 1, AvailableAt: time.Unix(-100, 0), CreatedAt: time.Unix(-100, 0)},
		{ID: "topic-a-2", Topic: "topic-a", Payload: []byte{}, PayloadVersion: 1, AvailableAt: time.Unix(-100, 0), CreatedAt: time.Unix(-99, 0)},
		{ID: "topic-b-1", Topic: "topic-b", Payload: []byte{}, PayloadVersion: 1, AvailableAt: time.Unix(-100, 0), CreatedAt: time.Unix(-100, 0)},
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin topic fixtures: %v", err)
	}
	if err := writer.InsertBatch(ctx, tx, topicFixtures); err != nil {
		t.Fatalf("insert topic fixtures: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit topic fixtures: %v", err)
	}
	topicClaims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "topic-relay", Limit: 2, LeaseDuration: time.Minute,
		Serialization: outboxpostgres.SerializeByTopic,
	})
	if err != nil {
		t.Fatalf("claim topics: %v", err)
	}
	assertClaimedIDs(t, topicClaims, "topic-a-1", "topic-b-1")
	for _, topicClaim := range topicClaims {
		if topicClaim.Envelope.ID == "topic-a-1" {
			if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{ID: topicClaim.Envelope.ID, Token: topicClaim.LeaseToken}); err != nil {
				t.Fatalf("deliver first topic claim: %v", err)
			}
		}
	}
	nextTopic, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "topic-relay", Limit: 1, LeaseDuration: time.Minute,
		Serialization: outboxpostgres.SerializeByTopic,
	})
	if err != nil {
		t.Fatalf("claim next topic: %v", err)
	}
	if len(nextTopic) != 1 || nextTopic[0].Envelope.ID != "topic-a-2" {
		t.Fatalf("next topic claim = %#v", nextTopic)
	}

	raceFixtures := []outbox.Envelope{
		{ID: "race-key-1", Topic: "race", Payload: []byte{}, OrderingKey: "shared", PayloadVersion: 1, AvailableAt: time.Unix(-200, 0), CreatedAt: time.Unix(-200, 0)},
		{ID: "race-key-2", Topic: "race", Payload: []byte{}, OrderingKey: "shared", PayloadVersion: 1, AvailableAt: time.Unix(-200, 0), CreatedAt: time.Unix(-199, 0)},
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ordering race fixtures: %v", err)
	}
	if err := writer.InsertBatch(ctx, tx, raceFixtures); err != nil {
		t.Fatalf("insert ordering race fixtures: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ordering race fixtures: %v", err)
	}
	raceStart := make(chan struct{})
	raceErrors := make([]error, 2)
	var raceWait sync.WaitGroup
	for index := range raceErrors {
		raceWait.Add(1)
		go func(index int) {
			defer raceWait.Done()
			<-raceStart
			_, raceErrors[index] = store.Claim(ctx, outboxpostgres.ClaimRequest{
				Owner: "ordering-race", Limit: 1, LeaseDuration: time.Minute,
				Serialization: outboxpostgres.SerializeByOrderingKey,
			})
		}(index)
	}
	close(raceStart)
	raceWait.Wait()
	for _, raceErr := range raceErrors {
		if raceErr != nil {
			t.Fatalf("concurrent ordered claim: %v", raceErr)
		}
	}
	var firstRaceState, secondRaceState string
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_messages WHERE id = 'race-key-1'").Scan(&firstRaceState); err != nil {
		t.Fatalf("read first race state: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_messages WHERE id = 'race-key-2'").Scan(&secondRaceState); err != nil {
		t.Fatalf("read second race state: %v", err)
	}
	if firstRaceState != "leased" || secondRaceState != "pending" {
		t.Fatalf("ordered race states = %q/%q", firstRaceState, secondRaceState)
	}

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping store: %v", err)
	}
	stats, err := store.Backlog(ctx)
	if err != nil {
		t.Fatalf("read backlog: %v", err)
	}
	var wantPending, wantLeased, wantDead int64
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE state = 'pending'),
       count(*) FILTER (WHERE state = 'leased'),
       count(*) FILTER (WHERE state = 'dead')
FROM outbox_messages`).Scan(&wantPending, &wantLeased, &wantDead); err != nil {
		t.Fatalf("read expected backlog: %v", err)
	}
	if stats.Pending != wantPending || stats.Leased != wantLeased || stats.Dead != wantDead || stats.OldestPendingAt == nil {
		t.Fatalf("backlog = %#v, want pending/leased/dead %d/%d/%d", stats, wantPending, wantLeased, wantDead)
	}

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_heartbeat (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create heartbeat table: %v", err)
	}
	heartbeatWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Table: "outbox_heartbeat"})
	if err != nil {
		t.Fatalf("create heartbeat writer: %v", err)
	}
	heartbeatEnvelope := outbox.Envelope{
		ID: "heartbeat-message", Topic: "heartbeat", Payload: []byte(`{}`),
		PayloadVersion: 1, AvailableAt: time.Now().Add(-time.Second), CreatedAt: time.Now(),
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin heartbeat insert: %v", err)
	}
	if err := heartbeatWriter.Insert(ctx, tx, heartbeatEnvelope); err != nil {
		t.Fatalf("insert heartbeat message: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit heartbeat message: %v", err)
	}
	heartbeatStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{Table: "outbox_heartbeat"})
	if err != nil {
		t.Fatalf("create heartbeat store: %v", err)
	}
	publisher := &blockingPublisher{release: make(chan struct{})}
	extended := make(chan struct{})
	worker, err := outboxrelay.New(heartbeatStore, publisher, outboxrelay.Config{
		Owner: "heartbeat-relay", LeaseDuration: 30 * time.Second,
		Heartbeat: func(heartbeatContext context.Context, _ time.Duration, extend func(context.Context) error) error {
			if err := extend(heartbeatContext); err != nil {
				return err
			}
			close(extended)
			<-heartbeatContext.Done()

			return nil
		},
	})
	if err != nil {
		t.Fatalf("create heartbeat relay: %v", err)
	}
	relayDone := make(chan error, 1)
	go func() {
		_, runErr := worker.RunOnce(ctx)
		relayDone <- runErr
	}()
	<-extended
	var heartbeatState string
	var heartbeatDeadline time.Time
	if err := pool.QueryRow(ctx, "SELECT state, leased_until FROM outbox_heartbeat WHERE id = $1", heartbeatEnvelope.ID).Scan(&heartbeatState, &heartbeatDeadline); err != nil {
		t.Fatalf("read renewed heartbeat lease: %v", err)
	}
	if heartbeatState != "leased" || !heartbeatDeadline.After(time.Now().Add(20*time.Second)) {
		t.Fatalf("heartbeat state/deadline = %q/%s", heartbeatState, heartbeatDeadline)
	}
	close(publisher.release)
	if err := <-relayDone; err != nil {
		t.Fatalf("run heartbeat relay: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_heartbeat WHERE id = $1", heartbeatEnvelope.ID).Scan(&heartbeatState); err != nil {
		t.Fatalf("read heartbeat delivery: %v", err)
	}
	if heartbeatState != "delivered" {
		t.Fatalf("heartbeat final state = %q", heartbeatState)
	}
	summaries, err := heartbeatStore.Inspect(ctx, outboxpostgres.InspectRequest{
		State: outboxpostgres.MessageStateDelivered, Topic: "heartbeat", Limit: 10,
	})
	if err != nil {
		t.Fatalf("inspect delivered heartbeat: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != heartbeatEnvelope.ID ||
		summaries[0].State != outboxpostgres.MessageStateDelivered || summaries[0].DeliveredAt == nil {
		t.Fatalf("heartbeat summaries = %#v", summaries)
	}

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_cancel (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create cancellation table: %v", err)
	}
	cancelWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Table: "outbox_cancel"})
	if err != nil {
		t.Fatalf("create cancellation writer: %v", err)
	}
	cancelEnvelope := outbox.Envelope{
		ID: "cancel-message", Topic: "cancel", Payload: []byte(`{}`),
		PayloadVersion: 1, AvailableAt: time.Now().Add(-time.Second), CreatedAt: time.Now(),
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cancellation insert: %v", err)
	}
	if err := cancelWriter.Insert(ctx, tx, cancelEnvelope); err != nil {
		t.Fatalf("insert cancellation message: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit cancellation message: %v", err)
	}
	cancelStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{Table: "outbox_cancel"})
	if err != nil {
		t.Fatalf("create cancellation store: %v", err)
	}
	publishStarted := make(chan struct{})
	cancelPublisher := &blockingPublisher{release: make(chan struct{}), started: publishStarted}
	cancelContext, cancelRelay := context.WithCancel(ctx)
	cancelWorker, err := outboxrelay.New(
		cancelStore, cancelPublisher,
		outboxrelay.Config{Owner: "cancel-relay", LeaseDuration: 30 * time.Second},
	)
	if err != nil {
		t.Fatalf("create cancellation relay: %v", err)
	}
	type cancellationResult struct {
		result outboxrelay.Result
		err    error
	}
	cancelDone := make(chan cancellationResult, 1)
	go func() {
		result, runErr := cancelWorker.RunOnce(cancelContext)
		cancelDone <- cancellationResult{result: result, err: runErr}
	}()
	<-publishStarted
	cancelRelay()
	canceled := <-cancelDone
	if canceled.err != nil || canceled.result.Released != 1 {
		t.Fatalf("canceled relay result/error = %#v/%v", canceled.result, canceled.err)
	}
	var canceledState string
	var canceledToken *string
	if err := pool.QueryRow(ctx, "SELECT state, lease_token FROM outbox_cancel WHERE id = $1", cancelEnvelope.ID).Scan(&canceledState, &canceledToken); err != nil {
		t.Fatalf("read canceled lease: %v", err)
	}
	if canceledState != "pending" || canceledToken != nil {
		t.Fatalf("canceled state/token = %q/%v", canceledState, canceledToken)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO outbox_heartbeat (
    id, topic, payload, payload_version, available_at, created_at, state,
    dead_lettered_at, last_error
) VALUES
    ('dead-archive', 'dead', '{}'::bytea, 1, clock_timestamp(), clock_timestamp() - interval '3 days', 'dead', clock_timestamp() - interval '2 days', 'permanent failure'),
    ('dead-prune', 'dead', '{}'::bytea, 1, clock_timestamp(), clock_timestamp() - interval '2 days', 'dead', clock_timestamp() - interval '2 days', 'permanent failure')`); err != nil {
		t.Fatalf("insert dead retention fixtures: %v", err)
	}
	var deadArchived []outboxpostgres.DeadMessage
	deadArchiveIDs, err := heartbeatStore.ArchiveAndPruneDead(
		ctx, time.Now().Add(-24*time.Hour), 1,
		outboxpostgres.DeadArchiveFunc(func(_ context.Context, messages []outboxpostgres.DeadMessage) error {
			deadArchived = append(deadArchived, messages...)

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("archive dead letter: %v", err)
	}
	if len(deadArchiveIDs) != 1 || deadArchiveIDs[0] != "dead-archive" ||
		len(deadArchived) != 1 || deadArchived[0].LastError != "permanent failure" {
		t.Fatalf("dead archive IDs/messages = %#v/%#v", deadArchiveIDs, deadArchived)
	}
	deadPruned, err := heartbeatStore.PruneDead(ctx, time.Now().Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("prune dead letter: %v", err)
	}
	if len(deadPruned) != 1 || deadPruned[0] != "dead-prune" {
		t.Fatalf("dead pruned IDs = %#v", deadPruned)
	}

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_duplicate (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create duplicate table: %v", err)
	}
	duplicateWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Table: "outbox_duplicate"})
	if err != nil {
		t.Fatalf("create duplicate writer: %v", err)
	}
	duplicateEnvelope := outbox.Envelope{
		ID: "duplicate-window", Topic: "duplicates", Payload: []byte(`{}`),
		PayloadVersion: 1, AvailableAt: time.Now().Add(-time.Second), CreatedAt: time.Now(),
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin duplicate insert: %v", err)
	}
	if err := duplicateWriter.Insert(ctx, tx, duplicateEnvelope); err != nil {
		t.Fatalf("insert duplicate message: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit duplicate message: %v", err)
	}
	duplicateStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{Table: "outbox_duplicate"})
	if err != nil {
		t.Fatalf("create duplicate store: %v", err)
	}
	deliveredFailure := errors.New("ambiguous delivered update")
	publishCounter := &countingPublisher{}
	firstRelay, err := outboxrelay.New(
		&failDeliveryStore{Store: duplicateStore, err: deliveredFailure},
		publishCounter,
		outboxrelay.Config{Owner: "duplicate-first", LeaseDuration: 30 * time.Second},
	)
	if err != nil {
		t.Fatalf("create first duplicate relay: %v", err)
	}
	if _, err := firstRelay.RunOnce(ctx); !errors.Is(err, deliveredFailure) {
		t.Fatalf("first duplicate relay error = %v, want %v", err, deliveredFailure)
	}
	var duplicateState string
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_duplicate WHERE id = $1", duplicateEnvelope.ID).Scan(&duplicateState); err != nil {
		t.Fatalf("read ambiguous delivery state: %v", err)
	}
	if duplicateState != "leased" || publishCounter.Count() != 1 {
		t.Fatalf("ambiguous state/publications = %q/%d", duplicateState, publishCounter.Count())
	}
	if _, err := pool.Exec(ctx, "UPDATE outbox_duplicate SET leased_until = clock_timestamp() - interval '1 second' WHERE id = $1", duplicateEnvelope.ID); err != nil {
		t.Fatalf("expire ambiguous lease: %v", err)
	}
	secondRelay, err := outboxrelay.New(
		duplicateStore, publishCounter,
		outboxrelay.Config{Owner: "duplicate-second", LeaseDuration: 30 * time.Second},
	)
	if err != nil {
		t.Fatalf("create second duplicate relay: %v", err)
	}
	if _, err := secondRelay.RunOnce(ctx); err != nil {
		t.Fatalf("second duplicate relay: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT state FROM outbox_duplicate WHERE id = $1", duplicateEnvelope.ID).Scan(&duplicateState); err != nil {
		t.Fatalf("read recovered delivery state: %v", err)
	}
	if duplicateState != "delivered" || publishCounter.Count() != 2 {
		t.Fatalf("recovered state/publications = %q/%d", duplicateState, publishCounter.Count())
	}

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_publisher_timeout (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create publisher-timeout table: %v", err)
	}
	timeoutWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Table: "outbox_publisher_timeout"})
	if err != nil {
		t.Fatalf("create publisher-timeout writer: %v", err)
	}
	timeoutEnvelope := outbox.Envelope{
		ID: "publisher-ambiguous-timeout", Topic: "duplicates", Payload: []byte(`{}`),
		PayloadVersion: 1, AvailableAt: time.Now().Add(-time.Second), CreatedAt: time.Now(),
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin publisher-timeout insert: %v", err)
	}
	if err := timeoutWriter.Insert(ctx, tx, timeoutEnvelope); err != nil {
		t.Fatalf("insert publisher-timeout message: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit publisher-timeout message: %v", err)
	}
	timeoutStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{Table: "outbox_publisher_timeout"})
	if err != nil {
		t.Fatalf("create publisher-timeout store: %v", err)
	}
	timeoutPublisher := &ambiguousTimeoutPublisher{
		err: errors.New("publisher timed out after possible acceptance"),
	}
	timeoutRelay, err := outboxrelay.New(
		timeoutStore, timeoutPublisher,
		outboxrelay.Config{
			Owner: "publisher-timeout", LeaseDuration: 30 * time.Second,
			Clock:   func() time.Time { return time.Now().Add(-time.Minute) },
			Backoff: func(int) time.Duration { return 0 },
		},
	)
	if err != nil {
		t.Fatalf("create publisher-timeout relay: %v", err)
	}
	firstTimeoutResult, err := timeoutRelay.RunOnce(ctx)
	if err != nil {
		t.Fatalf("first ambiguous publisher timeout: %v", err)
	}
	var timeoutState string
	var timeoutError string
	if err := pool.QueryRow(ctx, `
SELECT state, last_error
FROM outbox_publisher_timeout
WHERE id = $1`, timeoutEnvelope.ID).Scan(&timeoutState, &timeoutError); err != nil {
		t.Fatalf("read ambiguous publisher retry: %v", err)
	}
	if firstTimeoutResult.Retried != 1 || timeoutState != "pending" ||
		timeoutError != "operation failed" || timeoutPublisher.Count() != 1 {
		t.Fatalf("first timeout result/state/error/publications = %#v/%q/%q/%d",
			firstTimeoutResult, timeoutState, timeoutError, timeoutPublisher.Count())
	}
	secondTimeoutResult, err := timeoutRelay.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second ambiguous publisher attempt: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT state FROM outbox_publisher_timeout WHERE id = $1", timeoutEnvelope.ID,
	).Scan(&timeoutState); err != nil {
		t.Fatalf("read ambiguous publisher recovery: %v", err)
	}
	if secondTimeoutResult.Delivered != 1 || timeoutState != "delivered" ||
		timeoutPublisher.Count() != 2 {
		t.Fatalf("recovered timeout result/state/publications = %#v/%q/%d",
			secondTimeoutResult, timeoutState, timeoutPublisher.Count())
	}

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_plan (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create plan table: %v", err)
	}
	claimQuery := `
SELECT id
FROM outbox_plan
WHERE ((state = 'pending' AND available_at <= clock_timestamp())
   OR (state = 'leased' AND leased_until <= clock_timestamp()))
ORDER BY available_at, created_at, id
LIMIT 100`
	deliveredRetentionQuery := `
SELECT id
FROM outbox_plan
WHERE state = 'delivered' AND delivered_at < clock_timestamp() - interval '1 day'
ORDER BY delivered_at, id
LIMIT 100`
	deadRetentionQuery := `
SELECT id
FROM outbox_plan
WHERE state = 'dead' AND dead_lettered_at < clock_timestamp() - interval '1 day'
ORDER BY dead_lettered_at, id
LIMIT 100`
	planQueries := []struct {
		name  string
		query string
	}{
		{name: "claim", query: claimQuery},
		{name: "delivered retention", query: deliveredRetentionQuery},
		{name: "dead retention", query: deadRetentionQuery},
	}
	explainPlans := func(size string) map[string]string {
		t.Helper()

		plans := make(map[string]string, len(planQueries))
		for _, planQuery := range planQueries {
			plan := explainPlan(t, ctx, pool, planQuery.query)
			if strings.TrimSpace(plan) == "" || !strings.Contains(plan, "Scan") {
				t.Fatalf("%s %s plan is incomplete:\n%s", size, planQuery.name, plan)
			}
			t.Logf("%s %s plan:\n%s", size, planQuery.name, plan)
			plans[planQuery.name] = plan
		}

		return plans
	}
	if _, err := pool.Exec(ctx, "ANALYZE outbox_plan"); err != nil {
		t.Fatalf("analyze empty plan table: %v", err)
	}
	explainPlans("empty")

	if _, err := pool.Exec(ctx, `
INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
)
SELECT 'normal-delivered-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '2 days',
       clock_timestamp() - interval '2 days' + value * interval '1 microsecond',
       'delivered', clock_timestamp() - interval '2 days'
FROM generate_series(1, 50) AS value;

INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at, state,
    dead_lettered_at
)
SELECT 'normal-dead-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '2 days',
       clock_timestamp() - interval '2 days' + value * interval '1 microsecond',
       'dead', clock_timestamp() - interval '2 days'
FROM generate_series(1, 50) AS value;

INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at
)
SELECT 'normal-pending-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '1 minute',
       clock_timestamp() + value * interval '1 microsecond'
FROM generate_series(1, 50) AS value;

ANALYZE outbox_plan`); err != nil {
		t.Fatalf("seed normal plan table: %v", err)
	}
	explainPlans("normal")

	if _, err := pool.Exec(ctx, "TRUNCATE outbox_plan"); err != nil {
		t.Fatalf("truncate normal plan table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
)
SELECT 'delivered-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '2 days',
       clock_timestamp() - interval '2 days' + value * interval '1 microsecond',
       'delivered', clock_timestamp() - interval '2 days'
FROM generate_series(1, 20000) AS value;

INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at, state,
    dead_lettered_at
)
SELECT 'dead-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '2 days',
       clock_timestamp() - interval '2 days' + value * interval '1 microsecond',
       'dead', clock_timestamp() - interval '2 days'
FROM generate_series(1, 20000) AS value;

INSERT INTO outbox_plan (
    id, topic, payload, payload_version, available_at, created_at
)
SELECT 'pending-' || value, 'plan', '{}'::bytea, 1,
       clock_timestamp() - interval '1 minute',
       clock_timestamp() + value * interval '1 microsecond'
FROM generate_series(1, 100) AS value;

ANALYZE outbox_plan`); err != nil {
		t.Fatalf("seed large plan table: %v", err)
	}
	largePlans := explainPlans("large")
	claimPlan := largePlans["claim"]
	if !usesIndex(claimPlan) || strings.Contains(claimPlan, "Seq Scan") {
		t.Fatalf("claim plan does not use hot-set index:\n%s", claimPlan)
	}
	deliveredPlan := largePlans["delivered retention"]
	if !usesIndex(deliveredPlan) || strings.Contains(deliveredPlan, "Seq Scan") {
		t.Fatalf("delivered retention plan does not use terminal index:\n%s", deliveredPlan)
	}
	deadPlan := largePlans["dead retention"]
	if !usesIndex(deadPlan) || strings.Contains(deadPlan, "Seq Scan") {
		t.Fatalf("dead retention plan does not use terminal index:\n%s", deadPlan)
	}

	proveCanceledTransitionsAreAtomic(t, ctx, pool)

	if _, err := pool.Exec(ctx, migrationDownSQL(t)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	var messageTable, auditTable *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.outbox_messages')::text, to_regclass('public.outbox_replay_audit')::text").Scan(&messageTable, &auditTable); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if messageTable != nil || auditTable != nil {
		t.Fatalf("down migration retained tables: %v/%v", messageTable, auditTable)
	}
	if _, err := pool.Exec(ctx, migration); err != nil {
		t.Fatalf("reapply up migration after development rollback: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.outbox_messages')::text, to_regclass('public.outbox_replay_audit')::text",
	).Scan(&messageTable, &auditTable); err != nil {
		t.Fatalf("inspect reapplied migration: %v", err)
	}
	if messageTable == nil || auditTable == nil {
		t.Fatalf("reapplied migration tables = %v/%v", messageTable, auditTable)
	}
}

func proveCanceledTransitionsAreAtomic(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	if _, err := pool.Exec(ctx, "CREATE TABLE outbox_crash (LIKE outbox_messages INCLUDING ALL)"); err != nil {
		t.Fatalf("create crash transition table: %v", err)
	}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Table: "outbox_crash"})
	if err != nil {
		t.Fatalf("create crash transition writer: %v", err)
	}
	store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
		Table: "outbox_crash",
		ReplayAuthorizer: outboxpostgres.ReplayAuthorizeFunc(
			func(context.Context, outboxpostgres.ReplayRequest) error { return nil },
		),
	})
	if err != nil {
		t.Fatalf("create crash transition store: %v", err)
	}

	now := time.Now().UTC()
	envelopes := make([]outbox.Envelope, 0, 5)
	for index, id := range []string{"cancel-deliver", "cancel-retry", "cancel-dead", "cancel-release", "cancel-extend"} {
		envelopes = append(envelopes, outbox.Envelope{
			ID: id, Topic: "crash", Payload: []byte(`{}`), PayloadVersion: 1,
			AvailableAt: now.Add(-time.Minute), CreatedAt: now.Add(time.Duration(index) * time.Microsecond),
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin crash fixtures: %v", err)
	}
	if err := writer.InsertBatch(ctx, tx, envelopes); err != nil {
		t.Fatalf("insert crash fixtures: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit crash fixtures: %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Claim(canceled, outboxpostgres.ClaimRequest{
		Owner: "crash-relay", Limit: 5, LeaseDuration: time.Minute,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled claim error = %v, want %v", err, context.Canceled)
	}
	assertStateCount(t, ctx, pool, "outbox_crash", "pending", 5)

	claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "crash-relay", Limit: 5, LeaseDuration: time.Minute,
	})
	if err != nil || len(claims) != 5 {
		t.Fatalf("claim crash fixtures = %d/%v", len(claims), err)
	}
	byID := make(map[string]outboxpostgres.Claim, len(claims))
	for _, claim := range claims {
		byID[claim.Envelope.ID] = claim
	}

	extend := byID["cancel-extend"]
	extendRef := outboxpostgres.LeaseRef{ID: extend.Envelope.ID, Token: extend.LeaseToken}
	if _, err := store.ExtendLease(canceled, extendRef, 2*time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled extension error = %v, want %v", err, context.Canceled)
	}
	var unchangedDeadline time.Time
	if err := pool.QueryRow(ctx, "SELECT leased_until FROM outbox_crash WHERE id = $1", extend.Envelope.ID).Scan(&unchangedDeadline); err != nil {
		t.Fatalf("read canceled extension deadline: %v", err)
	}
	if !unchangedDeadline.Equal(extend.LeasedUntil) {
		t.Fatalf("canceled extension changed deadline from %s to %s", extend.LeasedUntil, unchangedDeadline)
	}
	extendedUntil, err := store.ExtendLease(ctx, extendRef, 2*time.Minute)
	if err != nil || !extendedUntil.After(extend.LeasedUntil) {
		t.Fatalf("successful extension deadline/error = %s/%v", extendedUntil, err)
	}

	deliver := byID["cancel-deliver"]
	deliverRef := outboxpostgres.LeaseRef{ID: deliver.Envelope.ID, Token: deliver.LeaseToken}
	if err := store.MarkDelivered(canceled, deliverRef); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delivery error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", deliver.Envelope.ID, "leased")
	if err := store.MarkDelivered(ctx, deliverRef); err != nil {
		t.Fatalf("deliver after canceled transition: %v", err)
	}

	retryClaim := byID["cancel-retry"]
	retryRef := outboxpostgres.LeaseRef{ID: retryClaim.Envelope.ID, Token: retryClaim.LeaseToken}
	if err := store.Retry(canceled, retryRef, time.Minute, errors.New("retry")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", retryClaim.Envelope.ID, "leased")
	if err := store.Retry(ctx, retryRef, time.Minute, errors.New("retry")); err != nil {
		t.Fatalf("retry after canceled transition: %v", err)
	}

	dead := byID["cancel-dead"]
	deadRef := outboxpostgres.LeaseRef{ID: dead.Envelope.ID, Token: dead.LeaseToken}
	if err := store.DeadLetter(canceled, deadRef, errors.New("terminal")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dead letter error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", dead.Envelope.ID, "leased")
	if err := store.DeadLetter(ctx, deadRef, errors.New("terminal")); err != nil {
		t.Fatalf("dead letter after canceled transition: %v", err)
	}

	release := byID["cancel-release"]
	releaseRef := outboxpostgres.LeaseRef{ID: release.Envelope.ID, Token: release.LeaseToken}
	if err := store.ReleaseLease(canceled, releaseRef); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled release error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", release.Envelope.ID, "leased")
	if err := store.ReleaseLease(ctx, releaseRef); err != nil {
		t.Fatalf("release after canceled transition: %v", err)
	}

	replay := outboxpostgres.ReplayRequest{
		IDs: []string{deliver.Envelope.ID}, RequestedBy: "crash-audit",
		Reason: "prove cancellation rollback", AvailableAt: now,
	}
	if _, err := store.Replay(canceled, replay); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replay error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", deliver.Envelope.ID, "delivered")
	var replayAudit int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_replay_audit WHERE message_id = $1", deliver.Envelope.ID).Scan(&replayAudit); err != nil {
		t.Fatalf("read canceled replay audit: %v", err)
	}
	if replayAudit != 0 {
		t.Fatalf("canceled replay wrote %d audit rows", replayAudit)
	}
	if _, err := store.Replay(ctx, replay); err != nil {
		t.Fatalf("replay after canceled transition: %v", err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO outbox_crash (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES (
    'cancel-prune', 'crash', '{}'::bytea, 1, clock_timestamp(),
    clock_timestamp() - interval '2 days', 'delivered',
    clock_timestamp() - interval '2 days'
)`); err != nil {
		t.Fatalf("insert canceled prune fixture: %v", err)
	}
	if _, err := store.PruneDelivered(canceled, now.Add(-24*time.Hour), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled prune error = %v, want %v", err, context.Canceled)
	}
	assertMessageState(t, ctx, pool, "outbox_crash", "cancel-prune", "delivered")
	pruned, err := store.PruneDelivered(ctx, now.Add(-24*time.Hour), 1)
	if err != nil || len(pruned) != 1 || pruned[0] != "cancel-prune" {
		t.Fatalf("prune after canceled transition = %#v/%v", pruned, err)
	}
}

func assertMessageState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	id string,
	want string,
) {
	t.Helper()

	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM "+table+" WHERE id = $1", id).Scan(&state); err != nil {
		t.Fatalf("read %s state: %v", id, err)
	}
	if state != want {
		t.Fatalf("%s state = %q, want %q", id, state, want)
	}
}

func assertStateCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	state string,
	want int,
) {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE state = $1", state).Scan(&count); err != nil {
		t.Fatalf("count %s messages: %v", state, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", state, count, want)
	}
}

type blockingPublisher struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

type failDeliveryStore struct {
	*outboxpostgres.Store
	err    error
	failed bool
	mu     sync.Mutex
}

func (store *failDeliveryStore) MarkDelivered(ctx context.Context, lease outboxpostgres.LeaseRef) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.failed {
		store.failed = true

		return store.err
	}

	return store.Store.MarkDelivered(ctx, lease)
}

type countingPublisher struct {
	count int
	mu    sync.Mutex
}

type ambiguousTimeoutPublisher struct {
	err   error
	count int
	mu    sync.Mutex
}

func (publisher *ambiguousTimeoutPublisher) Publish(context.Context, outbox.Envelope) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.count++
	if publisher.count == 1 {
		return publisher.err
	}

	return nil
}

func (publisher *ambiguousTimeoutPublisher) Count() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	return publisher.count
}

func (publisher *countingPublisher) Publish(context.Context, outbox.Envelope) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.count++

	return nil
}

func (publisher *countingPublisher) Count() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	return publisher.count
}

func (publisher *blockingPublisher) Publish(ctx context.Context, _ outbox.Envelope) error {
	if publisher.started != nil {
		publisher.once.Do(func() { close(publisher.started) })
	}
	select {
	case <-publisher.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func assertClaimedIDs(t *testing.T, claims []outboxpostgres.Claim, want ...string) {
	t.Helper()

	got := make(map[string]struct{}, len(claims))
	for _, claim := range claims {
		got[claim.Envelope.ID] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("claimed IDs = %#v, want %#v", got, want)
	}
	for _, id := range want {
		if _, exists := got[id]; !exists {
			t.Fatalf("claimed IDs = %#v, missing %q", got, id)
		}
	}
}

func assertCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantOrders, wantOutbox int) {
	t.Helper()

	var orders int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&orders); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	var messages int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages").Scan(&messages); err != nil {
		t.Fatalf("count outbox messages: %v", err)
	}
	if orders != wantOrders || messages != wantOutbox {
		t.Fatalf("counts = orders:%d outbox:%d, want orders:%d outbox:%d", orders, messages, wantOrders, wantOutbox)
	}
}

func explainPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) string {
	t.Helper()

	rows, err := pool.Query(ctx, "EXPLAIN "+query)
	if err != nil {
		t.Fatalf("explain query: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain plan: %v", err)
	}

	return plan.String()
}

func usesIndex(plan string) bool {
	return strings.Contains(plan, "Index Scan") ||
		strings.Contains(plan, "Index Only Scan") ||
		strings.Contains(plan, "Bitmap Index Scan")
}
