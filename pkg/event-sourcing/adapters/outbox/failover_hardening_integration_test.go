//go:build integration

package eventoutbox_test

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPrimaryFailoverRollsBackOpenCallerTransactionAsOnePair(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	clusterNetwork := newGooutboxReplicaNetwork(t, ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := clusterNetwork.Remove(cleanupCtx); err != nil {
			t.Errorf("remove PostgreSQL network: %v", err)
		}
	})

	primary, err := tcpostgres.Run(
		ctx,
		outboxPostgresIntegrationImage(t, "18"),
		network.WithNetwork([]string{"eventoutbox-primary"}, clusterNetwork),
		tcpostgres.WithDatabase("event_sourcing_outbox"),
		tcpostgres.WithUsername("event_sourcing_outbox"),
		tcpostgres.WithPassword("event_sourcing_outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL primary: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := primary.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL primary: %v", err)
		}
	})
	primaryConnection, err := primary.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	primaryPool, err := pgxpool.New(ctx, primaryConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(primaryPool.Close)
	applyMigrations(t, ctx, primaryPool, eventpostgres.Migrations())
	applyMigrations(t, ctx, primaryPool, outboxpostgres.Migrations())
	if _, err := primaryPool.Exec(
		ctx,
		"CREATE ROLE gooutbox_replica WITH REPLICATION LOGIN PASSWORD 'gooutbox_replica'",
	); err != nil {
		t.Fatalf("create replication role: %v", err)
	}
	execGooutboxPostgreSQLCommand(
		t,
		ctx,
		primary,
		"sh",
		"-c",
		`printf '%s\n' 'host replication gooutbox_replica 0.0.0.0/0 scram-sha-256' >> "$PGDATA/pg_hba.conf"`,
	)
	if _, err := primaryPool.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		t.Fatalf("reload primary access rules: %v", err)
	}

	const replicaData = "/tmp/eventoutbox-replica"
	replica, err := testcontainers.Run(
		ctx,
		outboxPostgresIntegrationImage(t, "18"),
		network.WithNetwork([]string{"eventoutbox-replica"}, clusterNetwork),
		testcontainers.WithEnv(map[string]string{"PGDATA": replicaData}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithEntrypoint("sh", "-c"),
		testcontainers.WithCmd(
			`set -eu
mkdir -p "$PGDATA"
rm -rf "$PGDATA"/*
printf '%s\n' 'eventoutbox-primary:5432:*:gooutbox_replica:gooutbox_replica' > /tmp/eventoutbox-replica.pgpass
chmod 600 /tmp/eventoutbox-replica.pgpass
pg_basebackup --dbname='host=eventoutbox-primary port=5432 user=gooutbox_replica passfile=/tmp/eventoutbox-replica.pgpass sslmode=disable' --pgdata="$PGDATA" --format=plain --wal-method=stream --write-recovery-conf
chmod 700 "$PGDATA"
exec postgres -D "$PGDATA"`,
		),
		testcontainers.WithConfigModifier(func(config *dockercontainer.Config) {
			config.User = "postgres"
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept read-only connections").
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL replica: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := replica.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL replica: %v", err)
		}
	})
	replicaHost, err := replica.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replicaPort, err := replica.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	replicaConnection := fmt.Sprintf(
		"postgres://event_sourcing_outbox:event_sourcing_outbox@%s:%s/event_sourcing_outbox?sslmode=disable",
		replicaHost,
		replicaPort.Port(),
	)
	replicaPool, err := pgxpool.New(ctx, replicaConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replicaPool.Close)
	var inRecovery bool
	if err := replicaPool.QueryRow(
		ctx,
		"SELECT pg_is_in_recovery()",
	).Scan(&inRecovery); err != nil {
		t.Fatal(err)
	}
	if !inRecovery {
		t.Fatal("replica started as writable primary")
	}

	stream := atomicityStream(t, "primary-failover")
	pending := atomicityPending(t, stream, "primary-failover-message")
	caller, err := primaryPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newAtomicityStager(t, caller).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatalf("stage before primary failover: %v", err)
	}
	assertFailoverTransactionCounts(t, ctx, caller, 1, 1)

	stopTimeout := 10 * time.Second
	if err := primary.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL primary: %v", err)
	}
	commitCtx, commitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer commitCancel()
	if err := caller.Commit(commitCtx); err == nil {
		t.Fatal("commit through stopped primary succeeded")
	}
	execGooutboxPostgreSQLCommand(
		t,
		ctx,
		replica,
		"pg_ctl",
		"-D",
		replicaData,
		"-w",
		"promote",
	)
	waitForGooutboxPostgreSQLPromotion(t, ctx, replicaPool)
	assertStoredCounts(t, ctx, replicaPool, 0, 0)
	assertAtomicityStreamAbsent(t, ctx, replicaPool, stream)

	retry, err := replicaPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newAtomicityStager(t, retry).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		_ = retry.Rollback(context.Background())
		t.Fatalf("stage after replica promotion: %v", err)
	}
	if err := retry.Commit(ctx); err != nil {
		t.Fatalf("commit after replica promotion: %v", err)
	}
	assertStoredCounts(t, ctx, replicaPool, 1, 1)
	assertAtomicityIdentity(t, ctx, replicaPool, pending.ID().String())
}

func assertFailoverTransactionCounts(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	wantEvents int,
	wantEnvelopes int,
) {
	t.Helper()

	var events, envelopes int
	if err := tx.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages",
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(
		ctx,
		"SELECT count(*) FROM outbox_messages",
	).Scan(&envelopes); err != nil {
		t.Fatal(err)
	}
	if events != wantEvents || envelopes != wantEnvelopes {
		t.Fatalf(
			"transactional counts = (%d, %d), want (%d, %d)",
			events,
			envelopes,
			wantEvents,
			wantEnvelopes,
		)
	}
}

func newGooutboxReplicaNetwork(
	t *testing.T,
	ctx context.Context,
) *testcontainers.DockerNetwork {
	t.Helper()

	const subnetPool = 4_096
	start := int(time.Now().UnixNano() % subnetPool)
	var lastErr error
	for attempt := range subnetPool {
		index := (start + attempt) % subnetPool
		prefix := netip.PrefixFrom(
			netip.AddrFrom4([4]byte{
				10,
				253,
				byte(index / 16),
				byte(index%16) * 16,
			}),
			28,
		)
		dockerNetwork, err := network.New(
			ctx,
			network.WithIPAM(&dockernetwork.IPAM{
				Driver: "default",
				Config: []dockernetwork.IPAMConfig{{Subnet: prefix}},
			}),
		)
		if err == nil {
			return dockerNetwork
		}
		if context.Cause(ctx) != nil ||
			!strings.Contains(err.Error(), "Pool overlaps") {
			t.Fatalf("create PostgreSQL replica network: %v", err)
		}
		lastErr = err
	}

	t.Fatalf(
		"create PostgreSQL replica network from dedicated subnet pool: %v",
		lastErr,
	)

	return nil
}

func waitForGooutboxPostgreSQLPromotion(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var inRecovery bool
		err := pool.QueryRow(
			deadline,
			"SELECT pg_is_in_recovery()",
		).Scan(&inRecovery)
		if err == nil && !inRecovery {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for PostgreSQL promotion: %v: %v",
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func execGooutboxPostgreSQLCommand(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	command ...string,
) {
	t.Helper()

	exitCode, output, err := container.Exec(
		ctx,
		command,
		tcexec.Multiplexed(),
	)
	if err != nil {
		t.Fatalf("execute PostgreSQL utility: %v", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(output, 64*1024))
	if readErr != nil {
		t.Fatalf("read PostgreSQL utility output: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf(
			"PostgreSQL utility exit code %d: %s",
			exitCode,
			strings.TrimSpace(string(contents)),
		)
	}
}
