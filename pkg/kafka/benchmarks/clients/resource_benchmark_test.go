//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	runtimemetrics "runtime/metrics"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	segmentkafka "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var benchmarkFixtureRestartMu sync.Mutex

const (
	benchmarkReconnectDownTimeout  = 2 * time.Second
	benchmarkFixtureStartupTimeout = 2 * time.Minute
)

type reconnectInspectionMeasurement struct {
	duration       time.Duration
	allocatedBytes uint64
	allocations    uint64
}

type idleResourceMeasurement struct {
	window      time.Duration
	cpu         time.Duration
	heapBytes   int64
	heapObjects int64
	goroutines  int64
	connections int64
	closed      int64
}

type resourceBenchmarkInspector interface {
	benchmarkInspector
	Connections() (int64, error)
}

type resourceInspectionCandidate struct {
	name string
	new  func(testing.TB, []string) resourceBenchmarkInspector
}

var resourceInspectionCandidates = []resourceInspectionCandidate{
	{name: "golib-policy", new: newPolicyResourceInspector},
	{name: "raw-franz-go", new: newFranzResourceInspector},
	{name: "kafka-go", new: newKafkaGoResourceInspector},
	{name: "sarama", new: newSaramaResourceInspector},
}

func BenchmarkEquivalentInspectionReconnect(benchmark *testing.B) {
	fixture := newRestartBenchmarkFixture(benchmark)
	brokers := fixture.brokers
	topic := createBenchmarkTopicWithPartitions(benchmark, brokers, 3)
	for _, candidate := range benchmarkInspectionCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			inspector := candidate.new(benchmark, brokers)
			benchmark.Cleanup(func() {
				if err := inspector.Close(); err != nil {
					benchmark.Errorf("close inspector: %v", err)
				}
			})
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkInspectionOperationTimeout,
			)
			want, err := warmResourceInspection(ctx, inspector, topic)
			cancel()
			if err != nil {
				benchmark.Fatalf("warm inspection: %v", err)
			}
			benchmark.StopTimer()
			var total reconnectInspectionMeasurement
			for range benchmark.N {
				got, measurement, err := reconnectInspection(
					benchmark,
					fixture,
					inspector,
					topic,
					len(want.partitions),
					benchmarkInspectionOperationTimeout,
				)
				if err != nil {
					benchmark.Fatalf("reconnect inspection: %v", err)
				}
				if !equalBenchmarkInspectionTopic(got, want) {
					benchmark.Fatalf(
						"inspection after reconnect = %#v, want %#v",
						got,
						want,
					)
				}
				total.duration += measurement.duration
				total.allocatedBytes += measurement.allocatedBytes
				total.allocations += measurement.allocations
			}
			benchmark.ReportMetric(
				float64(total.duration.Nanoseconds())/float64(benchmark.N),
				"ns/op",
			)
			benchmark.ReportMetric(
				float64(total.allocatedBytes)/float64(benchmark.N),
				"reconnect-bytes/op",
			)
			benchmark.ReportMetric(
				float64(total.allocations)/float64(benchmark.N),
				"reconnect-allocs/op",
			)
			benchmark.ReportMetric(3, "partitions/op")
		})
	}
}

func BenchmarkEquivalentInspectionIdleResources(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	topic := createBenchmarkTopicWithPartitions(benchmark, brokers, 3)
	for _, candidate := range resourceInspectionCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			benchmark.StopTimer()
			var total idleResourceMeasurement
			for range benchmark.N {
				state, measurement, err := measureIdleResources(
					benchmark,
					candidate,
					brokers,
					topic,
					500*time.Millisecond,
				)
				if err != nil {
					benchmark.Fatalf("measure idle resources: %v", err)
				}
				if state.name != topic || len(state.partitions) != 3 {
					benchmark.Fatalf("idle inspection = %#v", state)
				}
				total.window += measurement.window
				total.cpu += measurement.cpu
				total.heapBytes += measurement.heapBytes
				total.heapObjects += measurement.heapObjects
				total.goroutines += measurement.goroutines
				total.connections += measurement.connections
				total.closed += measurement.closed
			}
			operations := float64(benchmark.N)
			benchmark.ReportMetric(
				float64(total.window.Nanoseconds())/operations,
				"ns/op",
			)
			benchmark.ReportMetric(
				float64(total.cpu.Nanoseconds())/
					total.window.Seconds(),
				"idle-cpu-ns/s",
			)
			benchmark.ReportMetric(
				float64(total.heapBytes)/operations,
				"idle-heap-bytes",
			)
			benchmark.ReportMetric(
				float64(total.heapObjects)/operations,
				"idle-heap-objects",
			)
			benchmark.ReportMetric(
				float64(total.goroutines)/operations,
				"idle-goroutines",
			)
			benchmark.ReportMetric(
				float64(total.connections)/operations,
				"idle-connections",
			)
			benchmark.ReportMetric(
				float64(total.closed)/operations,
				"closed-connections/op",
			)
		})
	}
}

func TestEquivalentInspectionReconnectOutcomes(t *testing.T) {
	fixture := newRestartBenchmarkFixture(t)
	brokers := fixture.brokers
	topic := createBenchmarkTopicWithPartitions(t, brokers, 3)
	for _, candidate := range benchmarkInspectionCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			inspector := candidate.new(t, brokers)
			t.Cleanup(func() {
				if err := inspector.Close(); err != nil {
					t.Errorf("close inspector: %v", err)
				}
			})
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkInspectionOperationTimeout,
			)
			before, err := warmResourceInspection(ctx, inspector, topic)
			cancel()
			if err != nil {
				t.Fatalf("warm inspection: %v", err)
			}
			after, measurement, err := reconnectInspection(
				t,
				fixture,
				inspector,
				topic,
				len(before.partitions),
				benchmarkInspectionOperationTimeout,
			)
			if err != nil {
				t.Fatalf("reconnect inspection: %v", err)
			}
			if measurement.duration <= 0 ||
				measurement.allocatedBytes == 0 ||
				measurement.allocations == 0 {
				t.Fatalf("reconnect measurement = %#v", measurement)
			}
			if !equalBenchmarkInspectionTopic(after, before) {
				t.Fatalf("inspection after reconnect = %#v, want %#v", after, before)
			}
		})
	}
}

func TestEquivalentInspectionIdleResourceOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	topic := createBenchmarkTopicWithPartitions(t, brokers, 3)
	var want benchmarkInspectionTopic
	for index, candidate := range resourceInspectionCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			got, measurement, err := measureIdleResources(
				t,
				candidate,
				brokers,
				topic,
				100*time.Millisecond,
			)
			if err != nil {
				t.Fatalf("measure idle resources: %v", err)
			}
			if measurement.window < 100*time.Millisecond ||
				measurement.cpu < 0 ||
				measurement.connections <= 0 ||
				measurement.closed < measurement.connections {
				t.Fatalf("idle resource measurement = %#v", measurement)
			}
			if index == 0 {
				want = got

				return
			}
			if !equalBenchmarkInspectionTopic(got, want) {
				t.Fatalf("idle inspection = %#v, want %#v", got, want)
			}
		})
	}
}

func reconnectInspection(
	t testing.TB,
	fixture *restartBenchmarkFixture,
	inspector benchmarkInspector,
	topic string,
	partitionCount int,
	timeout time.Duration,
) (
	benchmarkInspectionTopic,
	reconnectInspectionMeasurement,
	error,
) {
	t.Helper()
	benchmarkFixtureRestartMu.Lock()
	defer benchmarkFixtureRestartMu.Unlock()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)
	stopTimeout := 5 * time.Second
	err := fixture.container.Stop(stopCtx, &stopTimeout)
	stopCancel()
	if err != nil {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{},
			fmt.Errorf("stop benchmark broker: %w", err)
	}

	downCtx, downCancel := context.WithTimeout(
		context.Background(),
		benchmarkReconnectDownTimeout,
	)
	_, downErr := inspector.Topic(downCtx, topic)
	downCancel()
	if downErr == nil {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{}, errors.New(
			"inspection succeeded while benchmark broker was stopped",
		)
	}

	startCtx, startCancel := context.WithTimeout(
		context.Background(),
		benchmarkFixtureStartupTimeout,
	)
	err = fixture.container.Start(startCtx)
	startCancel()
	if err != nil {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{},
			fmt.Errorf("restart benchmark broker: %w", err)
	}
	brokersCtx, brokersCancel := context.WithTimeout(
		context.Background(),
		benchmarkRequestTimeout,
	)
	restartedBrokers, err := fixture.container.Brokers(brokersCtx)
	brokersCancel()
	if err != nil {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{}, fmt.Errorf(
			"resolve restarted benchmark broker: %w",
			err,
		)
	}
	if !slices.Equal(restartedBrokers, fixture.brokers) {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{}, errors.New(
			"benchmark broker address changed after restart",
		)
	}
	readyCtx, readyCancel := context.WithTimeout(
		context.Background(),
		benchmarkFixtureStartupTimeout,
	)
	err = waitForRestartedBenchmarkBroker(
		readyCtx,
		restartedBrokers,
		topic,
		partitionCount,
	)
	readyCancel()
	if err != nil {
		return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{}, fmt.Errorf(
			"wait for restarted benchmark broker: %w",
			err,
		)
	}

	reconnectCtx, reconnectCancel := context.WithTimeout(
		context.Background(),
		timeout,
	)
	defer reconnectCancel()
	retry := time.NewTicker(benchmarkRetryMin)
	defer retry.Stop()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	startedAt := time.Now()
	var reconnectErr error
	for {
		attemptCtx, attemptCancel := context.WithTimeout(
			reconnectCtx,
			benchmarkRequestTimeout,
		)
		state, err := inspector.Topic(attemptCtx, topic)
		attemptCancel()
		if err == nil {
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			return state, reconnectInspectionMeasurement{
				duration:       time.Since(startedAt),
				allocatedBytes: after.TotalAlloc - before.TotalAlloc,
				allocations:    after.Mallocs - before.Mallocs,
			}, nil
		}
		reconnectErr = err
		select {
		case <-reconnectCtx.Done():
			return benchmarkInspectionTopic{}, reconnectInspectionMeasurement{}, errors.Join(
				reconnectCtx.Err(),
				reconnectErr,
			)
		case <-retry.C:
		}
	}
}

func waitForRestartedBenchmarkBroker(
	ctx context.Context,
	brokers []string,
	topic string,
	partitionCount int,
) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-benchmark-restart-readiness"),
		kgo.DialTimeout(benchmarkRequestTimeout),
		kgo.MetadataMinAge(benchmarkRetryMin),
	)
	if err != nil {
		return fmt.Errorf("construct restart readiness client: %w", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	retry := time.NewTicker(benchmarkRetryMin)
	defer retry.Stop()
	readinessErr := errors.New("restarted benchmark broker is not ready")
	for {
		attemptCtx, attemptCancel := context.WithTimeout(
			ctx,
			benchmarkRequestTimeout,
		)
		details, listErr := admin.ListTopics(attemptCtx, topic)
		attemptCancel()
		if listErr == nil {
			detail, exists := details[topic]
			ready := exists && detail.Err == nil &&
				len(detail.Partitions) == partitionCount
			for _, partition := range detail.Partitions {
				if partition.Err != nil || partition.Leader < 0 {
					ready = false
					break
				}
			}
			if ready {
				return nil
			}
			if exists && detail.Err != nil {
				readinessErr = detail.Err
			} else {
				readinessErr = errors.New(
					"restarted benchmark topic metadata is incomplete",
				)
			}
		} else {
			readinessErr = listErr
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), readinessErr)
		case <-retry.C:
		}
	}
}

func measureIdleResources(
	t testing.TB,
	candidate resourceInspectionCandidate,
	brokers []string,
	topic string,
	window time.Duration,
) (
	benchmarkInspectionTopic,
	idleResourceMeasurement,
	error,
) {
	t.Helper()
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	baselineGoroutines := runtime.NumGoroutine()
	inspector := candidate.new(t, brokers)
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkInspectionOperationTimeout,
	)
	state, err := warmResourceInspection(ctx, inspector, topic)
	cancel()
	if err != nil {
		_ = inspector.Close()

		return benchmarkInspectionTopic{}, idleResourceMeasurement{}, err
	}
	connections, err := inspector.Connections()
	if err != nil {
		_ = inspector.Close()

		return benchmarkInspectionTopic{}, idleResourceMeasurement{}, err
	}
	if connections <= 0 {
		_ = inspector.Close()

		return benchmarkInspectionTopic{}, idleResourceMeasurement{}, errors.New(
			"warmed inspector has no active broker connection",
		)
	}
	runtime.GC()
	cpuBefore := readRuntimeCPU()
	startedAt := time.Now()
	timer := time.NewTimer(window)
	<-timer.C
	elapsed := time.Since(startedAt)
	cpuAfter := readRuntimeCPU()
	runtime.GC()
	var idle runtime.MemStats
	runtime.ReadMemStats(&idle)
	idleGoroutines := runtime.NumGoroutine()
	if err := inspector.Close(); err != nil {
		return benchmarkInspectionTopic{}, idleResourceMeasurement{}, err
	}
	closeDeadline := time.NewTimer(benchmarkRequestTimeout)
	defer closeDeadline.Stop()
	closePoll := time.NewTicker(10 * time.Millisecond)
	defer closePoll.Stop()
	for {
		active, connectionErr := inspector.Connections()
		if connectionErr != nil {
			return benchmarkInspectionTopic{}, idleResourceMeasurement{},
				connectionErr
		}
		if active == 0 {
			break
		}
		select {
		case <-closeDeadline.C:
			return benchmarkInspectionTopic{}, idleResourceMeasurement{},
				errors.New("inspector broker connections did not close")
		case <-closePoll.C:
		}
	}
	return state, idleResourceMeasurement{
		window:      elapsed,
		cpu:         cpuAfter - cpuBefore,
		heapBytes:   int64(idle.HeapAlloc) - int64(baseline.HeapAlloc),
		heapObjects: int64(idle.HeapObjects) - int64(baseline.HeapObjects),
		goroutines:  int64(idleGoroutines - baselineGoroutines),
		connections: connections,
		closed:      connections,
	}, nil
}

func warmResourceInspection(
	ctx context.Context,
	inspector benchmarkInspector,
	topic string,
) (benchmarkInspectionTopic, error) {
	retry := time.NewTicker(benchmarkRetryMin)
	defer retry.Stop()
	var inspectionErr error
	for {
		attemptCtx, attemptCancel := context.WithTimeout(
			ctx,
			benchmarkRequestTimeout,
		)
		state, err := inspector.Topic(attemptCtx, topic)
		attemptCancel()
		if err == nil {
			return state, nil
		}
		inspectionErr = err
		select {
		case <-ctx.Done():
			return benchmarkInspectionTopic{}, errors.Join(
				ctx.Err(),
				inspectionErr,
			)
		case <-retry.C:
		}
	}
}

func readRuntimeCPU() time.Duration {
	samples := []runtimemetrics.Sample{
		{Name: "/cpu/classes/user:cpu-seconds"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/cpu/classes/scavenge/total:cpu-seconds"},
	}
	runtimemetrics.Read(samples)
	busySeconds := samples[0].Value.Float64() +
		samples[1].Value.Float64() +
		samples[2].Value.Float64()

	return time.Duration(busySeconds * float64(time.Second))
}

type benchmarkConnectionCounter struct {
	active atomic.Int64
}

func (counter *benchmarkConnectionCounter) connected() {
	counter.active.Add(1)
}

func (counter *benchmarkConnectionCounter) disconnected() {
	counter.active.Add(-1)
}

func (counter *benchmarkConnectionCounter) Connections() (int64, error) {
	return counter.active.Load(), nil
}

type policyResourceInspector struct {
	*policyBenchmarkInspector
	counter *benchmarkConnectionCounter
}

func newPolicyResourceInspector(
	t testing.TB,
	brokers []string,
) resourceBenchmarkInspector {
	t.Helper()
	counter := &benchmarkConnectionCounter{}
	inspector, err := policy.NewInspector(policy.InspectorConfig{
		Brokers:               brokers,
		ClientID:              "golib-policy-idle-resource-benchmark",
		Security:              policy.DevelopmentPlaintextSecurity(),
		DialTimeout:           benchmarkRequestTimeout,
		RequestTimeout:        benchmarkInspectionOperationTimeout,
		MaxMetadataBrokers:    100,
		MaxMetadataPartitions: 1_000,
		MaxGroupMembers:       1_000,
		Observers: policy.ObserverPolicy{
			Observers: []policy.ObserverFunc{
				func(_ context.Context, observation policy.Observation) error {
					switch observation.Kind {
					case policy.ObservationBrokerConnect:
						if observation.Succeeded {
							counter.connected()
						}
					case policy.ObservationBrokerDisconnect:
						counter.disconnected()
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, policy.ObservationFailure) {},
			Timeout:        100 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("construct policy resource inspector: %v", err)
	}

	return &policyResourceInspector{
		policyBenchmarkInspector: &policyBenchmarkInspector{inspector: inspector},
		counter:                  counter,
	}
}

func (inspector *policyResourceInspector) Connections() (int64, error) {
	return inspector.counter.Connections()
}

type franzResourceInspector struct {
	*franzBenchmarkInspector
	counter *benchmarkConnectionCounter
}

func newFranzResourceInspector(
	t testing.TB,
	brokers []string,
) resourceBenchmarkInspector {
	t.Helper()
	counter := &benchmarkConnectionCounter{}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-idle-resource-benchmark"),
		kgo.DialTimeout(benchmarkRequestTimeout),
		kgo.WithHooks(counter),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go resource inspector: %v", err)
	}

	return &franzResourceInspector{
		franzBenchmarkInspector: &franzBenchmarkInspector{
			client: client,
			admin:  kadm.NewClient(client),
		},
		counter: counter,
	}
}

func (counter *benchmarkConnectionCounter) OnBrokerConnect(
	_ kgo.BrokerMetadata,
	_ time.Duration,
	connection net.Conn,
	err error,
) {
	if err == nil && connection != nil {
		counter.connected()
	}
}

func (counter *benchmarkConnectionCounter) OnBrokerDisconnect(
	kgo.BrokerMetadata,
	net.Conn,
) {
	counter.disconnected()
}

func (inspector *franzResourceInspector) Connections() (int64, error) {
	return inspector.counter.Connections()
}

type trackedBenchmarkConnection struct {
	net.Conn
	counter *benchmarkConnectionCounter
	once    sync.Once
}

func (connection *trackedBenchmarkConnection) Close() error {
	err := connection.Conn.Close()
	connection.once.Do(connection.counter.disconnected)

	return err
}

type kafkaGoResourceInspector struct {
	*kafkaGoBenchmarkInspector
	counter *benchmarkConnectionCounter
}

func newKafkaGoResourceInspector(
	_ testing.TB,
	brokers []string,
) resourceBenchmarkInspector {
	counter := &benchmarkConnectionCounter{}
	address := segmentkafka.TCP(brokers...)
	dialer := &net.Dialer{Timeout: benchmarkRequestTimeout}
	transport := &segmentkafka.Transport{
		ClientID:    "kafka-go-idle-resource-benchmark",
		DialTimeout: benchmarkRequestTimeout,
		MetadataTTL: benchmarkRetryMin,
		Dial: func(
			ctx context.Context,
			network string,
			address string,
		) (net.Conn, error) {
			connection, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			counter.connected()

			return &trackedBenchmarkConnection{
				Conn:    connection,
				counter: counter,
			}, nil
		},
	}

	return &kafkaGoResourceInspector{
		kafkaGoBenchmarkInspector: &kafkaGoBenchmarkInspector{
			address:   address,
			transport: transport,
			client: &segmentkafka.Client{
				Addr:      address,
				Timeout:   benchmarkInspectionOperationTimeout,
				Transport: transport,
			},
		},
		counter: counter,
	}
}

func (inspector *kafkaGoResourceInspector) Connections() (int64, error) {
	return inspector.counter.Connections()
}

type saramaResourceInspector struct {
	*saramaBenchmarkInspector
}

func newSaramaResourceInspector(
	t testing.TB,
	brokers []string,
) resourceBenchmarkInspector {
	t.Helper()
	inspector := newSaramaBenchmarkInspector(t, brokers).(*saramaBenchmarkInspector)

	return &saramaResourceInspector{
		saramaBenchmarkInspector: inspector,
	}
}

func (inspector *saramaResourceInspector) Connections() (int64, error) {
	var active int64
	for _, broker := range inspector.client.Brokers() {
		connected, err := broker.Connected()
		if err != nil {
			return 0, err
		}
		if connected {
			active++
		}
	}
	return active, nil
}

type restartBenchmarkFixture struct {
	container *tckafka.KafkaContainer
	brokers   []string
}

func newRestartBenchmarkFixture(t testing.TB) *restartBenchmarkFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve benchmark broker port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release benchmark broker port: %v", err)
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkFixtureStartupTimeout,
	)
	defer cancel()
	container, err := tckafka.Run(
		ctx,
		benchmarkKafkaImage,
		testcontainers.WithLogger(benchmarkNoopLogger{}),
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) {
			config.PortBindings = dockernetwork.PortMap{
				dockernetwork.MustParsePort("9093/tcp"): {
					{
						HostIP:   netip.MustParseAddr("127.0.0.1"),
						HostPort: fmt.Sprint(port),
					},
				},
			}
		}),
	)
	if err != nil {
		t.Fatalf("start reconnect benchmark fixture: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer closeCancel()
		if err := container.Terminate(closeCtx); err != nil {
			t.Errorf("terminate reconnect benchmark fixture: %v", err)
		}
	})
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve reconnect benchmark brokers: %v", err)
	}
	versionCtx, versionCancel := context.WithTimeout(
		context.Background(),
		benchmarkRequestTimeout,
	)
	exitCode, output, err := container.Exec(
		versionCtx,
		[]string{"kafka-topics", "--version"},
		tcexec.Multiplexed(),
	)
	versionCancel()
	if err != nil || exitCode != 0 {
		t.Fatalf(
			"inspect reconnect benchmark runtime: exit=%d error=%v",
			exitCode,
			err,
		)
	}
	versionBytes, err := io.ReadAll(io.LimitReader(output, 256))
	if err != nil {
		t.Fatalf("read reconnect benchmark runtime: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if err := validateBenchmarkFixtureVersion(version); err != nil {
		t.Fatalf("validate reconnect benchmark runtime: %v", err)
	}
	reportBenchmarkRuntime("Confluent Local " + version)

	return &restartBenchmarkFixture{
		container: container,
		brokers:   brokers,
	}
}
