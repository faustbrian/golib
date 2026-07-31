//go:build integration

package clients_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	benchmarkTLSKafkaImage = "apache/kafka:4.3.1@" +
		"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"
	benchmarkTLSKafkaVersion   = "4.3.1"
	benchmarkTLSOpenSSLVersion = "OpenSSL 3.5.7 "
	benchmarkTLSClientPort     = "9094/tcp"
	benchmarkTLSInternalPort   = 19092
	benchmarkTLSControllerPort = 29093
	benchmarkTLSClusterID      = "4L6g3nShT-eMCtK--X86sw"
)

type benchmarkTLSFixture struct {
	container testcontainers.Container
	endpoint  string
	config    *tls.Config
}

type benchmarkTLSMaterial struct {
	caPEM        []byte
	serverPEM    []byte
	serverKeyPEM []byte
	roots        *x509.CertPool
}

type tlsProducerCandidate struct {
	name string
	new  func(testing.TB, []string, string, *tls.Config) synchronousProducer
}

var (
	benchmarkTLSFixtureOnce   sync.Once
	benchmarkTLSFixtureValue  *benchmarkTLSFixture
	benchmarkTLSFixtureErr    error
	benchmarkTLSRuntimeReport sync.Once
	benchmarkTLSCandidates    = []tlsProducerCandidate{
		{name: "golib-policy", new: newPolicyTLSProducer},
		{name: "raw-franz-go", new: newFranzTLSProducer},
		{name: "sarama", new: newSaramaTLSProducer},
	}
)

func BenchmarkEquivalentTLSSynchronousProduce(benchmark *testing.B) {
	brokers, tlsConfig := benchmarkTLSBrokers(benchmark)
	for _, size := range []int{128, 1024} {
		benchmark.Run(fmt.Sprintf("%dB", size), func(benchmark *testing.B) {
			value := benchmarkTLSValue(size)
			for _, candidate := range benchmarkTLSCandidates {
				benchmark.Run(candidate.name, func(benchmark *testing.B) {
					topic := createBenchmarkTLSTopic(
						benchmark,
						brokers,
						tlsConfig,
					)
					producer := candidate.new(
						benchmark,
						brokers,
						topic,
						tlsConfig,
					)
					ctx, cancel := context.WithTimeout(
						context.Background(),
						benchmarkDeliveryTimeout,
					)
					if err := producer.Produce(
						ctx,
						[]byte("tls-warmup-key"),
						value,
					); err != nil {
						cancel()
						_ = producer.Close(context.Background())
						benchmark.Fatalf("warm TLS producer: %v", err)
					}
					cancel()
					benchmark.SetBytes(int64(len(value)))
					benchmark.ResetTimer()
					for index := range benchmark.N {
						ctx, cancel = context.WithTimeout(
							context.Background(),
							benchmarkDeliveryTimeout,
						)
						err := producer.Produce(
							ctx,
							[]byte(fmt.Sprintf("tls-key-%d", index)),
							value,
						)
						cancel()
						if err != nil {
							benchmark.Fatalf("produce over TLS: %v", err)
						}
					}
					benchmark.StopTimer()
					closeCtx, closeCancel := context.WithTimeout(
						context.Background(),
						benchmarkDeliveryTimeout,
					)
					err := producer.Close(closeCtx)
					closeCancel()
					if err != nil {
						benchmark.Fatalf("close TLS producer: %v", err)
					}
				})
			}
		})
	}
}

func BenchmarkEquivalentTLSConnectProduceClose(benchmark *testing.B) {
	brokers, tlsConfig := benchmarkTLSBrokers(benchmark)
	value := benchmarkTLSValue(128)
	for _, candidate := range benchmarkTLSCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			topic := createBenchmarkTLSTopic(benchmark, brokers, tlsConfig)
			for index := range benchmark.N {
				producer := candidate.new(
					benchmark,
					brokers,
					topic,
					tlsConfig,
				)
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout,
				)
				err := producer.Produce(
					ctx,
					[]byte(fmt.Sprintf("tls-connect-key-%d", index)),
					value,
				)
				cancel()
				if err != nil {
					benchmark.Fatalf("connect and produce over TLS: %v", err)
				}
				closeCtx, closeCancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout,
				)
				err = producer.Close(closeCtx)
				closeCancel()
				if err != nil {
					benchmark.Fatalf("close connected TLS producer: %v", err)
				}
			}
			benchmark.ReportMetric(1, "connections/op")
		})
	}
}

func TestEquivalentTLSProducerOutcomes(t *testing.T) {
	brokers, tlsConfig := benchmarkTLSBrokers(t)
	assertBenchmarkTLSVersion(t, brokers[0], tlsConfig, tls.VersionTLS13)
	for _, candidate := range benchmarkTLSCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			topic := createBenchmarkTLSTopic(t, brokers, tlsConfig)
			producer := candidate.new(t, brokers, topic, tlsConfig)
			for index := range 3 {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout,
				)
				err := producer.Produce(
					ctx,
					[]byte(fmt.Sprintf("tls-key-%d", index)),
					[]byte(fmt.Sprintf("tls-value-%d", index)),
				)
				cancel()
				if err != nil {
					_ = producer.Close(context.Background())
					t.Fatalf("produce TLS record %d: %v", index, err)
				}
			}
			closeCtx, closeCancel := context.WithTimeout(
				context.Background(),
				benchmarkDeliveryTimeout,
			)
			err := producer.Close(closeCtx)
			closeCancel()
			if err != nil {
				t.Fatalf("close TLS producer: %v", err)
			}
			assertBenchmarkTLSRecords(t, brokers, tlsConfig, topic, 3)
		})
	}
}

func benchmarkTLSBrokers(tb testing.TB) ([]string, *tls.Config) {
	tb.Helper()
	benchmarkTLSFixtureOnce.Do(startBenchmarkTLSFixture)
	if benchmarkTLSFixtureErr != nil {
		tb.Fatalf("start benchmark TLS fixture: %v", benchmarkTLSFixtureErr)
	}
	benchmarkTLSRuntimeReport.Do(func() {
		fmt.Printf(
			"benchmark-tls-broker-runtime=Apache Kafka %s OpenSSL 3.5.7 TLS 1.3\n",
			benchmarkTLSKafkaVersion,
		)
	})

	return []string{benchmarkTLSFixtureValue.endpoint},
		benchmarkTLSFixtureValue.config.Clone()
}

func startBenchmarkTLSFixture() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        benchmarkTLSKafkaImage,
			User:         "0",
			ExposedPorts: []string{benchmarkTLSClientPort},
			Entrypoint:   []string{"sh"},
			Cmd: []string{
				"-c",
				"while [ ! -f /tmp/golib-kafka-tls-start.sh ]; do " +
					"sleep 0.05; done; exec /bin/bash " +
					"/tmp/golib-kafka-tls-start.sh",
			},
		},
	}
	container, err := testcontainers.GenericContainer(ctx, request)
	if container != nil {
		benchmarkTLSFixtureValue = &benchmarkTLSFixture{container: container}
	}
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	if err = container.Start(ctx); err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	endpoint, err := container.PortEndpoint(ctx, benchmarkTLSClientPort, "")
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	material, err := newBenchmarkTLSMaterial(host)
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	storePassword, err := benchmarkTLSRandomSecret()
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	files := []struct {
		path string
		data []byte
		mode int64
	}{
		{path: "/tmp/ca.pem", data: material.caPEM, mode: 0o644},
		{path: "/tmp/server.pem", data: material.serverPEM, mode: 0o644},
		{path: "/tmp/server-key.pem", data: material.serverKeyPEM, mode: 0o600},
		{path: "/tmp/store-password", data: []byte(storePassword), mode: 0o600},
		{
			path: "/tmp/store.properties",
			data: []byte("password=" + storePassword + "\n"),
			mode: 0o600,
		},
		{
			path: "/tmp/server.properties",
			data: []byte(benchmarkTLSServerProperties(endpoint)),
			mode: 0o644,
		},
		{
			path: "/tmp/golib-kafka-tls-start.sh",
			data: []byte(benchmarkTLSStartScript()),
			mode: 0o755,
		},
	}
	for _, file := range files {
		if err = container.CopyToContainer(
			ctx,
			file.data,
			file.path,
			file.mode,
		); err != nil {
			benchmarkTLSFixtureErr = fmt.Errorf(
				"copy TLS fixture file %s: %w",
				file.path,
				err,
			)

			return
		}
	}
	if err = wait.ForLog("Transition from STARTING to STARTED").
		WithStartupTimeout(90*time.Second).
		WithPollInterval(100*time.Millisecond).
		WaitUntilReady(ctx, container); err != nil {
		benchmarkTLSFixtureErr = fmt.Errorf(
			"wait for TLS Kafka fixture: %w",
			err,
		)

		return
	}
	if err = validateBenchmarkTLSRuntime(ctx, container); err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	benchmarkTLSFixtureValue.endpoint = endpoint
	benchmarkTLSFixtureValue.config = &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    material.roots.Clone(),
	}
}

func closeBenchmarkTLSFixture() error {
	if benchmarkTLSFixtureValue == nil ||
		benchmarkTLSFixtureValue.container == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return benchmarkTLSFixtureValue.container.Terminate(ctx)
}

func benchmarkTLSServerProperties(endpoint string) string {
	return fmt.Sprintf(
		"process.roles=broker,controller\n"+
			"node.id=1\n"+
			"controller.quorum.voters=1@localhost:%d\n"+
			"controller.listener.names=CONTROLLER\n"+
			"listeners=SSL://:9094,INTERNAL://:%d,CONTROLLER://:%d\n"+
			"advertised.listeners=SSL://%s,INTERNAL://localhost:%d\n"+
			"listener.security.protocol.map=SSL:SSL,"+
			"INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT\n"+
			"inter.broker.listener.name=INTERNAL\n"+
			"log.dirs=/tmp/golib-kafka-tls-data\n"+
			"num.partitions=1\n"+
			"offsets.topic.replication.factor=1\n"+
			"transaction.state.log.replication.factor=1\n"+
			"transaction.state.log.min.isr=1\n"+
			"share.coordinator.state.topic.replication.factor=1\n"+
			"share.coordinator.state.topic.min.isr=1\n"+
			"group.initial.rebalance.delay.ms=0\n"+
			"auto.create.topics.enable=false\n"+
			"config.providers=file\n"+
			"config.providers.file.class="+
			"org.apache.kafka.common.config.provider.FileConfigProvider\n"+
			"ssl.keystore.location=/tmp/server.p12\n"+
			"ssl.keystore.type=PKCS12\n"+
			"ssl.keystore.password=${file:/tmp/store.properties:password}\n"+
			"ssl.key.password=${file:/tmp/store.properties:password}\n"+
			"ssl.enabled.protocols=TLSv1.3\n"+
			"ssl.client.auth=none\n",
		benchmarkTLSControllerPort,
		benchmarkTLSInternalPort,
		benchmarkTLSControllerPort,
		endpoint,
		benchmarkTLSInternalPort,
	)
}

func benchmarkTLSStartScript() string {
	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  chown 1000:1000 /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties\n" +
		"  chmod 0600 /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties\n" +
		"  exec su appuser -s /bin/bash -c " +
		"'exec /bin/bash /tmp/golib-kafka-tls-start.sh'\n" +
		"fi\n" +
		"umask 077\n" +
		"openssl pkcs12 -export -name broker " +
		"-inkey /tmp/server-key.pem -in /tmp/server.pem " +
		"-certfile /tmp/ca.pem -out /tmp/server.p12 " +
		"-passout file:/tmp/store-password >/dev/null 2>&1\n" +
		"/opt/kafka/bin/kafka-storage.sh format --ignore-formatted " +
		"--cluster-id " + benchmarkTLSClusterID + " " +
		"--config /tmp/server.properties >/dev/null\n" +
		"exec /opt/kafka/bin/kafka-server-start.sh " +
		"/tmp/server.properties\n"
}

func newBenchmarkTLSMaterial(host string) (benchmarkTLSMaterial, error) {
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return benchmarkTLSMaterial{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "golib-kafka-benchmark-ca"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(
		rand.Reader,
		caTemplate,
		caTemplate,
		&caKey.PublicKey,
		caKey,
	)
	if err != nil {
		return benchmarkTLSMaterial{}, err
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return benchmarkTLSMaterial{}, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if address := net.ParseIP(host); address != nil {
		serverTemplate.IPAddresses = []net.IP{address}
	} else {
		serverTemplate.DNSNames = []string{host}
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader,
		serverTemplate,
		caTemplate,
		&serverKey.PublicKey,
		caKey,
	)
	if err != nil {
		return benchmarkTLSMaterial{}, err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caDER,
	})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return benchmarkTLSMaterial{}, errors.New("append benchmark TLS CA")
	}

	return benchmarkTLSMaterial{
		caPEM: caPEM,
		serverPEM: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: serverDER,
		}),
		serverKeyPEM: pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(serverKey),
		}),
		roots: roots,
	}, nil
}

func benchmarkTLSRandomSecret() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", value), nil
}

func validateBenchmarkTLSRuntime(
	ctx context.Context,
	container testcontainers.Container,
) error {
	version, err := benchmarkTLSCommandOutput(
		ctx,
		container,
		[]string{"/opt/kafka/bin/kafka-topics.sh", "--version"},
	)
	if err != nil {
		return err
	}
	if version != benchmarkTLSKafkaVersion {
		return fmt.Errorf("TLS Kafka runtime version = %q", version)
	}
	openssl, err := benchmarkTLSCommandOutput(
		ctx,
		container,
		[]string{"openssl", "version"},
	)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(openssl, benchmarkTLSOpenSSLVersion) {
		return fmt.Errorf("TLS OpenSSL runtime version = %q", openssl)
	}

	return nil
}

func benchmarkTLSCommandOutput(
	ctx context.Context,
	container testcontainers.Container,
	command []string,
) (string, error) {
	exitCode, output, err := container.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(output, 256))
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", fmt.Errorf("TLS runtime command exit = %d", exitCode)
	}

	return strings.TrimSpace(string(data)), nil
}

func createBenchmarkTLSTopic(
	tb testing.TB,
	brokers []string,
	tlsConfig *tls.Config,
) string {
	tb.Helper()
	topic := fmt.Sprintf("golib-tls-client-benchmark-%d", time.Now().UnixNano())
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-tls-benchmark-admin"),
		kgo.DialTLSConfig(tlsConfig.Clone()),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		tb.Fatalf("construct TLS benchmark administrator: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout,
	)
	defer cancel()
	responses, err := kadm.NewClient(client).CreateTopics(
		ctx,
		1,
		1,
		nil,
		topic,
	)
	if err != nil {
		tb.Fatalf("create TLS benchmark topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists || response.Err != nil {
		tb.Fatalf("TLS benchmark topic response = %#v", response)
	}

	return topic
}

func newPolicyTLSProducer(
	t testing.TB,
	brokers []string,
	topic string,
	tlsConfig *tls.Config,
) synchronousProducer {
	t.Helper()
	producer, err := policy.NewProducer(policy.ProducerConfig{
		Brokers:                slices.Clone(brokers),
		ClientID:               "golib-kafka-tls-client-benchmark",
		AllowedTopics:          []string{topic},
		KeyPolicy:              policy.KeyRequired,
		MaxBufferedRecords:     1_000,
		MaxBufferedBytes:       64 << 20,
		MaxBatchRecords:        100,
		MaxBatchBytes:          benchmarkBatchBytes,
		RecordRetries:          benchmarkRecordRetries,
		RetryBackoffMin:        benchmarkRetryMin,
		RetryBackoffMax:        benchmarkRetryMax,
		DeliveryTimeout:        benchmarkDeliveryTimeout,
		ShutdownTimeout:        benchmarkDeliveryTimeout + benchmarkRetryMax,
		RequestTimeout:         benchmarkRequestTimeout,
		DialTimeout:            benchmarkRequestTimeout,
		Linger:                 benchmarkLinger,
		CompressionPreferences: []policy.CompressionCodec{policy.CompressionNone},
		Security: policy.ClientSecurity{
			TLS: tlsConfig.Clone(),
		},
	})
	if err != nil {
		t.Fatalf("construct policy TLS producer: %v", err)
	}

	return &policyProducer{producer: producer, topic: topic}
}

func newFranzTLSProducer(
	t testing.TB,
	brokers []string,
	topic string,
	tlsConfig *tls.Config,
) synchronousProducer {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-tls-client-benchmark"),
		kgo.RecordPartitioner(kgo.UniformBytesPartitioner(65_536, true, true, nil)),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(1_000),
		kgo.MaxBufferedBytes(64<<20),
		kgo.ProducerBatchMaxBytes(benchmarkBatchBytes),
		kgo.RecordRetries(benchmarkRecordRetries),
		kgo.RetryBackoffFn(func(int) time.Duration { return benchmarkRetryMin }),
		kgo.MetadataMinAge(benchmarkRetryMin),
		kgo.RecordDeliveryTimeout(benchmarkDeliveryTimeout),
		kgo.ProduceRequestTimeout(benchmarkRequestTimeout),
		kgo.DialTimeout(benchmarkRequestTimeout),
		kgo.ProducerLinger(benchmarkLinger),
		kgo.ProducerBatchCompression(kgo.NoCompression()),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.DialTLSConfig(tlsConfig.Clone()),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go TLS producer: %v", err)
	}

	return &franzProducer{client: client, topic: topic}
}

func newSaramaTLSProducer(
	t testing.TB,
	brokers []string,
	topic string,
	tlsConfig *tls.Config,
) synchronousProducer {
	t.Helper()
	config := newSaramaProducerConfig(true, compressionNone)
	config.Net.TLS.Enable = true
	config.Net.TLS.Config = tlsConfig.Clone()
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama TLS producer: %v", err)
	}

	return &saramaProducer{producer: producer, topic: topic}
}

func assertBenchmarkTLSVersion(
	t *testing.T,
	endpoint string,
	tlsConfig *tls.Config,
	want uint16,
) {
	t.Helper()
	dialer := &net.Dialer{Timeout: benchmarkRequestTimeout}
	connection, err := tls.DialWithDialer(
		dialer,
		"tcp",
		endpoint,
		tlsConfig.Clone(),
	)
	if err != nil {
		t.Fatalf("dial TLS benchmark broker: %v", err)
	}
	defer connection.Close()
	if connection.ConnectionState().Version != want {
		t.Fatalf(
			"TLS benchmark protocol = %x, want %x",
			connection.ConnectionState().Version,
			want,
		)
	}
}

func assertBenchmarkTLSRecords(
	t *testing.T,
	brokers []string,
	tlsConfig *tls.Config,
	topic string,
	want int,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-tls-benchmark-verifier"),
		kgo.DialTLSConfig(tlsConfig.Clone()),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchMaxWait(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct TLS benchmark verifier: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout,
	)
	defer cancel()
	records := make([]*kgo.Record, 0, want)
	for len(records) < want {
		fetches := client.PollRecords(ctx, want-len(records))
		if err := fetches.Err(); err != nil {
			t.Fatalf("read TLS benchmark records: %v", err)
		}
		records = append(records, fetches.Records()...)
		if ctx.Err() != nil && len(records) < want {
			t.Fatalf("read TLS benchmark records: %v", ctx.Err())
		}
	}
	if len(records) != want {
		t.Fatalf("TLS benchmark record count = %d, want %d", len(records), want)
	}
	for index, record := range records {
		if record.Offset != int64(index) ||
			string(record.Key) != fmt.Sprintf("tls-key-%d", index) ||
			string(record.Value) != fmt.Sprintf("tls-value-%d", index) {
			t.Fatalf("TLS benchmark record %d = %#v", index, record)
		}
	}
}

func benchmarkTLSValue(size int) []byte {
	value := make([]byte, size)
	for index := range value {
		value[index] = byte(index % 251)
	}

	return value
}
