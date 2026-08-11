//go:build integration

package postgres

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

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgreSQLReplicaPromotionReconcilesAnInterruptedTransition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	clusterNetwork := newWorkflowReplicaNetwork(t, ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := clusterNetwork.Remove(cleanupCtx); err != nil {
			t.Errorf("remove PostgreSQL network: %v", err)
		}
	})

	primary, err := tcpostgres.Run(
		ctx,
		"postgres:18-alpine",
		network.WithNetwork([]string{"workflow-primary"}, clusterNetwork),
		tcpostgres.WithDatabase("workflow"),
		tcpostgres.WithUsername("workflow"),
		tcpostgres.WithPassword("workflow"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL primary: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if terminateErr := primary.Terminate(cleanupCtx); terminateErr != nil {
			t.Errorf("terminate PostgreSQL primary: %v", terminateErr)
		}
	})
	primaryConnection, err := primary.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("primary connection string: %v", err)
	}
	primaryPool := mustRecoveryPool(t, ctx, primaryConnection)
	defer primaryPool.Close()
	if _, err := primaryPool.Exec(ctx, "CREATE SCHEMA workflow"); err != nil {
		t.Fatalf("create workflow schema: %v", err)
	}
	for _, migration := range SchemaMigrations() {
		if _, err := primaryPool.Exec(ctx, migration.Up); err != nil {
			t.Fatalf("apply workflow migration %d: %v", migration.Version, err)
		}
	}
	primaryStore, err := New(primaryPool, Config{})
	if err != nil {
		t.Fatalf("construct primary store: %v", err)
	}
	baseline := mustCreateTransition(t)
	if err := primaryStore.Commit(ctx, baseline); err != nil {
		t.Fatalf("commit replicated baseline: %v", err)
	}
	if _, err := primaryPool.Exec(
		ctx,
		"CREATE ROLE workflow_replica WITH REPLICATION LOGIN PASSWORD 'workflow_replica'",
	); err != nil {
		t.Fatalf("create replication role: %v", err)
	}
	execWorkflowPostgreSQLCommand(
		t,
		ctx,
		primary,
		"sh",
		"-c",
		`printf '%s\n' 'host replication workflow_replica 0.0.0.0/0 scram-sha-256' >> "$PGDATA/pg_hba.conf"`,
	)
	if _, err := primaryPool.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		t.Fatalf("reload primary access rules: %v", err)
	}

	const replicaData = "/tmp/workflow-replica"
	replica, err := testcontainers.Run(
		ctx,
		"postgres:18-alpine",
		network.WithNetwork([]string{"workflow-replica"}, clusterNetwork),
		testcontainers.WithEnv(map[string]string{"PGDATA": replicaData}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithEntrypoint("sh", "-c"),
		testcontainers.WithCmd(
			`set -eu
mkdir -p "$PGDATA"
rm -rf "$PGDATA"/*
printf '%s\n' 'workflow-primary:5432:*:workflow_replica:workflow_replica' > /tmp/workflow-replica.pgpass
chmod 600 /tmp/workflow-replica.pgpass
pg_basebackup --dbname='host=workflow-primary port=5432 user=workflow_replica passfile=/tmp/workflow-replica.pgpass sslmode=disable' --pgdata="$PGDATA" --format=plain --wal-method=stream --write-recovery-conf
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if terminateErr := replica.Terminate(cleanupCtx); terminateErr != nil {
			t.Errorf("terminate PostgreSQL replica: %v", terminateErr)
		}
	})
	replicaHost, err := replica.Host(ctx)
	if err != nil {
		t.Fatalf("replica host: %v", err)
	}
	replicaPort, err := replica.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("replica port: %v", err)
	}
	replicaConnection := fmt.Sprintf(
		"postgres://workflow:workflow@%s:%s/workflow?sslmode=disable",
		replicaHost,
		replicaPort.Port(),
	)
	replicaPool := mustRecoveryPool(t, ctx, replicaConnection)
	defer replicaPool.Close()
	var inRecovery bool
	if err := replicaPool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		t.Fatalf("inspect replica recovery state: %v", err)
	}
	if !inRecovery {
		t.Fatal("replica started as a writable primary")
	}

	interrupted := mustAttemptTransition(t, baseline.Definition())
	caller, err := primaryPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin interrupted transition: %v", err)
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		_ = caller.Rollback(rollbackCtx)
	}()
	if err := primaryStore.Stage(ctx, caller, interrupted); err != nil {
		t.Fatalf("stage interrupted transition: %v", err)
	}
	stopTimeout := 10 * time.Second
	if err := primary.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL primary: %v", err)
	}
	commitCtx, commitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer commitCancel()
	if err := caller.Commit(commitCtx); err == nil {
		t.Fatal("commit through stopped primary succeeded")
	}

	execWorkflowPostgreSQLCommand(t, ctx, replica, "pg_ctl", "-D", replicaData, "-w", "promote")
	waitForWorkflowPostgreSQLPromotion(t, ctx, replicaPool)
	promotedStore, err := New(replicaPool, Config{})
	if err != nil {
		t.Fatalf("construct promoted store: %v", err)
	}
	if outcome, reconcileErr := promotedStore.ReconcileTransition(
		ctx,
		mustReconciliation(t, baseline),
	); reconcileErr != nil || outcome != workflow.TransitionCommitted {
		t.Fatalf("reconcile replicated baseline = %d, %v", outcome, reconcileErr)
	}
	if outcome, reconcileErr := promotedStore.ReconcileTransition(
		ctx,
		mustReconciliation(t, interrupted),
	); reconcileErr != nil || outcome != workflow.TransitionMissing {
		t.Fatalf("reconcile interrupted transition = %d, %v", outcome, reconcileErr)
	}
	if err := promotedStore.Commit(ctx, interrupted); err != nil {
		t.Fatalf("commit transition after promotion: %v", err)
	}
	assertRecoveryHistory(t, ctx, promotedStore, 3)
}

func newWorkflowReplicaNetwork(t *testing.T, ctx context.Context) *testcontainers.DockerNetwork {
	t.Helper()
	const subnetPool = 4_096
	start := int(time.Now().UnixNano() % subnetPool)
	var lastErr error
	for attempt := range subnetPool {
		index := (start + attempt) % subnetPool
		prefix := netip.PrefixFrom(
			netip.AddrFrom4([4]byte{10, 252, byte(index / 16), byte(index%16) * 16}),
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
		if context.Cause(ctx) != nil || !strings.Contains(err.Error(), "Pool overlaps") {
			t.Fatalf("create PostgreSQL replica network: %v", err)
		}
		lastErr = err
	}
	t.Fatalf("create PostgreSQL replica network from dedicated subnet pool: %v", lastErr)
	return nil
}

func execWorkflowPostgreSQLCommand(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	command ...string,
) {
	t.Helper()
	exitCode, output, err := container.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("execute PostgreSQL command %q: %v", command, err)
	}
	message, readErr := io.ReadAll(output)
	if readErr != nil {
		t.Fatalf("read PostgreSQL command %q: %v", command, readErr)
	}
	if exitCode != 0 {
		t.Fatalf("PostgreSQL command %q exited %d: %s", command, exitCode, strings.TrimSpace(string(message)))
	}
}

func waitForWorkflowPostgreSQLPromotion(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var inRecovery bool
		if err := pool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err == nil && !inRecovery {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for PostgreSQL promotion: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}
