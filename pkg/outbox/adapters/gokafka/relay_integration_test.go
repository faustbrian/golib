//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	outboxrelay "github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/jackc/pgx/v5/pgxpool"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	integrationPostgresImage = "postgres:18.4-alpine@" +
		"sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
	relayCrashExitCode = 42
	relayCrashLease    = 500 * time.Millisecond
)

func TestRelayProcessDeathAcrossKafkaAckAndOutboxMark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	kafkaContainer, err := tckafka.Run(ctx, integrationKafkaImage)
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := kafkaContainer.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate Kafka: %v", err)
		}
	})
	brokers, err := kafkaContainer.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}

	postgresContainer, err := tcpostgres.Run(
		ctx,
		integrationPostgresImage,
		tcpostgres.WithDatabase("outbox"),
		tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := postgresContainer.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})
	connectionString, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("resolve PostgreSQL connection: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	applyOutboxMigration(t, ctx, pool)

	tests := []struct {
		stage       string
		wantRecords int
		wantClaimed int
	}{
		{stage: "before-kafka-ack", wantRecords: 1, wantClaimed: 1},
		{stage: "after-kafka-ack", wantRecords: 2, wantClaimed: 1},
		{stage: "before-outbox-mark", wantRecords: 2, wantClaimed: 1},
		{stage: "after-outbox-mark", wantRecords: 1, wantClaimed: 0},
	}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatalf("construct outbox writer: %v", err)
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			topic := fmt.Sprintf("relay-crash-%s-%d", test.stage, time.Now().UnixNano())
			createIntegrationTopic(t, ctx, brokers, topic)
			envelope := outbox.Envelope{
				ID: test.stage, Topic: topic, Payload: []byte(`{"stage":"` + test.stage + `"}`),
				PayloadVersion: 1, OrderingKey: "stream-1", IdempotencyKey: test.stage,
				AvailableAt: time.Now().Add(-time.Second).UTC(), CreatedAt: time.Now().UTC(),
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin outbox transaction: %v", err)
			}
			if err := writer.Insert(ctx, tx, envelope); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("insert outbox envelope: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit outbox envelope: %v", err)
			}

			runRelayCrashHelper(t, ctx, connectionString, brokers, topic, test.stage)
			if test.wantClaimed != 0 {
				waitForExpiredCrashLease(t, ctx, pool, envelope.ID)
			}

			producer := newIntegrationProducer(t, brokers, topic, "relay-recovery-"+test.stage)
			publisher, err := gokafka.New(producer)
			if err != nil {
				t.Fatalf("construct recovery publisher: %v", err)
			}
			store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
			if err != nil {
				t.Fatalf("construct recovery store: %v", err)
			}
			recovery, err := outboxrelay.New(store, publisher, relayConfig("recovery-"+test.stage))
			if err != nil {
				t.Fatalf("construct recovery relay: %v", err)
			}
			result, err := recovery.RunOnce(ctx)
			if err != nil {
				t.Fatalf("recover relay: %v", err)
			}
			if result.Claimed != test.wantClaimed || result.Delivered != test.wantClaimed {
				t.Fatalf("recovery result = %#v, want claimed/delivered %d", result, test.wantClaimed)
			}
			if err := producer.Close(); err != nil {
				t.Fatalf("close recovery producer: %v", err)
			}

			var state string
			var delivered bool
			if err := pool.QueryRow(ctx,
				"SELECT state, delivered_at IS NOT NULL FROM outbox_messages WHERE id = $1",
				envelope.ID,
			).Scan(&state, &delivered); err != nil {
				t.Fatalf("read recovered outbox state: %v", err)
			}
			if state != "delivered" || !delivered {
				t.Fatalf("outbox state/delivered = %q/%v, want delivered/true", state, delivered)
			}
			assertCrashRecords(t, ctx, brokers, topic, envelope.ID, test.wantRecords)
		})
	}
}

func TestRelayCrashHelper(t *testing.T) {
	if os.Getenv("GO_OUTBOX_KAFKA_CRASH_HELPER") != "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("GO_OUTBOX_KAFKA_POSTGRES_DSN"))
	if err != nil {
		t.Fatal("connect crash helper to PostgreSQL")
	}
	store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
	if err != nil {
		t.Fatal("construct crash helper store")
	}
	stage := os.Getenv("GO_OUTBOX_KAFKA_CRASH_STAGE")
	topic := os.Getenv("GO_OUTBOX_KAFKA_TOPIC")
	producer, err := kafka.NewProducer(integrationProducerConfig(
		strings.Split(os.Getenv("GO_OUTBOX_KAFKA_BROKERS"), ","),
		topic,
		"relay-crash-helper-"+stage,
	))
	if err != nil {
		t.Fatal("construct crash helper producer")
	}
	publisher, err := gokafka.New(producer)
	if err != nil {
		t.Fatal("construct crash helper publisher")
	}
	crashingStore := &relayCrashStore{Store: store, stage: stage}
	crashingPublisher := relayCrashPublisher{Publisher: publisher, stage: stage}
	doomed, err := outboxrelay.New(
		crashingStore,
		crashingPublisher,
		relayConfig("doomed-"+stage),
	)
	if err != nil {
		t.Fatal("construct crash helper relay")
	}
	if _, err := doomed.RunOnce(ctx); err != nil {
		t.Fatal("run crash helper relay")
	}
	t.Fatal("crash helper returned without process death")
}

type relayCrashPublisher struct {
	Publisher *gokafka.Publisher
	stage     string
}

func (publisher relayCrashPublisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if publisher.stage == "before-kafka-ack" {
		os.Exit(relayCrashExitCode)
	}
	if err := publisher.Publisher.Publish(ctx, envelope); err != nil {
		return err
	}
	if publisher.stage == "after-kafka-ack" {
		os.Exit(relayCrashExitCode)
	}

	return nil
}

type relayCrashStore struct {
	*outboxpostgres.Store
	stage string
}

func (store *relayCrashStore) MarkDelivered(ctx context.Context, lease outboxpostgres.LeaseRef) error {
	if store.stage == "before-outbox-mark" {
		os.Exit(relayCrashExitCode)
	}
	if err := store.Store.MarkDelivered(ctx, lease); err != nil {
		return err
	}
	if store.stage == "after-outbox-mark" {
		os.Exit(relayCrashExitCode)
	}

	return nil
}

func relayConfig(owner string) outboxrelay.Config {
	return outboxrelay.Config{
		Owner: owner, BatchSize: 1, Workers: 1,
		LeaseDuration: relayCrashLease, LeaseRenewalInterval: relayCrashLease / 4,
		MaxAttempts: 3, PollInterval: 10 * time.Millisecond,
		TransitionTimeout: time.Second, ClassifyError: gokafka.ClassifyError,
		Backoff: func(int) time.Duration { return 0 },
	}
}

func integrationProducerConfig(brokers []string, topic, clientID string) kafka.ProducerConfig {
	return kafka.ProducerConfig{
		Brokers: brokers, ClientID: clientID, AllowedTopics: []string{topic},
		Limits: gokafka.DefaultLimits().Kafka, Security: kafka.DevelopmentPlaintextSecurity(),
		RecordRetries: 5, RetryBackoffMin: 25 * time.Millisecond,
		RetryBackoffMax: 100 * time.Millisecond, DeliveryTimeout: 10 * time.Second,
		ShutdownTimeout: 11 * time.Second, RequestTimeout: time.Second,
		DialTimeout: time.Second, Linger: time.Millisecond,
	}
}

func newIntegrationProducer(t *testing.T, brokers []string, topic, clientID string) *kafka.Producer {
	t.Helper()
	producer, err := kafka.NewProducer(integrationProducerConfig(brokers, topic, clientID))
	if err != nil {
		t.Fatalf("construct integration producer: %v", err)
	}

	return producer
}

func applyOutboxMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	contents, err := fs.ReadFile(outboxpostgres.Migrations(), "000001_create_outbox.sql")
	if err != nil {
		t.Fatalf("read outbox migration: %v", err)
	}
	up, _, exists := strings.Cut(string(contents), "-- +migrations Down")
	if !exists {
		t.Fatal("outbox migration lacks down marker")
	}
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply outbox migration: %v", err)
	}
}

func runRelayCrashHelper(
	t *testing.T,
	ctx context.Context,
	connectionString string,
	brokers []string,
	topic string,
	stage string,
) {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRelayCrashHelper$")
	command.Env = append(os.Environ(),
		"GO_OUTBOX_KAFKA_CRASH_HELPER=1",
		"GO_OUTBOX_KAFKA_POSTGRES_DSN="+connectionString,
		"GO_OUTBOX_KAFKA_BROKERS="+strings.Join(brokers, ","),
		"GO_OUTBOX_KAFKA_TOPIC="+topic,
		"GO_OUTBOX_KAFKA_CRASH_STAGE="+stage,
	)
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != relayCrashExitCode {
		t.Fatalf("relay crash helper stage %q exit was not the expected process death", stage)
	}
}

func waitForExpiredCrashLease(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		if err := pool.QueryRow(ctx,
			"SELECT state = 'leased' AND leased_until <= clock_timestamp() FROM outbox_messages WHERE id = $1",
			id,
		).Scan(&expired); err != nil {
			t.Fatalf("read crash lease: %v", err)
		}
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for crash lease: %v", ctx.Err())
		case <-deadline.C:
			t.Fatal("wait for crash lease: timeout")
		case <-ticker.C:
		}
	}
}

func assertCrashRecords(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	eventID string,
	want int,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
	)
	if err != nil {
		t.Fatalf("construct crash record consumer: %v", err)
	}
	defer client.Close()
	records := make([]*kgo.Record, 0, want)
	for len(records) < want {
		fetches := client.PollRecords(ctx, want-len(records))
		if err := fetches.Err(); err != nil {
			t.Fatalf("consume crash records: %v", err)
		}
		fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })
	}
	extraCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	extra := client.PollRecords(extraCtx, 1)
	cancel()
	if extra.NumRecords() != 0 {
		t.Fatalf("Kafka records = more than %d", want)
	}
	for _, record := range records {
		if string(record.Key) != "stream-1" {
			t.Fatalf("Kafka crash record key = %q", record.Key)
		}
		foundEventID := false
		for _, header := range record.Headers {
			if header.Key == "event-id" && string(header.Value) == eventID {
				foundEventID = true
			}
		}
		if !foundEventID {
			t.Fatal("Kafka crash record lacks stable event identity")
		}
	}
}
