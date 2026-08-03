//go:build integration && (darwin || linux)

package kafka_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kerr"
)

const (
	apacheKafkaSessionTimeoutChild    = "GOLIB_KAFKA_SESSION_TIMEOUT_CHILD"
	apacheKafkaSessionTimeoutReady    = "golib-kafka-session-timeout-ready"
	apacheKafkaSessionTimeoutHandling = "golib-kafka-session-timeout-handling"
	apacheKafkaSessionTimeoutResult   = "golib-kafka-session-timeout-result"
)

func TestApacheKafkaSessionTimeoutConsumerChild(t *testing.T) {
	if os.Getenv(apacheKafkaSessionTimeoutChild) == "" {
		t.Skip("subprocess helper")
	}

	runApacheKafkaSessionTimeoutConsumerChild(t)
}

func TestApacheKafkaConsumerSessionTimeoutOwnershipLoss(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf(
		"golib-apache-session-timeout-loss-%d",
		time.Now().UnixNano(),
	)
	groupID := topic + "-group"
	createApacheKafkaTopic(t, ctx, brokers, topic, 1)

	child := startApacheKafkaSessionTimeoutConsumerChild(
		t,
		ctx,
		brokers,
		topic,
		groupID,
	)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-session-timeout-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct session-timeout producer: %v", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close session-timeout producer: %v", closeErr)
		}
	}()
	if result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(0),
		Key:       []byte("session-timeout-offset-0"),
		Value:     []byte("session-timeout-offset-0"),
	}); result.Err != nil || result.Partition != 0 || result.Offset != 0 {
		t.Fatalf("publish session-timeout record = %#v", result)
	}

	child.startSessionTimeoutHandling(t)
	if err := child.command.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatalf("suspend session-timeout consumer child: %v", err)
	}
	childStopped := true
	defer func() {
		if childStopped && !child.exited {
			_ = child.command.Process.Signal(syscall.SIGCONT)
		}
	}()

	replacement := newApacheKafkaOwnershipConsumer(
		t,
		brokers,
		topic,
		groupID,
		"golib-apache-session-timeout-replacement",
		"",
		kafka.ObserverPolicy{},
	)
	defer closeApacheKafkaConsumer(t, replacement)
	replacementResult := make(chan apacheKafkaConsumerRecordRun, 1)
	go runApacheKafkaOneRecord(ctx, replacement, replacementResult)
	assertApacheKafkaOneRecordRun(
		t,
		ctx,
		waitForApacheKafkaConsumerRecordRun(ctx, replacementResult),
		0,
		0,
	)

	if _, err := child.stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release session-timeout consumer child: %v", err)
	}
	if err := child.command.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("resume session-timeout consumer child: %v", err)
	}
	childStopped = false
	child.waitForSessionTimeoutResult(t)
	assertApacheKafkaGroupCommits(
		t,
		ctx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{{Topic: topic, Partition: 0}: 1},
	)
}

func runApacheKafkaSessionTimeoutConsumerChild(t *testing.T) {
	t.Helper()

	var outputMu sync.Mutex
	report := func(marker string) error {
		outputMu.Lock()
		defer outputMu.Unlock()

		_, err := fmt.Fprintln(os.Stdout, marker)

		return err
	}
	lost := make(chan kafka.Observation, 1)
	var lostOnce sync.Once
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:               strings.Split(os.Getenv(apacheKafkaConsumerBrokers), ","),
		ClientID:              "golib-apache-session-timeout-original",
		GroupID:               os.Getenv(apacheKafkaConsumerGroup),
		Topics:                []string{os.Getenv(apacheKafkaConsumerTopic)},
		ResetOffset:           kafka.OffsetEarliest,
		BalancePolicy:         kafka.BalanceCooperativeSticky,
		RebalanceHandler:      kafka.RebalanceDrainHandler,
		MaxPollRecords:        1,
		MaxConcurrentHandlers: 1,
		MaxAssignedPartitions: 1,
		FetchMaxWait:          100 * time.Millisecond,
		SessionTimeout:        6 * time.Second,
		HeartbeatInterval:     time.Second,
		RebalanceTimeout:      30 * time.Second,
		HandlerTimeout:        20 * time.Second,
		CommitTimeout:         3 * time.Second,
		ShutdownTimeout:       10 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{func(
				_ context.Context,
				observation kafka.Observation,
			) error {
				if observation.Kind == kafka.ObservationConsumeLost {
					lostOnce.Do(func() { lost <- observation })
				}

				return nil
			}},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("construct session-timeout consumer child: %v", err)
	}
	defer closeApacheKafkaConsumer(t, consumer)

	assignmentCtx, cancelAssignment := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancelAssignment()
	waitForApacheKafkaConsumerPartitions(
		t,
		assignmentCtx,
		consumer,
		os.Getenv(apacheKafkaConsumerTopic),
		1,
	)
	partition := kafka.TopicPartition{
		Topic: os.Getenv(apacheKafkaConsumerTopic), Partition: 0,
	}
	pauseApacheKafkaConsumerPartitions(t, consumer, partition)
	if err := report(apacheKafkaSessionTimeoutReady); err != nil {
		t.Fatalf("report session-timeout child readiness: %v", err)
	}
	var start [1]byte
	if _, err := io.ReadFull(os.Stdin, start[:]); err != nil {
		t.Fatalf("start session-timeout child handling: %v", err)
	}
	resumeApacheKafkaConsumerPartitions(t, consumer, partition)
	childCtx, cancelChild := context.WithTimeout(
		context.Background(),
		90*time.Second,
	)
	defer cancelChild()
	waitForApacheKafkaBufferedConsumerRecords(t, childCtx, consumer, 1)
	result, runErr := consumer.RunOnce(
		childCtx,
		kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedRecord,
		) error {
			if err := report(apacheKafkaSessionTimeoutHandling); err != nil {
				return err
			}
			var release [1]byte
			_, err := io.ReadFull(os.Stdin, release[:])

			return err
		}),
	)
	if result != (kafka.PollResult{Polled: 1, Processed: 1}) ||
		!errors.Is(runErr, kerr.UnknownMemberID) {
		t.Fatalf("session-timeout child result = (%#v, %v)", result, runErr)
	}
	select {
	case observation := <-lost:
		if observation.Succeeded || observation.PartitionCount != 1 ||
			observation.Truncated || observation.Category != kafka.ErrorFenced {
			t.Fatalf("session-timeout loss observation = %#v", observation)
		}
	case <-childCtx.Done():
		t.Fatalf(
			"wait for session-timeout loss observation: %v",
			context.Cause(childCtx),
		)
	}
	assignment, assignmentErr := consumer.Assignment()
	if assignmentErr != nil || !assignment.Lost ||
		len(assignment.Partitions) != 0 {
		t.Fatalf(
			"session-timeout child assignment = %#v, %v",
			assignment,
			assignmentErr,
		)
	}
	if err := report(apacheKafkaSessionTimeoutResult); err != nil {
		t.Fatalf("report session-timeout child result: %v", err)
	}
}

func startApacheKafkaSessionTimeoutConsumerChild(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
) *apacheKafkaProcessorChildProcess {
	t.Helper()

	command := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestApacheKafkaSessionTimeoutConsumerChild$",
	)
	command.Env = append(
		os.Environ(),
		apacheKafkaSessionTimeoutChild+"=1",
		apacheKafkaConsumerBrokers+"="+strings.Join(brokers, ","),
		apacheKafkaConsumerTopic+"="+topic,
		apacheKafkaConsumerGroup+"="+groupID,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open session-timeout consumer child stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		t.Fatalf("open session-timeout consumer child stdout: %v", err)
	}
	child := &apacheKafkaProcessorChildProcess{
		command: command,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
	}
	command.Stderr = &child.stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		t.Fatalf("start session-timeout consumer child: %v", err)
	}
	t.Cleanup(child.stop)
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaSessionTimeoutReady,
		&child.stderr,
	)

	return child
}

func (child *apacheKafkaProcessorChildProcess) startSessionTimeoutHandling(
	t *testing.T,
) {
	t.Helper()

	if _, err := child.stdin.Write([]byte{1}); err != nil {
		t.Fatalf("start session-timeout consumer child handling: %v", err)
	}
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaSessionTimeoutHandling,
		&child.stderr,
	)
}

func (child *apacheKafkaProcessorChildProcess) waitForSessionTimeoutResult(
	t *testing.T,
) {
	t.Helper()

	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaSessionTimeoutResult,
		&child.stderr,
	)
	if err := child.stdin.Close(); err != nil {
		t.Fatalf("close session-timeout consumer child stdin: %v", err)
	}
	if err := child.command.Wait(); err != nil {
		t.Fatalf(
			"wait for session-timeout consumer child: %v: %s",
			err,
			strings.TrimSpace(child.stderr.String()),
		)
	}
	child.exited = true
}
