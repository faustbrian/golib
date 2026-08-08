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
	"github.com/twmb/franz-go/pkg/sasl"
	franzplain "github.com/twmb/franz-go/pkg/sasl/plain"
	franzscram "github.com/twmb/franz-go/pkg/sasl/scram"
	xdgscram "github.com/xdg-go/scram"
)

const (
	benchmarkTLSKafkaImage = "apache/kafka:4.3.1@" +
		"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"
	benchmarkTLSKafkaVersion   = "4.3.1"
	benchmarkTLSOpenSSLVersion = "OpenSSL 3.5.7 "
	benchmarkTLSClientPort     = "9094/tcp"
	benchmarkMTLSClientPort    = "9095/tcp"
	benchmarkSASLClientPort    = "9096/tcp"
	benchmarkTLSInternalPort   = 19092
	benchmarkTLSControllerPort = 29093
	benchmarkTLSClusterID      = "4L6g3nShT-eMCtK--X86sw"
	benchmarkSASLUsername      = "benchmark-user"
	benchmarkSCRAM256Username  = "benchmark-scram-256"
	benchmarkSCRAM512Username  = "benchmark-scram-512"
)

type benchmarkTLSFixture struct {
	container    testcontainers.Container
	endpoint     string
	mtlsEndpoint string
	saslEndpoint string
	config       *tls.Config
	mtlsConfig   *tls.Config
	clientCert   tls.Certificate
	saslPassword string
	scram256Pass string
	scram512Pass string
}

type benchmarkTLSMaterial struct {
	caPEM        []byte
	serverPEM    []byte
	serverKeyPEM []byte
	clientCert   tls.Certificate
	roots        *x509.CertPool
}

type benchmarkAuthenticatedMode uint8

const (
	benchmarkMutualTLS benchmarkAuthenticatedMode = iota + 1
	benchmarkSASLPlain
	benchmarkSCRAMSHA256
	benchmarkSCRAMSHA512
)

func (mode benchmarkAuthenticatedMode) String() string {
	switch mode {
	case benchmarkMutualTLS:
		return "mtls"
	case benchmarkSASLPlain:
		return "sasl-plain"
	case benchmarkSCRAMSHA256:
		return "scram-sha-256"
	case benchmarkSCRAMSHA512:
		return "scram-sha-512"
	default:
		return "unknown"
	}
}

type benchmarkAuthenticatedClientConnection struct {
	mode       benchmarkAuthenticatedMode
	brokers    []string
	tlsConfig  *tls.Config
	clientCert tls.Certificate
	username   string
	password   string
}

type benchmarkAuthenticatedProducerCandidate struct {
	name string
	new  func(
		testing.TB,
		string,
		benchmarkAuthenticatedClientConnection,
	) synchronousProducer
}

type benchmarkXDGSCRAMClient struct {
	hash         xdgscram.HashGeneratorFcn
	conversation *xdgscram.ClientConversation
}

func (client *benchmarkXDGSCRAMClient) Begin(
	username string,
	password string,
	authorizationID string,
) error {
	scramClient, err := client.hash.NewClient(username, password, authorizationID)
	if err != nil {
		return err
	}
	client.conversation = scramClient.NewConversation()

	return nil
}

func (client *benchmarkXDGSCRAMClient) Step(challenge string) (string, error) {
	return client.conversation.Step(challenge)
}

func (client *benchmarkXDGSCRAMClient) Done() bool {
	return client.conversation != nil && client.conversation.Done()
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
	benchmarkAuthenticatedCandidates = []benchmarkAuthenticatedProducerCandidate{
		{name: "golib-policy", new: newPolicyAuthenticatedProducer},
		{name: "raw-franz-go", new: newFranzAuthenticatedProducer},
		{name: "sarama", new: newSaramaAuthenticatedProducer},
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
			benchmark.StopTimer()
			topic := createBenchmarkTLSTopic(benchmark, brokers, tlsConfig)
			benchmark.StartTimer()
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

func BenchmarkEquivalentAuthenticatedSynchronousProduce(benchmark *testing.B) {
	for _, mode := range []benchmarkAuthenticatedMode{
		benchmarkMutualTLS,
		benchmarkSASLPlain,
		benchmarkSCRAMSHA256,
		benchmarkSCRAMSHA512,
	} {
		benchmark.Run(mode.String(), func(benchmark *testing.B) {
			connection := benchmarkAuthenticatedConnection(benchmark, mode)
			for _, size := range []int{128, 1024} {
				benchmark.Run(fmt.Sprintf("%dB", size), func(benchmark *testing.B) {
					value := benchmarkTLSValue(size)
					for _, candidate := range benchmarkAuthenticatedCandidates {
						benchmark.Run(candidate.name, func(benchmark *testing.B) {
							topic := createBenchmarkAuthenticatedTopic(
								benchmark,
								connection,
							)
							producer := candidate.new(benchmark, topic, connection)
							ctx, cancel := context.WithTimeout(
								context.Background(),
								benchmarkDeliveryTimeout,
							)
							err := producer.Produce(
								ctx,
								[]byte("authenticated-warmup-key"),
								value,
							)
							cancel()
							if err != nil {
								_ = producer.Close(context.Background())
								benchmark.Fatalf("warm authenticated producer: %v", err)
							}
							benchmark.SetBytes(int64(len(value)))
							benchmark.ResetTimer()
							for index := range benchmark.N {
								ctx, cancel = context.WithTimeout(
									context.Background(),
									benchmarkDeliveryTimeout,
								)
								err = producer.Produce(
									ctx,
									[]byte(fmt.Sprintf("authenticated-key-%d", index)),
									value,
								)
								cancel()
								if err != nil {
									benchmark.Fatalf("produce authenticated record: %v", err)
								}
							}
							benchmark.StopTimer()
							closeCtx, closeCancel := context.WithTimeout(
								context.Background(),
								benchmarkDeliveryTimeout,
							)
							err = producer.Close(closeCtx)
							closeCancel()
							if err != nil {
								benchmark.Fatalf("close authenticated producer: %v", err)
							}
						})
					}
				})
			}
		})
	}
}

func BenchmarkEquivalentAuthenticatedConnectProduceClose(benchmark *testing.B) {
	value := benchmarkTLSValue(128)
	for _, mode := range []benchmarkAuthenticatedMode{
		benchmarkMutualTLS,
		benchmarkSASLPlain,
		benchmarkSCRAMSHA256,
		benchmarkSCRAMSHA512,
	} {
		benchmark.Run(mode.String(), func(benchmark *testing.B) {
			connection := benchmarkAuthenticatedConnection(benchmark, mode)
			for _, candidate := range benchmarkAuthenticatedCandidates {
				benchmark.Run(candidate.name, func(benchmark *testing.B) {
					benchmark.StopTimer()
					topic := createBenchmarkAuthenticatedTopic(benchmark, connection)
					benchmark.StartTimer()
					for index := range benchmark.N {
						producer := candidate.new(benchmark, topic, connection)
						ctx, cancel := context.WithTimeout(
							context.Background(),
							benchmarkDeliveryTimeout,
						)
						err := producer.Produce(
							ctx,
							[]byte(fmt.Sprintf("authenticated-connect-key-%d", index)),
							value,
						)
						cancel()
						if err != nil {
							benchmark.Fatalf("connect and produce with authentication: %v", err)
						}
						closeCtx, closeCancel := context.WithTimeout(
							context.Background(),
							benchmarkDeliveryTimeout,
						)
						err = producer.Close(closeCtx)
						closeCancel()
						if err != nil {
							benchmark.Fatalf("close authenticated producer: %v", err)
						}
					}
					benchmark.ReportMetric(1, "connections/op")
				})
			}
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

func TestEquivalentAuthenticatedProducerOutcomes(t *testing.T) {
	for _, mode := range []benchmarkAuthenticatedMode{
		benchmarkMutualTLS,
		benchmarkSASLPlain,
		benchmarkSCRAMSHA256,
		benchmarkSCRAMSHA512,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			connection := benchmarkAuthenticatedConnection(t, mode)
			assertBenchmarkTLSVersion(
				t,
				connection.brokers[0],
				connection.tlsConfig,
				tls.VersionTLS13,
			)
			for _, candidate := range benchmarkAuthenticatedCandidates {
				t.Run(candidate.name, func(t *testing.T) {
					topic := createBenchmarkAuthenticatedTopic(t, connection)
					producer := candidate.new(t, topic, connection)
					for index := range 3 {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							benchmarkDeliveryTimeout,
						)
						err := producer.Produce(
							ctx,
							[]byte(fmt.Sprintf("authenticated-key-%d", index)),
							[]byte(fmt.Sprintf("authenticated-value-%d", index)),
						)
						cancel()
						if err != nil {
							_ = producer.Close(context.Background())
							t.Fatalf("produce authenticated record %d: %v", index, err)
						}
					}
					closeCtx, closeCancel := context.WithTimeout(
						context.Background(),
						benchmarkDeliveryTimeout,
					)
					err := producer.Close(closeCtx)
					closeCancel()
					if err != nil {
						t.Fatalf("close authenticated producer: %v", err)
					}
					assertBenchmarkAuthenticatedRecords(
						t,
						connection,
						topic,
						3,
					)
				})
			}
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
			Image: benchmarkTLSKafkaImage,
			User:  "0",
			ExposedPorts: []string{
				benchmarkTLSClientPort,
				benchmarkMTLSClientPort,
				benchmarkSASLClientPort,
			},
			Entrypoint: []string{"sh"},
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
	mtlsEndpoint, err := container.PortEndpoint(ctx, benchmarkMTLSClientPort, "")
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	saslEndpoint, err := container.PortEndpoint(ctx, benchmarkSASLClientPort, "")
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
	saslPassword, err := benchmarkTLSRandomSecret()
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	scram256Password, err := benchmarkTLSRandomSecret()
	if err != nil {
		benchmarkTLSFixtureErr = err

		return
	}
	scram512Password, err := benchmarkTLSRandomSecret()
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
			path: "/tmp/plain.properties",
			data: []byte("password=" + saslPassword + "\n"),
			mode: 0o600,
		},
		{
			path: "/tmp/scram256-password",
			data: []byte(scram256Password),
			mode: 0o600,
		},
		{
			path: "/tmp/scram512-password",
			data: []byte(scram512Password),
			mode: 0o600,
		},
		{
			path: "/tmp/server.properties",
			data: []byte(benchmarkTLSServerProperties(
				endpoint,
				mtlsEndpoint,
				saslEndpoint,
			)),
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
	benchmarkTLSFixtureValue.mtlsEndpoint = mtlsEndpoint
	benchmarkTLSFixtureValue.saslEndpoint = saslEndpoint
	benchmarkTLSFixtureValue.clientCert = material.clientCert
	benchmarkTLSFixtureValue.saslPassword = saslPassword
	benchmarkTLSFixtureValue.scram256Pass = scram256Password
	benchmarkTLSFixtureValue.scram512Pass = scram512Password
	benchmarkTLSFixtureValue.config = &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    material.roots.Clone(),
	}
	benchmarkTLSFixtureValue.mtlsConfig = benchmarkTLSFixtureValue.config.Clone()
	benchmarkTLSFixtureValue.mtlsConfig.Certificates = []tls.Certificate{
		material.clientCert,
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

func benchmarkTLSServerProperties(
	endpoint string,
	mtlsEndpoint string,
	saslEndpoint string,
) string {
	return fmt.Sprintf(
		"process.roles=broker,controller\n"+
			"node.id=1\n"+
			"controller.quorum.voters=1@localhost:%d\n"+
			"controller.listener.names=CONTROLLER\n"+
			"listeners=SSL://:9094,MTLS://:9095,SASL_SSL://:9096,"+
			"INTERNAL://:%d,CONTROLLER://:%d\n"+
			"advertised.listeners=SSL://%s,MTLS://%s,SASL_SSL://%s,"+
			"INTERNAL://localhost:%d\n"+
			"listener.security.protocol.map=SSL:SSL,MTLS:SSL,"+
			"SASL_SSL:SASL_SSL,"+
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
			"ssl.truststore.location=/tmp/ca.pem\n"+
			"ssl.truststore.type=PEM\n"+
			"ssl.client.auth=none\n"+
			"listener.name.mtls.ssl.client.auth=required\n"+
			"sasl.enabled.mechanisms=PLAIN,SCRAM-SHA-256,SCRAM-SHA-512\n"+
			"listener.name.sasl_ssl.plain.sasl.jaas.config="+
			"org.apache.kafka.common.security.plain.PlainLoginModule required "+
			"user_"+benchmarkSASLUsername+"="+
			"\"${file:/tmp/plain.properties:password}\";\n"+
			"listener.name.sasl_ssl.scram-sha-256.sasl.jaas.config="+
			"org.apache.kafka.common.security.scram.ScramLoginModule required;\n"+
			"listener.name.sasl_ssl.scram-sha-512.sasl.jaas.config="+
			"org.apache.kafka.common.security.scram.ScramLoginModule required;\n",
		benchmarkTLSControllerPort,
		benchmarkTLSInternalPort,
		benchmarkTLSControllerPort,
		endpoint,
		mtlsEndpoint,
		saslEndpoint,
		benchmarkTLSInternalPort,
	)
}

func benchmarkTLSStartScript() string {
	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  chown 1000:1000 /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties /tmp/plain.properties " +
		"/tmp/scram256-password /tmp/scram512-password\n" +
		"  chmod 0600 /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties /tmp/plain.properties " +
		"/tmp/scram256-password /tmp/scram512-password\n" +
		"  exec su appuser -s /bin/bash -c " +
		"'exec /bin/bash /tmp/golib-kafka-tls-start.sh'\n" +
		"fi\n" +
		"umask 077\n" +
		"scram256_password=\"$(cat /tmp/scram256-password)\"\n" +
		"scram512_password=\"$(cat /tmp/scram512-password)\"\n" +
		"openssl pkcs12 -export -name broker " +
		"-inkey /tmp/server-key.pem -in /tmp/server.pem " +
		"-certfile /tmp/ca.pem -out /tmp/server.p12 " +
		"-passout file:/tmp/store-password >/dev/null 2>&1\n" +
		"/opt/kafka/bin/kafka-storage.sh format --ignore-formatted " +
		"--cluster-id " + benchmarkTLSClusterID + " " +
		"--config /tmp/server.properties " +
		"--add-scram \"SCRAM-SHA-256=[name=" +
		benchmarkSCRAM256Username + ",password=$scram256_password]\" " +
		"--add-scram \"SCRAM-SHA-512=[name=" +
		benchmarkSCRAM512Username + ",password=$scram512_password]\" " +
		">/dev/null\n" +
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
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return benchmarkTLSMaterial{}, err
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "golib-kafka-benchmark-client",
		},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(
		rand.Reader,
		clientTemplate,
		caTemplate,
		&clientKey.PublicKey,
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
	clientCertificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: clientDER,
		}),
		pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
		}),
	)
	if err != nil {
		return benchmarkTLSMaterial{}, err
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
		clientCert: clientCertificate,
		roots:      roots,
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

func benchmarkAuthenticatedConnection(
	tb testing.TB,
	mode benchmarkAuthenticatedMode,
) benchmarkAuthenticatedClientConnection {
	tb.Helper()
	benchmarkTLSFixtureOnce.Do(startBenchmarkTLSFixture)
	if benchmarkTLSFixtureErr != nil {
		tb.Fatalf("start benchmark authenticated fixture: %v", benchmarkTLSFixtureErr)
	}
	connection := benchmarkAuthenticatedClientConnection{
		mode:       mode,
		tlsConfig:  benchmarkTLSFixtureValue.config.Clone(),
		clientCert: benchmarkTLSFixtureValue.clientCert,
	}
	switch mode {
	case benchmarkMutualTLS:
		connection.brokers = []string{benchmarkTLSFixtureValue.mtlsEndpoint}
		connection.tlsConfig = benchmarkTLSFixtureValue.mtlsConfig.Clone()
	case benchmarkSASLPlain:
		connection.brokers = []string{benchmarkTLSFixtureValue.saslEndpoint}
		connection.username = benchmarkSASLUsername
		connection.password = benchmarkTLSFixtureValue.saslPassword
	case benchmarkSCRAMSHA256:
		connection.brokers = []string{benchmarkTLSFixtureValue.saslEndpoint}
		connection.username = benchmarkSCRAM256Username
		connection.password = benchmarkTLSFixtureValue.scram256Pass
	case benchmarkSCRAMSHA512:
		connection.brokers = []string{benchmarkTLSFixtureValue.saslEndpoint}
		connection.username = benchmarkSCRAM512Username
		connection.password = benchmarkTLSFixtureValue.scram512Pass
	default:
		tb.Fatalf("unsupported benchmark authentication mode %d", mode)
	}
	benchmarkTLSRuntimeReport.Do(func() {
		fmt.Printf(
			"benchmark-tls-broker-runtime=Apache Kafka %s OpenSSL 3.5.7 TLS 1.3\n",
			benchmarkTLSKafkaVersion,
		)
	})

	return connection
}

func createBenchmarkAuthenticatedTopic(
	tb testing.TB,
	connection benchmarkAuthenticatedClientConnection,
) string {
	tb.Helper()
	topic := fmt.Sprintf(
		"golib-%s-client-benchmark-%d",
		connection.mode.String(),
		time.Now().UnixNano(),
	)
	options := []kgo.Opt{
		kgo.SeedBrokers(connection.brokers...),
		kgo.ClientID("golib-authenticated-benchmark-admin"),
		kgo.DialTLSConfig(connection.tlsConfig.Clone()),
		kgo.DialTimeout(benchmarkRequestTimeout),
	}
	options = append(options, benchmarkAuthenticatedFranzSASL(connection)...)
	client, err := kgo.NewClient(options...)
	if err != nil {
		tb.Fatalf("construct authenticated benchmark administrator: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout,
	)
	defer cancel()
	responses, err := kadm.NewClient(client).CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil {
		tb.Fatalf("create authenticated benchmark topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists || response.Err != nil {
		tb.Fatalf("authenticated benchmark topic response = %#v", response)
	}

	return topic
}

func newPolicyAuthenticatedProducer(
	t testing.TB,
	topic string,
	connection benchmarkAuthenticatedClientConnection,
) synchronousProducer {
	t.Helper()
	security := policy.ClientSecurity{TLS: connection.tlsConfig.Clone()}
	if connection.mode == benchmarkMutualTLS {
		security.TLS.Certificates = nil
		security.ClientCertificateProvider = policy.ClientCertificateProviderFunc(
			func(context.Context, policy.ClientCertificateRequest) (
				tls.Certificate,
				error,
			) {
				return connection.clientCert, nil
			},
		)
	} else {
		provider := policy.UsernamePasswordProviderFunc(func(context.Context) (
			policy.UsernamePassword,
			error,
		) {
			return policy.UsernamePassword{
				Username: connection.username,
				Password: []byte(connection.password),
			}, nil
		})
		switch connection.mode {
		case benchmarkSASLPlain:
			security.Authentication = policy.NewPlainAuthentication(provider)
		case benchmarkSCRAMSHA256:
			security.Authentication = policy.NewSCRAMSHA256Authentication(provider)
		case benchmarkSCRAMSHA512:
			security.Authentication = policy.NewSCRAMSHA512Authentication(provider)
		default:
			t.Fatalf("unsupported policy authentication mode %d", connection.mode)
		}
		security.CredentialTimeout = benchmarkRequestTimeout
	}
	producer, err := policy.NewProducer(policy.ProducerConfig{
		Brokers:                slices.Clone(connection.brokers),
		ClientID:               "golib-kafka-authenticated-client-benchmark",
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
		Security:               security,
	})
	if err != nil {
		t.Fatalf("construct policy authenticated producer: %v", err)
	}

	return &policyProducer{producer: producer, topic: topic}
}

func newFranzAuthenticatedProducer(
	t testing.TB,
	topic string,
	connection benchmarkAuthenticatedClientConnection,
) synchronousProducer {
	t.Helper()
	options := []kgo.Opt{
		kgo.SeedBrokers(connection.brokers...),
		kgo.ClientID("raw-franz-go-authenticated-client-benchmark"),
		kgo.RecordPartitioner(kgo.UniformBytesPartitioner(65_536, true, true, nil)),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(1_000),
		kgo.MaxBufferedBytes(64 << 20),
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
		kgo.DialTLSConfig(connection.tlsConfig.Clone()),
	}
	options = append(options, benchmarkAuthenticatedFranzSASL(connection)...)
	client, err := kgo.NewClient(options...)
	if err != nil {
		t.Fatalf("construct raw franz-go authenticated producer: %v", err)
	}

	return &franzProducer{client: client, topic: topic}
}

func benchmarkAuthenticatedFranzSASL(
	connection benchmarkAuthenticatedClientConnection,
) []kgo.Opt {
	var mechanism sasl.Mechanism
	switch connection.mode {
	case benchmarkMutualTLS:
		return nil
	case benchmarkSASLPlain:
		mechanism = franzplain.Auth{
			User: connection.username,
			Pass: connection.password,
		}.AsMechanism()
	case benchmarkSCRAMSHA256:
		mechanism = franzscram.Auth{
			User: connection.username,
			Pass: connection.password,
		}.AsSha256Mechanism()
	case benchmarkSCRAMSHA512:
		mechanism = franzscram.Auth{
			User: connection.username,
			Pass: connection.password,
		}.AsSha512Mechanism()
	default:
		panic(fmt.Sprintf("unsupported franz-go authentication mode %d", connection.mode))
	}

	return []kgo.Opt{kgo.SASL(mechanism)}
}

func newSaramaAuthenticatedProducer(
	t testing.TB,
	topic string,
	connection benchmarkAuthenticatedClientConnection,
) synchronousProducer {
	t.Helper()
	config := newSaramaProducerConfig(true, compressionNone)
	config.Net.TLS.Enable = true
	config.Net.TLS.Config = connection.tlsConfig.Clone()
	switch connection.mode {
	case benchmarkMutualTLS:
	case benchmarkSASLPlain:
		config.Net.SASL.Enable = true
		config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		config.Net.SASL.User = connection.username
		config.Net.SASL.Password = connection.password
	case benchmarkSCRAMSHA256:
		configureSaramaSCRAM(
			config,
			connection,
			sarama.SASLTypeSCRAMSHA256,
			xdgscram.SHA256,
		)
	case benchmarkSCRAMSHA512:
		configureSaramaSCRAM(
			config,
			connection,
			sarama.SASLTypeSCRAMSHA512,
			xdgscram.SHA512,
		)
	default:
		t.Fatalf("unsupported Sarama authentication mode %d", connection.mode)
	}
	producer, err := sarama.NewSyncProducer(connection.brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama authenticated producer: %v", err)
	}

	return &saramaProducer{producer: producer, topic: topic}
}

func configureSaramaSCRAM(
	config *sarama.Config,
	connection benchmarkAuthenticatedClientConnection,
	mechanism sarama.SASLMechanism,
	hash xdgscram.HashGeneratorFcn,
) {
	config.Net.SASL.Enable = true
	config.Net.SASL.Mechanism = mechanism
	config.Net.SASL.User = connection.username
	config.Net.SASL.Password = connection.password
	config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
		return &benchmarkXDGSCRAMClient{hash: hash}
	}
}

func assertBenchmarkAuthenticatedRecords(
	t *testing.T,
	connection benchmarkAuthenticatedClientConnection,
	topic string,
	want int,
) {
	t.Helper()
	options := []kgo.Opt{
		kgo.SeedBrokers(connection.brokers...),
		kgo.ClientID("golib-authenticated-benchmark-verifier"),
		kgo.DialTLSConfig(connection.tlsConfig.Clone()),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchMaxWait(100 * time.Millisecond),
	}
	options = append(options, benchmarkAuthenticatedFranzSASL(connection)...)
	client, err := kgo.NewClient(options...)
	if err != nil {
		t.Fatalf("construct authenticated benchmark verifier: %v", err)
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
			t.Fatalf("read authenticated benchmark records: %v", err)
		}
		records = append(records, fetches.Records()...)
		if ctx.Err() != nil && len(records) < want {
			t.Fatalf("read authenticated benchmark records: %v", ctx.Err())
		}
	}
	if len(records) != want {
		t.Fatalf("authenticated record count = %d, want %d", len(records), want)
	}
	for index, record := range records {
		if record.Offset != int64(index) ||
			string(record.Key) != fmt.Sprintf("authenticated-key-%d", index) ||
			string(record.Value) != fmt.Sprintf("authenticated-value-%d", index) {
			t.Fatalf("authenticated benchmark record %d = %#v", index, record)
		}
	}
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
