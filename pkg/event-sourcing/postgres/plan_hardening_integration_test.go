//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	planFixtureStreams         = 128
	planFixtureEventsPerStream = 512
	planReadLimit              = 100
)

type postgreSQLExplain struct {
	Plan postgreSQLPlanNode `json:"Plan"`
}

type postgreSQLPlanNode struct {
	NodeType     string               `json:"Node Type"`
	RelationName string               `json:"Relation Name"`
	IndexName    string               `json:"Index Name"`
	ActualRows   float64              `json:"Actual Rows"`
	ActualLoops  float64              `json:"Actual Loops"`
	Plans        []postgreSQLPlanNode `json:"Plans"`
}

func TestPostgreSQLRealisticReadPlansAvoidTableScans(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	})
	seedPostgreSQLPlanFixture(t, ctx, tx)

	streamPlan := explainPostgreSQLJSON(
		t,
		ctx,
		tx,
		`SELECT global_position, message_id, aggregate_type, aggregate_id,
			stream_version, event_name, event_schema_version, content_type,
			payload, metadata, recorded_at, correlation_id, causation_id,
			tenant, partition_key
		 FROM event_sourcing.messages
		 WHERE aggregate_type = $1 AND aggregate_id = $2
			AND stream_version >= $3
			AND ($4::bigint = 0 OR stream_version <= $4)
			AND $6::boolean
		 ORDER BY stream_version LIMIT $5`,
		"plan.account",
		"stream-64",
		257,
		0,
		planReadLimit,
		true,
	)
	assertPostgreSQLIndexedReadPlan(
		t,
		streamPlan,
		"messages_stream_version_idx",
	)

	globalPlan := explainPostgreSQLJSON(
		t,
		ctx,
		tx,
		`SELECT global_position, message_id, aggregate_type, aggregate_id,
			stream_version, event_name, event_schema_version, content_type,
			payload, metadata, recorded_at, correlation_id, causation_id,
			tenant, partition_key
		 FROM event_sourcing.messages
		 WHERE global_position >= $1
			AND ($2::bigint = 0 OR global_position <= $2)
			AND $4::boolean
		 ORDER BY global_position LIMIT $3`,
		32769,
		0,
		planReadLimit,
		true,
	)
	assertPostgreSQLIndexedReadPlan(t, globalPlan, "messages_pkey")
}

func seedPostgreSQLPlanFixture(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
) {
	t.Helper()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO event_sourcing.streams (
			aggregate_type, aggregate_id, current_version
		 )
		 SELECT 'plan.account', 'stream-' || stream_number::text, $2
		 FROM generate_series(1, $1::integer) AS streams(stream_number)`,
		planFixtureStreams,
		planFixtureEventsPerStream,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO event_sourcing.messages (
			global_position, message_id, aggregate_type, aggregate_id,
			stream_version, event_name, event_schema_version, content_type,
			payload, metadata, recorded_at
		 )
		 SELECT
			((stream_number - 1) * $2::integer + stream_version)::bigint,
			'plan-message-' ||
				((stream_number - 1) * $2::integer + stream_version),
			'plan.account',
			'stream-' || stream_number::text,
			stream_version,
			'plan.recorded',
			1,
			'application/json',
			convert_to('{}', 'UTF8'),
			'{}'::jsonb,
			TIMESTAMPTZ '2026-01-01 00:00:00+00' +
				((stream_number - 1) * $2::integer + stream_version) *
				INTERVAL '1 microsecond'
		 FROM generate_series(1, $1::integer) AS streams(stream_number)
		 CROSS JOIN generate_series(1, $2::integer) AS events(stream_version)`,
		planFixtureStreams,
		planFixtureEventsPerStream,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE event_sourcing.positions
		 SET last_position = $1::bigint * $2::bigint
		 WHERE singleton = true`,
		planFixtureStreams,
		planFixtureEventsPerStream,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE event_sourcing.messages"); err != nil {
		t.Fatal(err)
	}
}

func explainPostgreSQLJSON(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
	query string,
	arguments ...any,
) postgreSQLPlanNode {
	t.Helper()

	var encoded []byte
	if err := tx.QueryRow(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, FORMAT JSON) "+query,
		arguments...,
	).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var explained []postgreSQLExplain
	if err := json.Unmarshal(encoded, &explained); err != nil {
		t.Fatal(err)
	}
	if len(explained) != 1 {
		t.Fatalf("EXPLAIN documents = %d", len(explained))
	}

	return explained[0].Plan
}

func assertPostgreSQLIndexedReadPlan(
	t testing.TB,
	plan postgreSQLPlanNode,
	wantIndex string,
) {
	t.Helper()

	if plan.NodeType != "Limit" || len(plan.Plans) != 1 {
		t.Fatalf("read plan root = %#v", plan)
	}
	scan := plan.Plans[0]
	if scan.NodeType != "Index Scan" ||
		scan.RelationName != "messages" ||
		scan.IndexName != wantIndex ||
		scan.ActualRows != planReadLimit ||
		scan.ActualLoops != 1 {
		t.Fatalf("read plan scan = %#v", scan)
	}
	assertPostgreSQLNoTableScan(t, plan)
}

func assertPostgreSQLNoTableScan(t testing.TB, plan postgreSQLPlanNode) {
	t.Helper()

	if plan.NodeType == "Seq Scan" || plan.NodeType == "Bitmap Heap Scan" {
		t.Fatalf("read plan contains table scan = %#v", plan)
	}
	for _, child := range plan.Plans {
		assertPostgreSQLNoTableScan(t, child)
	}
}
