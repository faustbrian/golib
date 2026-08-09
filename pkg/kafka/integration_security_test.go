//go:build integration

package kafka_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	franzoauth "github.com/twmb/franz-go/pkg/sasl/oauth"
	franzplain "github.com/twmb/franz-go/pkg/sasl/plain"
	franzscram "github.com/twmb/franz-go/pkg/sasl/scram"
)

const (
	secureKafkaClientPort       = "9094/tcp"
	secureKafkaInternalPort     = 19092
	secureKafkaControllerPort   = 29093
	secureKafkaDiagnosticBytes  = 32 << 10
	secureKafkaDiagnosticInput  = 1 << 20
	secureKafkaTransientRetries = 8
	secureKafkaIssuer           = "https://issuer.golib.test"
	secureKafkaAudience         = "golib-kafka"
	secureKafkaOAuthKeyID       = "golib-test-key"
)

type secureKafkaMode uint8

const (
	secureKafkaMutualTLS secureKafkaMode = iota + 1
	secureKafkaSASL
	secureKafkaOAuth
)

type secureKafkaBroker struct {
	container     testcontainers.Container
	endpoint      string
	pki           secureKafkaPKI
	storePassword string

	plainPassword    string
	limitedPassword  string
	scram256Password string
	scram512Password string
	oauthKey         *rsa.PrivateKey
}

type secureKafkaBrokerOptions struct {
	oauthJWKSURL      string
	oauthJWKSTrustPEM []byte
	hostAccessPorts   []int
	oauthKey          *rsa.PrivateKey
}

type secureKafkaPKI struct {
	caDER                 []byte
	caPEM                 []byte
	serverDER             []byte
	serverPEM             []byte
	serverKeyPEM          []byte
	clientPEM             []byte
	clientKeyPEM          []byte
	clientIdentity        tls.Certificate
	rotatedClientIdentity tls.Certificate
	roots                 *x509.CertPool
}

func TestApacheKafkaTLSAndMutualTLSCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaMutualTLS)
	broker.assertRuntimeVersions(t, ctx)
	broker.assertTLSVersion(t, tls.VersionTLS12)
	broker.assertTLSVersion(t, tls.VersionTLS13)

	topic := fmt.Sprintf("golib-mtls-%d", time.Now().UnixNano())
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.staticMutualTLSConfig(),
		nil,
		topic,
	)

	var currentCertificate atomic.Pointer[tls.Certificate]
	currentCertificate.Store(&broker.pki.clientIdentity)
	var certificateCalls atomic.Int64
	var rotatedCertificateCalls atomic.Int64
	security := kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		ClientCertificateProvider: kafka.ClientCertificateProviderFunc(func(
			context.Context,
			kafka.ClientCertificateRequest,
		) (tls.Certificate, error) {
			certificateCalls.Add(1)
			certificate := currentCertificate.Load()
			if certificate == &broker.pki.rotatedClientIdentity {
				rotatedCertificateCalls.Add(1)
			}

			return *certificate, nil
		}),
		CredentialTimeout: time.Second,
	}
	publishSecureRecord(t, ctx, broker.endpoint, topic, "mtls", security, kafka.ObserverPolicy{})
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-mtls-group",
		1,
		security,
	)
	if len(values) != 1 || values[0] != "mtls" {
		t.Fatalf("mTLS values = %q", values)
	}
	if certificateCalls.Load() < 2 {
		t.Fatalf("mTLS certificate provider calls = %d, want at least 2", certificateCalls.Load())
	}

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker.endpoint},
		ClientID: "golib-mtls-inspector",
		Security: security,
	})
	if err != nil {
		t.Fatalf("construct mTLS inspector: %v", err)
	}
	if err := inspector.DependencyHealth(ctx); err != nil {
		_ = inspector.Close()
		t.Fatalf("mTLS inspector health: %v", err)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("close mTLS inspector: %v", err)
	}

	rotationTopic := fmt.Sprintf("golib-mtls-rotation-%d", time.Now().UnixNano())
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.staticMutualTLSConfig(),
		nil,
		rotationTopic,
	)
	const rotationClientCount = 3
	type rotatingMTLSClient struct {
		producer             *kafka.Producer
		providerCalls        atomic.Int64
		rotatedProviderCalls atomic.Int64
		baselineCalls        int64
		disconnects          chan struct{}
	}
	rotationClients := make([]*rotatingMTLSClient, 0, rotationClientCount)
	expectedRotationValues := make([]string, 0, rotationClientCount*2)
	for clientIndex := range rotationClientCount {
		client := &rotatingMTLSClient{disconnects: make(chan struct{}, 1)}
		clientSecurity := kafka.ClientSecurity{
			TLS: broker.serverTLSConfig(),
			ClientCertificateProvider: kafka.ClientCertificateProviderFunc(func(
				context.Context,
				kafka.ClientCertificateRequest,
			) (tls.Certificate, error) {
				client.providerCalls.Add(1)
				certificate := currentCertificate.Load()
				if certificate == &broker.pki.rotatedClientIdentity {
					client.rotatedProviderCalls.Add(1)
				}

				return *certificate, nil
			}),
			CredentialTimeout: time.Second,
		}
		producer, producerErr := kafka.NewProducer(kafka.ProducerConfig{
			Brokers:         []string{broker.endpoint},
			ClientID:        fmt.Sprintf("golib-mtls-rotation-producer-%d", clientIndex),
			AllowedTopics:   []string{rotationTopic},
			DeliveryTimeout: 5 * time.Second,
			RequestTimeout:  2 * time.Second,
			ShutdownTimeout: 6 * time.Second,
			Security:        clientSecurity,
			Observers: kafka.ObserverPolicy{
				Timeout: time.Second,
				FailureHandler: func(
					context.Context,
					kafka.ObservationFailure,
				) {
				},
				Observers: []kafka.ObserverFunc{func(
					_ context.Context,
					observation kafka.Observation,
				) error {
					if observation.Kind == kafka.ObservationBrokerDisconnect {
						select {
						case client.disconnects <- struct{}{}:
						default:
						}
					}

					return nil
				}},
			},
		})
		if producerErr != nil {
			t.Fatalf("construct rotating mTLS producer %d: %v", clientIndex, producerErr)
		}
		client.producer = producer
		t.Cleanup(func() { _ = client.producer.Close() })
		value := fmt.Sprintf("client-%d-before-certificate-renewal", clientIndex)
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: rotationTopic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("initial rotating mTLS delivery for client %d: %v", clientIndex, result.Err)
		}
		if client.providerCalls.Load() == 0 {
			t.Fatalf("initial mTLS provider %d was not used", clientIndex)
		}
		select {
		case <-client.disconnects:
			t.Fatalf("mTLS producer %d disconnected before certificate renewal", clientIndex)
		default:
		}
		client.baselineCalls = client.providerCalls.Load()
		rotationClients = append(rotationClients, client)
		expectedRotationValues = append(expectedRotationValues, value)
	}

	currentCertificate.Store(&broker.pki.rotatedClientIdentity)
	waitForSecureKafkaIdleExpiry(t, ctx)

	rotationCtx, cancelRotation := context.WithTimeout(ctx, 20*time.Second)
	defer cancelRotation()
	for clientIndex, client := range rotationClients {
		value := fmt.Sprintf("client-%d-after-certificate-renewal", clientIndex)
		result := client.producer.PublishRecord(rotationCtx, kafka.ProducerRecord{
			Topic: rotationTopic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		disconnected := false
		select {
		case <-client.disconnects:
			disconnected = true
		default:
		}
		if result.Err != nil || !disconnected ||
			client.providerCalls.Load() <= client.baselineCalls ||
			client.rotatedProviderCalls.Load() == 0 {
			t.Fatalf(
				"mTLS certificate renewal result/calls for client %d = %#v/%d/%d",
				clientIndex,
				result,
				client.providerCalls.Load(),
				client.rotatedProviderCalls.Load(),
			)
		}
		expectedRotationValues = append(expectedRotationValues, value)
	}
	for clientIndex, client := range rotationClients {
		if err := client.producer.Close(); err != nil {
			t.Fatalf("close rotating mTLS producer %d: %v", clientIndex, err)
		}
	}
	rotationValues := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		rotationTopic,
		"golib-mtls-rotation-group",
		len(expectedRotationValues),
		security,
	)
	if len(rotationValues) != len(expectedRotationValues) {
		t.Fatalf(
			"mTLS renewal values = %d, want %d",
			len(rotationValues),
			len(expectedRotationValues),
		)
	}
	for index := range expectedRotationValues {
		if rotationValues[index] != expectedRotationValues[index] {
			t.Fatalf(
				"mTLS renewal value %d = %q, want %q",
				index,
				rotationValues[index],
				expectedRotationValues[index],
			)
		}
	}

	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		kafka.ClientSecurity{TLS: broker.serverTLSConfig()},
		nil,
	)
	wrongRoots := broker.staticMutualTLSConfig()
	wrongRoots.RootCAs = x509.NewCertPool()
	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		kafka.ClientSecurity{TLS: wrongRoots},
		nil,
	)
	wrongHostname := broker.staticMutualTLSConfig()
	wrongHostname.ServerName = "not-localhost.invalid"
	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		kafka.ClientSecurity{TLS: wrongHostname},
		nil,
	)

	host, _, err := net.SplitHostPort(broker.endpoint)
	if err != nil {
		t.Fatalf("parse secured Kafka endpoint for certificate rotation: %v", err)
	}
	rotatedPKI := newSecureKafkaPKI(t, host)
	var currentTrustAnchors atomic.Value
	currentTrustAnchors.Store(kafka.TrustAnchors{Certificates: [][]byte{
		append([]byte(nil), broker.pki.caDER...),
	}})
	var trustAnchorCalls atomic.Int64
	trustSecurity := kafka.ClientSecurity{
		TLS:                       &tls.Config{MinVersion: tls.VersionTLS12},
		ClientCertificateProvider: security.ClientCertificateProvider,
		TrustAnchorProvider: kafka.TrustAnchorProviderFunc(func(
			context.Context,
		) (kafka.TrustAnchors, error) {
			trustAnchorCalls.Add(1)
			current := currentTrustAnchors.Load().(kafka.TrustAnchors)
			certificates := make([][]byte, len(current.Certificates))
			for index := range current.Certificates {
				certificates[index] = append([]byte(nil), current.Certificates[index]...)
			}

			return kafka.TrustAnchors{Certificates: certificates}, nil
		}),
		CredentialTimeout: time.Second,
	}
	trustProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{broker.endpoint},
		ClientID:        "golib-server-trust-rotation-producer",
		AllowedTopics:   []string{topic},
		DeliveryTimeout: 5 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 6 * time.Second,
		Security:        trustSecurity,
	})
	if err != nil {
		t.Fatalf("construct trust-anchor rotation producer: %v", err)
	}
	publishTrustRotation := func(value string) {
		t.Helper()
		result := trustProducer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		if result.Err != nil {
			_ = trustProducer.Close()
			t.Fatalf("trust-anchor rotation delivery %q: %v", value, result.Err)
		}
	}
	publishTrustRotation("before-server-certificate-rotation")
	baselineTrustAnchorCalls := trustAnchorCalls.Load()
	if baselineTrustAnchorCalls == 0 {
		_ = trustProducer.Close()
		t.Fatal("initial trust-anchor provider was not used")
	}

	currentTrustAnchors.Store(kafka.TrustAnchors{Certificates: [][]byte{
		append([]byte(nil), broker.pki.caDER...),
		append([]byte(nil), rotatedPKI.caDER...),
	}})
	rotateSecureKafkaServerCertificate(t, ctx, broker, rotatedPKI)
	waitForSecureKafkaServerCertificate(t, ctx, broker, rotatedPKI)
	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		kafka.ClientSecurity{TLS: broker.staticMutualTLSConfig()},
		nil,
	)
	waitForSecureKafkaIdleExpiry(t, ctx)
	publishTrustRotation("after-server-certificate-rotation")
	if trustAnchorCalls.Load() <= baselineTrustAnchorCalls {
		_ = trustProducer.Close()
		t.Fatalf(
			"trust-anchor provider calls after server rotation = %d, want greater than %d",
			trustAnchorCalls.Load(),
			baselineTrustAnchorCalls,
		)
	}

	rotatedTrustAnchorCalls := trustAnchorCalls.Load()
	currentTrustAnchors.Store(kafka.TrustAnchors{Certificates: [][]byte{
		append([]byte(nil), rotatedPKI.caDER...),
	}})
	waitForSecureKafkaIdleExpiry(t, ctx)
	publishTrustRotation("after-retired-trust-anchor-removal")
	if trustAnchorCalls.Load() <= rotatedTrustAnchorCalls {
		_ = trustProducer.Close()
		t.Fatalf(
			"trust-anchor provider calls after retired anchor removal = %d, want greater than %d",
			trustAnchorCalls.Load(),
			rotatedTrustAnchorCalls,
		)
	}
	if err := trustProducer.Close(); err != nil {
		t.Fatalf("close trust-anchor rotation producer: %v", err)
	}
}

func TestApacheKafkaSASLOverTLSCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaSASL)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-sasl-%d", time.Now().UnixNano())
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzplain.Auth{
			User: "plain-user",
			Pass: broker.plainPassword,
		}.AsMechanism(),
		topic,
	)

	tests := []struct {
		name     string
		username string
		password string
		build    func(kafka.UsernamePasswordProvider) kafka.Authentication
	}{
		{
			name: "plain", username: "plain-user",
			password: broker.plainPassword,
			build:    kafka.NewPlainAuthentication,
		},
		{
			name: "scram-sha-256", username: "scram256-user",
			password: broker.scram256Password,
			build:    kafka.NewSCRAMSHA256Authentication,
		},
		{
			name: "scram-sha-512", username: "scram512-user",
			password: broker.scram512Password,
			build:    kafka.NewSCRAMSHA512Authentication,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			security, calls := usernamePasswordSecurity(
				broker,
				test.username,
				test.password,
				test.build,
			)
			var (
				connected          atomic.Bool
				observedAuthMethod atomic.Uint32
			)
			observerPolicy := kafka.ObserverPolicy{
				Timeout: time.Second,
				FailureHandler: func(
					context.Context,
					kafka.ObservationFailure,
				) {
				},
				Observers: []kafka.ObserverFunc{func(
					_ context.Context,
					observation kafka.Observation,
				) error {
					if observation.Kind == kafka.ObservationBrokerConnect &&
						observation.Succeeded {
						observedAuthMethod.Store(uint32(
							observation.AuthenticationMethod,
						))
						connected.Store(true)
					}

					return nil
				}},
			}
			publishSecureRecord(
				t,
				ctx,
				broker.endpoint,
				topic,
				test.name,
				security,
				observerPolicy,
			)
			if calls.Load() == 0 {
				t.Fatalf("%s credential provider was not used", test.name)
			}
			if !connected.Load() ||
				kafka.AuthenticationMethod(observedAuthMethod.Load()) !=
					security.Authentication.Method() {
				t.Fatalf(
					"%s observed authentication = %s, connected=%t",
					test.name,
					kafka.AuthenticationMethod(observedAuthMethod.Load()),
					connected.Load(),
				)
			}

			badSecurity, _ := usernamePasswordSecurity(
				broker,
				test.username,
				"invalid-"+test.password,
				test.build,
			)
			assertSecureKafkaHealthFailure(
				t,
				ctx,
				broker.endpoint,
				badSecurity,
				[]string{test.password},
			)
		})
	}
	if t.Failed() {
		return
	}

	consumerSecurity, consumerCalls := usernamePasswordSecurity(
		broker,
		"scram512-user",
		broker.scram512Password,
		kafka.NewSCRAMSHA512Authentication,
	)
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-sasl-group",
		len(tests),
		consumerSecurity,
	)
	if len(values) != len(tests) {
		t.Fatalf("SASL values = %q", values)
	}
	if consumerCalls.Load() == 0 {
		t.Fatal("SCRAM-SHA-512 consumer credential provider was not used")
	}

	inspectorSecurity, _ := usernamePasswordSecurity(
		broker,
		"scram256-user",
		broker.scram256Password,
		kafka.NewSCRAMSHA256Authentication,
	)
	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker.endpoint},
		ClientID: "golib-sasl-inspector",
		Security: inspectorSecurity,
	})
	if err != nil {
		t.Fatalf("construct SASL inspector: %v", err)
	}
	if err := inspector.DependencyHealth(ctx); err != nil {
		_ = inspector.Close()
		t.Fatalf("SASL inspector health: %v", err)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("close SASL inspector: %v", err)
	}
}

func TestApacheKafkaAuthorizationFailureCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaSASL)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-authorization-%d", time.Now().UnixNano())
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzplain.Auth{
			User: "plain-user",
			Pass: broker.plainPassword,
		}.AsMechanism(),
		topic,
	)

	limitedSecurity, calls := usernamePasswordSecurity(
		broker,
		"limited-user",
		broker.limitedPassword,
		kafka.NewPlainAuthentication,
	)
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker.endpoint},
		ClientID:      "golib-authorization-producer",
		AllowedTopics: []string{topic},
		Security:      limitedSecurity,
	})
	if err != nil {
		t.Fatalf("construct ACL-denied Kafka producer: %v", err)
	}
	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte("denied"),
		Value: []byte("denied"),
	})
	if err := producer.Close(); err != nil {
		t.Fatalf("close ACL-denied Kafka producer: %v", err)
	}
	if calls.Load() == 0 {
		t.Fatal("ACL-denied credential provider was not used")
	}
	var deliveryErr *kafka.DeliveryError
	if !errors.As(result.Err, &deliveryErr) ||
		deliveryErr.Category() != kafka.ErrorAuthorization ||
		!errors.Is(result.Err, kerr.TopicAuthorizationFailed) {
		t.Fatalf("ACL-denied delivery error = %#v", result.Err)
	}
	if strings.Contains(result.Err.Error(), broker.limitedPassword) {
		t.Fatal("ACL-denied delivery error disclosed a credential")
	}

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker.endpoint},
		ClientID: "golib-authorization-inspector",
		Security: limitedSecurity,
	})
	if err != nil {
		t.Fatalf("construct ACL-denied Kafka inspector: %v", err)
	}
	_, inspectionErr := inspector.Topics(ctx, topic)
	if closeErr := inspector.Close(); closeErr != nil {
		t.Fatalf("close ACL-denied Kafka inspector: %v", closeErr)
	}
	if !errors.Is(inspectionErr, kerr.TopicAuthorizationFailed) {
		t.Fatalf("ACL-denied inspection error = %v", inspectionErr)
	}
	if strings.Contains(inspectionErr.Error(), broker.limitedPassword) {
		t.Fatal("ACL-denied inspection error disclosed a credential")
	}

	allowedGroup := fmt.Sprintf("golib-authorization-allowed-%d", time.Now().UnixNano())
	deniedGroup := allowedGroup + "-denied"
	createSecureKafkaGroupDescribeACL(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzplain.Auth{
			User: "plain-user",
			Pass: broker.plainPassword,
		}.AsMechanism(),
		"limited-user",
		allowedGroup,
		topic,
	)
	groupInspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker.endpoint},
		ClientID: "golib-authorization-group-inspector",
		Security: limitedSecurity,
	})
	if err != nil {
		t.Fatalf("construct group ACL Kafka inspector: %v", err)
	}
	groupResults, groupErr := groupInspector.InspectConsumerGroups(
		ctx,
		deniedGroup,
		allowedGroup,
	)
	if closeErr := groupInspector.Close(); closeErr != nil {
		t.Fatalf("close group ACL Kafka inspector: %v", closeErr)
	}
	if !errors.Is(groupErr, kafka.ErrInspectionTargetsFailed) ||
		len(groupResults) != 2 ||
		groupResults[0].Group != deniedGroup ||
		groupResults[0].Category != kafka.ErrorAuthorization ||
		!errors.Is(groupResults[0].Err, kerr.GroupAuthorizationFailed) ||
		groupResults[1].Group != allowedGroup ||
		groupResults[1].Err != nil ||
		groupResults[1].State.State != "Empty" ||
		len(groupResults[1].State.Partitions) != 1 ||
		groupResults[1].State.Partitions[0].Topic != topic ||
		groupResults[1].State.Partitions[0].CommittedOffset != 0 {
		t.Fatalf(
			"group ACL inspection results = %#v, %v; target errors = %v / %v",
			groupResults,
			groupErr,
			groupResults[0].Err,
			groupResults[1].Err,
		)
	}
	if strings.Contains(groupResults[0].Err.Error(), broker.limitedPassword) {
		t.Fatal("group ACL inspection error disclosed a credential")
	}
}

func TestApacheKafkaPlainCredentialReplacementCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaSASL)
	broker.assertRuntimeVersions(t, ctx)
	adminMechanism := franzscram.Auth{
		User: "scram256-user",
		Pass: broker.scram256Password,
	}.AsSha256Mechanism()
	proveApacheKafkaLiveUsernamePasswordRotation(
		t,
		ctx,
		broker,
		"PLAIN",
		"plain-rotation",
		adminMechanism,
		"plain-user",
		broker.plainPassword,
		3,
		1,
		kafka.NewPlainAuthentication,
		func(password string) {
			restartSecureKafkaWithPlainCredential(
				t,
				ctx,
				broker,
				adminMechanism,
				password,
			)
		},
	)
}

func TestApacheKafkaLiveSCRAMCredentialRotationCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaSASL)
	broker.assertRuntimeVersions(t, ctx)
	adminMechanism := franzplain.Auth{
		User: "plain-user",
		Pass: broker.plainPassword,
	}.AsMechanism()
	for _, test := range []struct {
		name                string
		username            string
		initialPassword     string
		mechanism           kadm.ScramMechanism
		buildAuthentication func(kafka.UsernamePasswordProvider) kafka.Authentication
	}{
		{
			name:                "SHA-256",
			username:            "scram256-user",
			initialPassword:     broker.scram256Password,
			mechanism:           kadm.ScramSha256,
			buildAuthentication: kafka.NewSCRAMSHA256Authentication,
		},
		{
			name:                "SHA-512",
			username:            "scram512-user",
			initialPassword:     broker.scram512Password,
			mechanism:           kadm.ScramSha512,
			buildAuthentication: kafka.NewSCRAMSHA512Authentication,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			proveApacheKafkaLiveUsernamePasswordRotation(
				t,
				ctx,
				broker,
				"SCRAM",
				"scram-rotation-"+test.username,
				adminMechanism,
				test.username,
				test.initialPassword,
				3,
				3,
				test.buildAuthentication,
				func(password string) {
					alterSecureKafkaSCRAMCredential(
						t,
						ctx,
						broker,
						adminMechanism,
						test.username,
						password,
						test.mechanism,
					)
				},
			)
		})
	}
}

func proveApacheKafkaLiveUsernamePasswordRotation(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	credentialName string,
	topicPrefix string,
	adminMechanism sasl.Mechanism,
	username string,
	initialPassword string,
	clientCount int,
	rotationCount int,
	buildAuthentication func(kafka.UsernamePasswordProvider) kafka.Authentication,
	replaceCredential func(string),
) {
	t.Helper()

	topic := fmt.Sprintf("golib-%s-%d", topicPrefix, time.Now().UnixNano())
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		adminMechanism,
		topic,
	)

	var currentPassword atomic.Value
	currentPassword.Store(initialPassword)
	type rotatingClient struct {
		producer      *kafka.Producer
		providerCalls *atomic.Int64
	}
	clients := make([]rotatingClient, 0, clientCount)
	expectedValues := make([]string, 0, clientCount*(rotationCount+1))
	for clientIndex := range clientCount {
		providerCalls := &atomic.Int64{}
		security := kafka.ClientSecurity{
			TLS: broker.serverTLSConfig(),
			Authentication: buildAuthentication(
				kafka.UsernamePasswordProviderFunc(func(
					context.Context,
				) (kafka.UsernamePassword, error) {
					providerCalls.Add(1)
					return kafka.UsernamePassword{
						Username: username,
						Password: []byte(currentPassword.Load().(string)),
					}, nil
				}),
			),
			CredentialTimeout: time.Second,
		}
		producer, err := kafka.NewProducer(kafka.ProducerConfig{
			Brokers: []string{broker.endpoint},
			ClientID: fmt.Sprintf(
				"golib-%s-producer-%d",
				topicPrefix,
				clientIndex,
			),
			AllowedTopics:   []string{topic},
			DeliveryTimeout: 3 * time.Second,
			RequestTimeout:  2 * time.Second,
			ShutdownTimeout: 4 * time.Second,
			Security:        security,
		})
		if err != nil {
			t.Fatalf("construct rotating %s producer %d: %v", credentialName, clientIndex, err)
		}
		t.Cleanup(func() { _ = producer.Close() })
		value := fmt.Sprintf("client-%d-before-rotation", clientIndex)
		initial := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		if initial.Err != nil {
			t.Fatalf("initial %s delivery for client %d: %v", credentialName, clientIndex, initial.Err)
		}
		baselineCalls := providerCalls.Load()
		if baselineCalls == 0 {
			t.Fatalf("initial %s provider %d was not used", credentialName, clientIndex)
		}
		clients = append(clients, rotatingClient{
			producer:      producer,
			providerCalls: providerCalls,
		})
		expectedValues = append(expectedValues, value)
	}

	retiredPasswords := make([]string, 0, rotationCount)
	for rotation := 1; rotation <= rotationCount; rotation++ {
		retiredPasswords = append(
			retiredPasswords,
			currentPassword.Load().(string),
		)
		newPassword := randomSecureKafkaCredential(t)
		replaceCredential(newPassword)
		currentPassword.Store(newPassword)

		for clientIndex := range clients {
			client := &clients[clientIndex]
			baselineCalls := client.providerCalls.Load()
			rotationCtx, cancelRotation := context.WithTimeout(ctx, 15*time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			var lastErr error
			for attempt := 1; ; attempt++ {
				select {
				case <-rotationCtx.Done():
					ticker.Stop()
					cancelRotation()
					t.Fatalf(
						"%s provider %d did not refresh during rotation %d after %d calls: %v",
						credentialName,
						clientIndex,
						rotation,
						client.providerCalls.Load(),
						lastErr,
					)
				case <-ticker.C:
				}
				value := fmt.Sprintf(
					"client-%d-rotation-%d-attempt-%d",
					clientIndex,
					rotation,
					attempt,
				)
				result := client.producer.PublishRecord(
					rotationCtx,
					kafka.ProducerRecord{
						Topic: topic,
						Key:   []byte(value),
						Value: []byte(value),
					},
				)
				lastErr = result.Err
				if result.Err == nil {
					expectedValues = append(expectedValues, value)
				}
				if client.providerCalls.Load() > baselineCalls &&
					result.Err == nil {
					break
				}
			}
			ticker.Stop()
			cancelRotation()
		}
	}
	for clientIndex := range clients {
		if err := clients[clientIndex].producer.Close(); err != nil {
			t.Fatalf("close rotating %s producer %d: %v", credentialName, clientIndex, err)
		}
	}

	currentSecurity, _ := usernamePasswordSecurity(
		broker,
		username,
		currentPassword.Load().(string),
		buildAuthentication,
	)
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-"+topicPrefix+"-group",
		len(expectedValues),
		currentSecurity,
	)
	if len(values) != len(expectedValues) {
		t.Fatalf("%s rotation values = %d, want %d", credentialName, len(values), len(expectedValues))
	}
	for index := range expectedValues {
		if values[index] != expectedValues[index] {
			t.Fatalf(
				"%s rotation value %d = %q, want %q",
				credentialName,
				index,
				values[index],
				expectedValues[index],
			)
		}
	}
	for _, retiredPassword := range retiredPasswords {
		retiredSecurity, _ := usernamePasswordSecurity(
			broker,
			username,
			retiredPassword,
			buildAuthentication,
		)
		assertSecureKafkaHealthFailure(
			t,
			ctx,
			broker.endpoint,
			retiredSecurity,
			[]string{retiredPassword},
		)
	}
}

func TestApacheKafkaReplayCompactionGapCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaMutualTLS)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-replay-compaction-%d", time.Now().UnixNano())
	createSecureKafkaTopicWithConfigs(
		t,
		ctx,
		broker.endpoint,
		broker.staticMutualTLSConfig(),
		nil,
		topic,
		map[string]*string{
			"cleanup.policy":            kadm.StringPtr("compact"),
			"segment.bytes":             kadm.StringPtr("1048576"),
			"segment.ms":                kadm.StringPtr("100"),
			"min.cleanable.dirty.ratio": kadm.StringPtr("0.01"),
			"min.compaction.lag.ms":     kadm.StringPtr("0"),
			"max.compaction.lag.ms":     kadm.StringPtr("1000"),
		},
	)

	security := kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		ClientCertificateProvider: kafka.ClientCertificateProviderFunc(func(
			context.Context,
			kafka.ClientCertificateRequest,
		) (tls.Certificate, error) {
			return broker.pki.clientIdentity, nil
		}),
		CredentialTimeout: time.Second,
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                []string{broker.endpoint},
		ClientID:               "golib-replay-compaction-producer",
		AllowedTopics:          []string{topic},
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionNone},
		Security:               security,
	})
	if err != nil {
		t.Fatalf("construct compacted replay producer: %v", err)
	}
	for index := range 8 {
		key := fmt.Sprintf("unique-%d", index)
		if index == 0 || index == 2 {
			key = "replaced-key"
		}
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(key),
			Value: []byte(strings.Repeat(fmt.Sprintf("%d", index), 600<<10)),
		})
		if result.Err != nil || result.Partition != 0 || result.Offset != int64(index) {
			_ = producer.Close()
			t.Fatalf("publish compacted replay record %d: %#v", index, result)
		}
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close compacted replay producer: %v", err)
	}

	firstRetained := waitForSecureKafkaCompaction(
		t,
		ctx,
		broker,
		topic,
		0,
	)
	if firstRetained != 1 {
		t.Fatalf("first retained compacted offset = %d, want 1", firstRetained)
	}

	reader, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  []string{broker.endpoint},
		ClientID: "golib-replay-compaction-reader",
		Ranges: []kafka.ReplayRange{{
			Topic: topic, Partition: 0, StartOffset: 0, EndOffset: 1,
		}},
		SideEffects:     kafka.ReplaySideEffectsAllowed,
		FetchMaxWait:    100 * time.Millisecond,
		ProgressTimeout: 5 * time.Second,
		Security:        security,
	})
	if err != nil {
		t.Fatalf("construct compacted replay reader: %v", err)
	}
	result, replayErr := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		context.Context,
		kafka.ReplayRecord,
	) error {
		t.Fatal("compacted replay invoked handler")

		return nil
	}))
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close compacted replay reader: %v", closeErr)
	}
	if !errors.Is(replayErr, kafka.ErrReplayOffsetGap) ||
		result.Processed != 0 ||
		result.Failed != 1 ||
		result.IncompleteRanges != 1 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].NextOffset != 0 {
		t.Fatalf("compacted replay result/error = %#v/%v", result, replayErr)
	}
}

func TestApacheKafkaSignedJWTOAuthBearerCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaOAuth)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-oauth-%d", time.Now().UnixNano())
	adminToken, _ := broker.issueOAuthToken(t, secureKafkaAudience, 2*time.Minute)
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzoauth.Auth{Token: string(adminToken)}.AsMechanism(),
		topic,
	)

	var providerCalls atomic.Int64
	providerToken, providerExpiry := broker.issueOAuthToken(
		t,
		secureKafkaAudience,
		2*time.Minute,
	)
	provider := kafka.OAuthBearerProviderFunc(func(
		context.Context,
	) (kafka.OAuthBearerToken, error) {
		providerCalls.Add(1)
		return kafka.OAuthBearerToken{
			Token:     append([]byte(nil), providerToken...),
			ExpiresAt: providerExpiry,
		}, nil
	})
	security := kafka.ClientSecurity{
		TLS:               broker.serverTLSConfig(),
		Authentication:    kafka.NewOAuthBearerAuthentication(provider),
		CredentialTimeout: time.Second,
	}
	publishSecureRecord(t, ctx, broker.endpoint, topic, "oauth", security, kafka.ObserverPolicy{})
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-oauth-group",
		1,
		security,
	)
	if len(values) != 1 || values[0] != "oauth" {
		t.Fatalf("OAUTHBEARER values = %q", values)
	}
	if providerCalls.Load() < 2 {
		t.Fatalf("OAUTHBEARER provider calls = %d, want at least 2", providerCalls.Load())
	}

	wrongAudienceToken, wrongAudienceExpiry := broker.issueOAuthToken(
		t,
		"not-golib-kafka",
		2*time.Minute,
	)
	wrongAudience := kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		Authentication: kafka.NewOAuthBearerAuthentication(
			kafka.OAuthBearerProviderFunc(func(
				context.Context,
			) (kafka.OAuthBearerToken, error) {
				return kafka.OAuthBearerToken{
					Token:     wrongAudienceToken,
					ExpiresAt: wrongAudienceExpiry,
				}, nil
			}),
		),
		CredentialTimeout: time.Second,
	}
	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		wrongAudience,
		[]string{string(wrongAudienceToken)},
	)

	wrongIssuerToken, wrongIssuerExpiry := broker.issueOAuthTokenForIssuer(
		t,
		"https://not-issuer.golib.test",
		secureKafkaAudience,
		2*time.Minute,
	)
	wrongIssuer := kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		Authentication: kafka.NewOAuthBearerAuthentication(
			kafka.OAuthBearerProviderFunc(func(
				context.Context,
			) (kafka.OAuthBearerToken, error) {
				return kafka.OAuthBearerToken{
					Token:     wrongIssuerToken,
					ExpiresAt: wrongIssuerExpiry,
				}, nil
			}),
		),
		CredentialTimeout: time.Second,
	}
	assertSecureKafkaHealthFailure(
		t,
		ctx,
		broker.endpoint,
		wrongIssuer,
		[]string{string(wrongIssuerToken)},
	)
}

func startSecureKafkaBroker(
	t *testing.T,
	ctx context.Context,
	mode secureKafkaMode,
) *secureKafkaBroker {
	t.Helper()

	return startSecureKafkaBrokerWithOptions(
		t,
		ctx,
		mode,
		secureKafkaBrokerOptions{},
	)
}

func startSecureKafkaBrokerWithOptions(
	t *testing.T,
	ctx context.Context,
	mode secureKafkaMode,
	options secureKafkaBrokerOptions,
) *secureKafkaBroker {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve secured Apache Kafka port: %v", err)
	}
	hostPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release secured Apache Kafka port: %v", err)
	}
	request := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        apacheKafkaImage,
			User:         "0",
			ExposedPorts: []string{secureKafkaClientPort},
			HostAccessPorts: append(
				[]int(nil),
				options.hostAccessPorts...,
			),
			Entrypoint: []string{"sh"},
			Cmd: []string{
				"-c",
				"while [ ! -f /tmp/golib-kafka-secure-ready ]; do " +
					"sleep 0.05; done; exec /bin/bash " +
					"/tmp/golib-kafka-secure-start.sh",
			},
			HostConfigModifier: func(config *container.HostConfig) {
				config.PortBindings = dockernetwork.PortMap{
					dockernetwork.MustParsePort(secureKafkaClientPort): {
						{
							HostIP:   netip.MustParseAddr("127.0.0.1"),
							HostPort: fmt.Sprint(hostPort),
						},
					},
				}
			},
		},
	}
	container, err := testcontainers.GenericContainer(ctx, request)
	if container != nil {
		cleanupKafkaContainer(t, container)
	}
	if err != nil {
		t.Fatalf("create secured Apache Kafka broker: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("start secured Apache Kafka container: %v", err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		stateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		state, stateErr := container.State(stateCtx)
		if stateErr != nil {
			t.Logf("inspect failed secured Apache Kafka broker: %v", stateErr)
			return
		}
		t.Logf(
			"failed secured Apache Kafka broker running=%t status=%s exit=%d",
			state.Running,
			state.Status,
			state.ExitCode,
		)
	})

	endpoint := waitForSecureKafkaPortEndpoint(t, ctx, container)
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("parse secured Apache Kafka endpoint: %v", err)
	}
	pki := newSecureKafkaPKI(t, host)
	broker := &secureKafkaBroker{
		container:        container,
		endpoint:         endpoint,
		pki:              pki,
		plainPassword:    randomSecureKafkaCredential(t),
		limitedPassword:  randomSecureKafkaCredential(t),
		scram256Password: randomSecureKafkaCredential(t),
		scram512Password: randomSecureKafkaCredential(t),
	}
	storePassword := randomSecureKafkaCredential(t)
	broker.storePassword = storePassword
	copySecureKafkaFile(t, ctx, container, "/tmp/ca.pem", pki.caPEM, 0o644)
	copySecureKafkaFile(t, ctx, container, "/tmp/server.pem", pki.serverPEM, 0o644)
	copySecureKafkaFile(t, ctx, container, "/tmp/server-key.pem", pki.serverKeyPEM, 0o600)
	copySecureKafkaFile(
		t,
		ctx,
		container,
		"/tmp/store-password",
		[]byte(storePassword),
		0o600,
	)
	copySecureKafkaFile(
		t,
		ctx,
		container,
		"/tmp/store.properties",
		[]byte("password="+storePassword+"\n"),
		0o600,
	)

	switch mode {
	case secureKafkaMutualTLS:
	case secureKafkaSASL:
		copySecureKafkaFile(
			t,
			ctx,
			container,
			"/tmp/plain.properties",
			[]byte(
				"password="+broker.plainPassword+"\n"+
					"limited-password="+broker.limitedPassword+"\n",
			),
			0o600,
		)
		copySecureKafkaFile(
			t,
			ctx,
			container,
			"/tmp/scram256-password",
			[]byte(broker.scram256Password),
			0o600,
		)
		copySecureKafkaFile(
			t,
			ctx,
			container,
			"/tmp/scram512-password",
			[]byte(broker.scram512Password),
			0o600,
		)
	case secureKafkaOAuth:
		broker.oauthKey = options.oauthKey
		if broker.oauthKey == nil {
			broker.oauthKey = newSecureKafkaRSAKey(t)
		}
		if options.oauthJWKSURL == "" {
			copySecureKafkaFile(
				t,
				ctx,
				container,
				"/tmp/jwks.json",
				secureKafkaJWKS(t, &broker.oauthKey.PublicKey),
				0o644,
			)
		} else {
			if len(options.oauthJWKSTrustPEM) == 0 ||
				len(options.hostAccessPorts) == 0 {
				t.Fatal("HTTPS JWKS broker options require trust and host access")
			}
			copySecureKafkaFile(
				t,
				ctx,
				container,
				"/tmp/jwks-ca.pem",
				options.oauthJWKSTrustPEM,
				0o644,
			)
		}
	default:
		t.Fatalf("unknown secured Apache Kafka mode %d", mode)
	}

	copySecureKafkaFile(
		t,
		ctx,
		container,
		"/tmp/server.properties",
		[]byte(secureKafkaServerProperties(mode, endpoint, options.oauthJWKSURL)),
		0o644,
	)
	copySecureKafkaFile(
		t,
		ctx,
		container,
		"/tmp/golib-kafka-secure-start.sh",
		[]byte(secureKafkaStartScript(mode, options.oauthJWKSURL)),
		0o755,
	)
	copySecureKafkaFile(
		t,
		ctx,
		container,
		"/tmp/golib-kafka-secure-ready",
		[]byte("ready\n"),
		0o644,
	)

	if err := wait.ForLog("Transition from STARTING to STARTED").
		WithStartupTimeout(90*time.Second).
		WithPollInterval(100*time.Millisecond).
		WaitUntilReady(ctx, container); err != nil {
		diagnostic, diagnosticErr := secureKafkaStartupDiagnostic(
			ctx,
			container,
			[]string{
				storePassword,
				broker.plainPassword,
				broker.limitedPassword,
				broker.scram256Password,
				broker.scram512Password,
			},
		)
		if diagnosticErr != nil {
			t.Logf("read secured Apache Kafka startup diagnostic: %v", diagnosticErr)
		} else if diagnostic != "" {
			t.Logf("secured Apache Kafka startup diagnostic:\n%s", diagnostic)
		}
		t.Fatalf("wait for secured Apache Kafka broker: %v", err)
	}

	return broker
}

func secureKafkaStartupDiagnostic(
	ctx context.Context,
	container testcontainers.Container,
	secrets []string,
) (string, error) {
	logs, err := container.Logs(ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()
	data, err := io.ReadAll(io.LimitReader(logs, secureKafkaDiagnosticInput))
	if err != nil {
		return "", err
	}
	if len(data) > secureKafkaDiagnosticBytes {
		data = data[len(data)-secureKafkaDiagnosticBytes:]
	}
	diagnostic := string(data)
	for _, secret := range secrets {
		if secret != "" {
			diagnostic = strings.ReplaceAll(diagnostic, secret, "[redacted]")
		}
	}

	return diagnostic, nil
}

func waitForSecureKafkaPortEndpoint(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
) string {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		endpoint, err := container.PortEndpoint(waitCtx, secureKafkaClientPort, "")
		if err == nil {
			return endpoint
		}
		lastErr = err
		state, stateErr := container.State(waitCtx)
		if stateErr == nil && !state.Running {
			t.Fatalf(
				"resolve secured Apache Kafka endpoint: container %s, exit %d",
				state.Status,
				state.ExitCode,
			)
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"resolve secured Apache Kafka endpoint: %v; last error: %v",
				context.Cause(waitCtx),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func secureKafkaServerProperties(
	mode secureKafkaMode,
	endpoint string,
	oauthJWKSURL string,
) string {
	listener := "SSL"
	if mode != secureKafkaMutualTLS {
		listener = "SASL_SSL"
	}
	properties := fmt.Sprintf(
		"process.roles=broker,controller\n"+
			"node.id=1\n"+
			"controller.quorum.voters=1@localhost:%d\n"+
			"controller.listener.names=CONTROLLER\n"+
			"listeners=%s://:9094,INTERNAL://:%d,CONTROLLER://:%d\n"+
			"advertised.listeners=%s://%s,INTERNAL://localhost:%d\n"+
			"listener.security.protocol.map=SSL:SSL,SASL_SSL:SASL_SSL,"+
			"INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT\n"+
			"inter.broker.listener.name=INTERNAL\n"+
			"log.dirs=/tmp/golib-kafka-secure-data\n"+
			"num.partitions=1\n"+
			"offsets.topic.replication.factor=1\n"+
			"transaction.state.log.replication.factor=1\n"+
			"transaction.state.log.min.isr=1\n"+
			"share.coordinator.state.topic.replication.factor=1\n"+
			"share.coordinator.state.topic.min.isr=1\n"+
			"group.initial.rebalance.delay.ms=0\n"+
			"log.cleaner.backoff.ms=100\n"+
			"auto.create.topics.enable=false\n"+
			"config.providers=file\n"+
			"config.providers.file.class="+
			"org.apache.kafka.common.config.provider.FileConfigProvider\n"+
			"ssl.keystore.location=/tmp/server.p12\n"+
			"ssl.keystore.type=PKCS12\n"+
			"ssl.keystore.password=${file:/tmp/store.properties:password}\n"+
			"ssl.key.password=${file:/tmp/store.properties:password}\n"+
			"ssl.enabled.protocols=TLSv1.2,TLSv1.3\n",
		secureKafkaControllerPort,
		listener,
		secureKafkaInternalPort,
		secureKafkaControllerPort,
		listener,
		endpoint,
		secureKafkaInternalPort,
	)

	switch mode {
	case secureKafkaMutualTLS:
		properties += "ssl.truststore.location=/tmp/ca.pem\n" +
			"ssl.truststore.type=PEM\n" +
			"ssl.client.auth=required\n" +
			"connections.max.idle.ms=2000\n"
	case secureKafkaSASL:
		properties += "ssl.client.auth=none\n" +
			"authorizer.class.name=" +
			"org.apache.kafka.metadata.authorizer.StandardAuthorizer\n" +
			"allow.everyone.if.no.acl.found=false\n" +
			"super.users=User:ANONYMOUS;User:plain-user;" +
			"User:scram256-user;User:scram512-user\n" +
			"sasl.enabled.mechanisms=PLAIN,SCRAM-SHA-256,SCRAM-SHA-512\n" +
			"listener.name.sasl_ssl.scram-sha-256." +
			"connections.max.reauth.ms=3000\n" +
			"listener.name.sasl_ssl.scram-sha-512." +
			"connections.max.reauth.ms=3000\n" +
			"listener.name.sasl_ssl.plain.sasl.jaas.config=" +
			"org.apache.kafka.common.security.plain.PlainLoginModule required " +
			"user_plain-user=\"${file:/tmp/plain.properties:password}\" " +
			"user_limited-user=" +
			"\"${file:/tmp/plain.properties:limited-password}\";\n" +
			"listener.name.sasl_ssl.scram-sha-256.sasl.jaas.config=" +
			"org.apache.kafka.common.security.scram.ScramLoginModule required;\n" +
			"listener.name.sasl_ssl.scram-sha-512.sasl.jaas.config=" +
			"org.apache.kafka.common.security.scram.ScramLoginModule required;\n"
	case secureKafkaOAuth:
		if oauthJWKSURL == "" {
			oauthJWKSURL = "file:///tmp/jwks.json"
		}
		properties += "ssl.client.auth=none\n" +
			"sasl.enabled.mechanisms=OAUTHBEARER\n" +
			"listener.name.sasl_ssl.oauthbearer." +
			"connections.max.reauth.ms=3000\n" +
			"listener.name.sasl_ssl.oauthbearer.sasl.jaas.config=" +
			"org.apache.kafka.common.security.oauthbearer." +
			"OAuthBearerLoginModule required;\n" +
			"listener.name.sasl_ssl.oauthbearer." +
			"sasl.oauthbearer.expected.audience=" + secureKafkaAudience + "\n" +
			"listener.name.sasl_ssl.oauthbearer." +
			"sasl.oauthbearer.expected.issuer=" + secureKafkaIssuer + "\n" +
			"listener.name.sasl_ssl.oauthbearer." +
			"sasl.oauthbearer.jwks.endpoint.url=" + oauthJWKSURL + "\n" +
			"listener.name.sasl_ssl.oauthbearer." +
			"sasl.server.callback.handler.class=" +
			"org.apache.kafka.common.security.oauthbearer." +
			"OAuthBearerValidatorCallbackHandler\n"
		if strings.HasPrefix(oauthJWKSURL, "https://") {
			properties += "listener.name.sasl_ssl.oauthbearer." +
				"sasl.oauthbearer.jwks.endpoint.refresh.ms=1000\n" +
				"listener.name.sasl_ssl.oauthbearer." +
				"sasl.oauthbearer.jwks.endpoint.retry.backoff.ms=100\n" +
				"listener.name.sasl_ssl.oauthbearer." +
				"sasl.oauthbearer.jwks.endpoint.retry.backoff.max.ms=1000\n"
		}
	}

	return properties
}

func secureKafkaStartScript(mode secureKafkaMode, oauthJWKSURL string) string {
	format := "/opt/kafka/bin/kafka-storage.sh format --ignore-formatted " +
		"--cluster-id " + apacheKafkaClusterID + " " +
		"--config /tmp/server.properties"
	preparation := ""
	if mode == secureKafkaSASL {
		preparation = "scram256_password=\"$(cat /tmp/scram256-password)\"\n" +
			"scram512_password=\"$(cat /tmp/scram512-password)\"\n"
		format += " --add-scram \"SCRAM-SHA-256=" +
			"[name=scram256-user,password=$scram256_password]\"" +
			" --add-scram \"SCRAM-SHA-512=" +
			"[name=scram512-user,password=$scram512_password]\""
	}
	if mode == secureKafkaOAuth {
		if oauthJWKSURL == "" {
			oauthJWKSURL = "file:///tmp/jwks.json"
		} else {
			preparation += "keytool -importcert -noprompt " +
				"-alias golib-jwks-ca -file /tmp/jwks-ca.pem " +
				"-keystore /tmp/jwks-truststore.p12 -storetype PKCS12 " +
				"-storepass changeit >/dev/null 2>&1\n"
		}
		preparation += "export KAFKA_OPTS=" +
			"\"-Dorg.apache.kafka.sasl.oauthbearer.allowed.urls=" +
			oauthJWKSURL
		if strings.HasPrefix(oauthJWKSURL, "https://") {
			preparation += " -Djavax.net.ssl.trustStore=" +
				"/tmp/jwks-truststore.p12" +
				" -Djavax.net.ssl.trustStoreType=PKCS12" +
				" -Djavax.net.ssl.trustStorePassword=changeit"
		}
		preparation += "\"\n"
	}

	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  for secret in /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties /tmp/plain.properties " +
		"/tmp/scram256-password /tmp/scram512-password; do\n" +
		"    if [ -e \"$secret\" ]; then\n" +
		"      chown 1000:1000 \"$secret\"\n" +
		"      chmod 0600 \"$secret\"\n" +
		"    fi\n" +
		"  done\n" +
		"  exec su appuser -s /bin/bash -c " +
		"'exec /bin/bash /tmp/golib-kafka-secure-start.sh'\n" +
		"fi\n" +
		"umask 077\n" +
		"openssl pkcs12 -export -name broker " +
		"-inkey /tmp/server-key.pem -in /tmp/server.pem " +
		"-certfile /tmp/ca.pem -out /tmp/server.p12 " +
		"-passout file:/tmp/store-password >/dev/null 2>&1\n" +
		preparation +
		format + " >/dev/null\n" +
		"unset scram256_password scram512_password 2>/dev/null || true\n" +
		"exec /opt/kafka/bin/kafka-server-start.sh /tmp/server.properties\n"
}

func newSecureKafkaPKI(t *testing.T, endpointHost string) secureKafkaPKI {
	t.Helper()

	now := time.Now()
	caKey := newSecureKafkaRSAKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "golib-kafka-test-ca"},
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
		t.Fatalf("create secured Kafka CA: %v", err)
	}

	serverKey := newSecureKafkaRSAKey(t)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "golib-kafka-broker"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature |
			x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if endpointIP := net.ParseIP(endpointHost); endpointIP != nil {
		serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, endpointIP)
	} else if endpointHost != "" && endpointHost != "localhost" {
		serverTemplate.DNSNames = append(serverTemplate.DNSNames, endpointHost)
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader,
		serverTemplate,
		caTemplate,
		&serverKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create secured Kafka server certificate: %v", err)
	}

	clientPEM, clientKeyPEM, clientIdentity := newSecureKafkaClientIdentity(
		t,
		now,
		caTemplate,
		caKey,
		3,
		"golib-kafka-client",
	)
	_, _, rotatedClientIdentity := newSecureKafkaClientIdentity(
		t,
		now,
		caTemplate,
		caKey,
		4,
		"golib-kafka-client-rotated",
	)

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	serverPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: serverDER,
	})
	serverKeyPEM := secureKafkaPrivateKeyPEM(t, serverKey)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append secured Kafka CA")
	}

	return secureKafkaPKI{
		caDER:                 append([]byte(nil), caDER...),
		caPEM:                 caPEM,
		serverDER:             append([]byte(nil), serverDER...),
		serverPEM:             serverPEM,
		serverKeyPEM:          serverKeyPEM,
		clientPEM:             clientPEM,
		clientKeyPEM:          clientKeyPEM,
		clientIdentity:        clientIdentity,
		rotatedClientIdentity: rotatedClientIdentity,
		roots:                 roots,
	}
}

func newSecureKafkaClientIdentity(
	t *testing.T,
	now time.Time,
	caTemplate *x509.Certificate,
	caKey *rsa.PrivateKey,
	serialNumber int64,
	commonName string,
) ([]byte, []byte, tls.Certificate) {
	t.Helper()

	clientKey := newSecureKafkaRSAKey(t)
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(serialNumber),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(
		rand.Reader,
		clientTemplate,
		caTemplate,
		&clientKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create secured Kafka client certificate: %v", err)
	}

	clientPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: clientDER,
	})
	clientKeyPEM := secureKafkaPrivateKeyPEM(t, clientKey)
	clientIdentity, err := tls.X509KeyPair(clientPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("parse secured Kafka client identity: %v", err)
	}
	return clientPEM, clientKeyPEM, clientIdentity
}

func newSecureKafkaRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate secured Kafka RSA key: %v", err)
	}

	return key
}

func secureKafkaPrivateKeyPEM(
	t *testing.T,
	key *rsa.PrivateKey,
) []byte {
	t.Helper()

	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal secured Kafka private key: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}

func secureKafkaJWKS(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()

	return secureKafkaJWKSForKeys(t, []secureKafkaJWK{{
		keyID: secureKafkaOAuthKeyID,
		key:   key,
	}})
}

type secureKafkaJWK struct {
	keyID string
	key   *rsa.PublicKey
}

func secureKafkaJWKSForKeys(t *testing.T, keys []secureKafkaJWK) []byte {
	t.Helper()

	encodedKeys := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		encodedKeys = append(encodedKeys, map[string]string{
			"kty": "RSA",
			"kid": key.keyID,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(key.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(
				big.NewInt(int64(key.key.E)).Bytes(),
			),
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"keys": encodedKeys,
	})
	if err != nil {
		t.Fatalf("marshal secured Kafka JWKS: %v", err)
	}

	return encoded
}

func (broker *secureKafkaBroker) issueOAuthToken(
	t *testing.T,
	audience string,
	lifetime time.Duration,
) ([]byte, time.Time) {
	t.Helper()

	return broker.issueOAuthTokenForIssuer(
		t,
		secureKafkaIssuer,
		audience,
		lifetime,
	)
}

func (broker *secureKafkaBroker) issueOAuthTokenForIssuer(
	t *testing.T,
	issuer string,
	audience string,
	lifetime time.Duration,
) ([]byte, time.Time) {
	t.Helper()

	return issueSecureKafkaOAuthToken(
		t,
		broker.oauthKey,
		secureKafkaOAuthKeyID,
		issuer,
		audience,
		lifetime,
	)
}

func issueSecureKafkaOAuthToken(
	t *testing.T,
	key *rsa.PrivateKey,
	keyID string,
	issuer string,
	audience string,
	lifetime time.Duration,
) ([]byte, time.Time) {
	t.Helper()

	now := time.Now()
	expiresAt := now.Add(lifetime)
	header, err := json.Marshal(map[string]string{
		"alg": "RS256", "typ": "JWT", "kid": keyID,
	})
	if err != nil {
		t.Fatalf("marshal secured Kafka JWT header: %v", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": issuer,
		"aud": audience,
		"sub": "golib-client",
		"iat": now.Add(-time.Second).Unix(),
		"exp": expiresAt.Unix(),
		"jti": randomSecureKafkaCredential(t),
	})
	if err != nil {
		t.Fatalf("marshal secured Kafka JWT claims: %v", err)
	}
	encoder := base64.RawURLEncoding
	signed := encoder.EncodeToString(header) + "." + encoder.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(
		rand.Reader,
		key,
		crypto.SHA256,
		digest[:],
	)
	if err != nil {
		t.Fatalf("sign secured Kafka JWT: %v", err)
	}

	return []byte(signed + "." + encoder.EncodeToString(signature)), expiresAt
}

func randomSecureKafkaCredential(t *testing.T) string {
	t.Helper()

	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate secured Kafka credential: %v", err)
	}

	return base64.RawURLEncoding.EncodeToString(value)
}

func copySecureKafkaFile(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	path string,
	data []byte,
	mode int64,
) {
	t.Helper()

	if err := container.CopyToContainer(ctx, data, path, mode); err != nil {
		t.Fatalf("copy secured Kafka fixture %s: %v", path, err)
	}
}

func (broker *secureKafkaBroker) serverTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    broker.pki.roots.Clone(),
	}
}

func (broker *secureKafkaBroker) staticMutualTLSConfig() *tls.Config {
	config := broker.serverTLSConfig()
	config.Certificates = []tls.Certificate{broker.pki.clientIdentity}

	return config
}

func (broker *secureKafkaBroker) assertTLSVersion(
	t *testing.T,
	version uint16,
) {
	t.Helper()

	config := broker.staticMutualTLSConfig()
	config.MinVersion = version
	config.MaxVersion = version
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	connection, err := tls.DialWithDialer(dialer, "tcp", broker.endpoint, config)
	if err != nil {
		t.Fatalf("dial secured Kafka with TLS %x: %v", version, err)
	}
	defer connection.Close()
	if connection.ConnectionState().Version != version {
		t.Fatalf(
			"secured Kafka TLS version = %x, want %x",
			connection.ConnectionState().Version,
			version,
		)
	}
}

func (broker *secureKafkaBroker) assertRuntimeVersions(
	t *testing.T,
	ctx context.Context,
) {
	t.Helper()

	assertSecureKafkaCommandOutput(
		t,
		ctx,
		broker.container,
		[]string{"/opt/kafka/bin/kafka-topics.sh", "--version"},
		"4.3.1",
	)
	assertSecureKafkaCommandPrefix(
		t,
		ctx,
		broker.container,
		[]string{"openssl", "version"},
		"OpenSSL 3.5.7 ",
	)
	assertSecureKafkaCommandOutput(
		t,
		ctx,
		broker.container,
		[]string{"sh", "-c", "awk '/^Uid:/{print $2}' /proc/1/status"},
		"1000",
	)
}

func assertSecureKafkaCommandOutput(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	command []string,
	want string,
) {
	t.Helper()

	exitCode, output, err := container.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("execute secured Kafka version command: %v", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(output, 256))
	if readErr != nil {
		t.Fatalf("read secured Kafka version command: %v", readErr)
	}
	if exitCode != 0 || strings.TrimSpace(string(data)) != want {
		t.Fatalf(
			"secured Kafka version output = %q, exit %d; want %q",
			strings.TrimSpace(string(data)),
			exitCode,
			want,
		)
	}
}

func assertSecureKafkaCommandPrefix(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	command []string,
	wantPrefix string,
) {
	t.Helper()

	exitCode, output, err := container.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("execute secured Kafka tool command: %v", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(output, 256))
	if readErr != nil {
		t.Fatalf("read secured Kafka tool command: %v", readErr)
	}
	if exitCode != 0 || !strings.HasPrefix(strings.TrimSpace(string(data)), wantPrefix) {
		t.Fatalf(
			"secured Kafka tool output = %q, exit %d; want prefix %q",
			strings.TrimSpace(string(data)),
			exitCode,
			wantPrefix,
		)
	}
}

func createSecureKafkaTopic(
	t *testing.T,
	ctx context.Context,
	broker string,
	tlsConfig *tls.Config,
	mechanism sasl.Mechanism,
	topic string,
) {
	t.Helper()
	createSecureKafkaTopicWithConfigs(
		t,
		ctx,
		broker,
		tlsConfig,
		mechanism,
		topic,
		nil,
	)
}

func createSecureKafkaTopicWithConfigs(
	t *testing.T,
	ctx context.Context,
	broker string,
	tlsConfig *tls.Config,
	mechanism sasl.Mechanism,
	topic string,
	configs map[string]*string,
) {
	t.Helper()

	options := []kgo.Opt{
		kgo.SeedBrokers(broker),
		kgo.ClientID("golib-secure-admin"),
		kgo.DialTLSConfig(tlsConfig),
	}
	if mechanism != nil {
		options = append(options, kgo.SASL(mechanism))
	}
	client, err := kgo.NewClient(options...)
	if err != nil {
		t.Fatalf("construct secured Kafka administrator: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).CreateTopics(ctx, 1, 1, configs, topic)
	if err != nil {
		t.Fatalf("create secured Kafka topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists {
		t.Fatalf("secured Kafka topic response omitted %q", topic)
	}
	if response.Err != nil {
		t.Fatalf("create secured Kafka topic %q: %v", topic, response.Err)
	}
}

func createSecureKafkaGroupDescribeACL(
	t *testing.T,
	ctx context.Context,
	broker string,
	tlsConfig *tls.Config,
	mechanism sasl.Mechanism,
	principal string,
	group string,
	topic string,
) {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker),
		kgo.ClientID("golib-secure-acl-admin"),
		kgo.DialTLSConfig(tlsConfig),
		kgo.SASL(mechanism),
	)
	if err != nil {
		t.Fatalf("construct secured Kafka ACL administrator: %v", err)
	}
	defer client.Close()
	var offsets kadm.Offsets
	offsets.Add(kadm.Offset{Topic: topic, Partition: 0, At: 0})
	commitResults, err := kadm.NewClient(client).CommitOffsets(ctx, group, offsets)
	if err != nil {
		t.Fatalf("initialize secured Kafka group offset: %v", err)
	}
	if err := commitResults.Error(); err != nil {
		t.Fatalf("secured Kafka group offset results: %v", err)
	}
	builder := kadm.NewACLs().
		Groups(group).
		Topics(topic).
		Allow(principal).
		Operations(kadm.OpDescribe).
		ResourcePatternType(kadm.ACLPatternLiteral)
	builder.PrefixUser()
	results, err := kadm.NewClient(client).CreateACLs(ctx, builder)
	if err != nil {
		t.Fatalf("create secured Kafka group ACL: %v", err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("secured Kafka group ACL results = %#v", results)
	}
}

func waitForSecureKafkaCompaction(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	topic string,
	missingOffset int64,
) int64 {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		client, err := kgo.NewClient(
			kgo.SeedBrokers(broker.endpoint),
			kgo.ClientID("golib-secure-compaction-observer"),
			kgo.DialTLSConfig(broker.staticMutualTLSConfig()),
			kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
				topic: {0: kgo.NewOffset().At(missingOffset)},
			}),
			kgo.FetchMaxWait(100*time.Millisecond),
		)
		if err != nil {
			t.Fatalf("construct secured Kafka compaction observer: %v", err)
		}
		pollCtx, cancelPoll := context.WithTimeout(waitCtx, time.Second)
		fetches := client.PollRecords(pollCtx, 100)
		cancelPoll()
		starts, startErr := kadm.NewClient(client).ListStartOffsets(waitCtx, topic)
		client.Close()
		if fetchErr := fetches.Err(); fetchErr != nil {
			t.Fatalf("fetch secured Kafka compacted offsets: %v", fetchErr)
		}
		start, exists := starts.Lookup(topic, 0)
		if startErr != nil || !exists || start.Err != nil {
			t.Fatalf("list compacted topic start offset: %#v/%v", start, startErr)
		}
		if start.Offset != missingOffset {
			t.Fatalf(
				"compacted topic start offset = %d, want %d",
				start.Offset,
				missingOffset,
			)
		}
		records := fetches.Records()
		if len(records) > 0 && records[0].Offset > missingOffset {
			return records[0].Offset
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf("wait for secured Kafka compaction: %v", context.Cause(waitCtx))
		case <-ticker.C:
		}
	}
}

func restartSecureKafkaWithPlainCredential(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	adminMechanism sasl.Mechanism,
	password string,
) {
	t.Helper()

	copySecureKafkaFile(
		t,
		ctx,
		broker.container,
		"/tmp/plain.properties",
		[]byte(
			"password="+password+"\n"+
				"limited-password="+broker.limitedPassword+"\n",
		),
		0o600,
	)
	stopCtx, cancelStop := context.WithTimeout(ctx, 10*time.Second)
	stopTimeout := 5 * time.Second
	stopErr := broker.container.Stop(stopCtx, &stopTimeout)
	cancelStop()
	if stopErr != nil {
		t.Fatalf("stop secured Kafka for PLAIN credential replacement: %v", stopErr)
	}
	startCtx, cancelStart := context.WithTimeout(ctx, 30*time.Second)
	startErr := broker.container.Start(startCtx)
	cancelStart()
	if startErr != nil {
		t.Fatalf("restart secured Kafka after PLAIN credential replacement: %v", startErr)
	}
	restartedEndpoint := waitForSecureKafkaPortEndpoint(t, ctx, broker.container)
	if restartedEndpoint != broker.endpoint {
		t.Fatalf(
			"secured Kafka endpoint after PLAIN replacement = %q, want %q",
			restartedEndpoint,
			broker.endpoint,
		)
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker.endpoint),
		kgo.ClientID("golib-secure-plain-restart-observer"),
		kgo.DialTLSConfig(broker.serverTLSConfig()),
		kgo.SASL(adminMechanism),
	)
	if err != nil {
		t.Fatalf("construct secured Kafka PLAIN restart observer: %v", err)
	}
	defer client.Close()
	readinessCtx, cancelReadiness := context.WithTimeout(ctx, 60*time.Second)
	defer cancelReadiness()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		attemptCtx, cancelAttempt := context.WithTimeout(readinessCtx, 2*time.Second)
		lastErr = client.Ping(attemptCtx)
		cancelAttempt()
		if lastErr == nil {
			return
		}
		select {
		case <-readinessCtx.Done():
			diagnosticCtx, cancelDiagnostic := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			diagnostic, diagnosticErr := secureKafkaStartupDiagnostic(
				diagnosticCtx,
				broker.container,
				[]string{
					password,
					broker.plainPassword,
					broker.limitedPassword,
					broker.scram256Password,
					broker.scram512Password,
				},
			)
			cancelDiagnostic()
			if diagnosticErr != nil {
				t.Logf("read restarted secured Kafka diagnostic: %v", diagnosticErr)
			} else if diagnostic != "" {
				t.Logf("restarted secured Kafka diagnostic:\n%s", diagnostic)
			}
			t.Fatalf("wait for secured Kafka after PLAIN credential replacement: %v", lastErr)
		case <-ticker.C:
		}
	}
}

func alterSecureKafkaSCRAMCredential(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	adminMechanism sasl.Mechanism,
	username string,
	password string,
	mechanism kadm.ScramMechanism,
) {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker.endpoint),
		kgo.ClientID("golib-secure-credential-administrator"),
		kgo.DialTLSConfig(broker.serverTLSConfig()),
		kgo.SASL(adminMechanism),
	)
	if err != nil {
		t.Fatalf("construct secured Kafka credential administrator: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).AlterUserSCRAMs(
		ctx,
		nil,
		[]kadm.UpsertSCRAM{{
			User:       username,
			Mechanism:  mechanism,
			Iterations: 4096,
			Password:   password,
		}},
	)
	if err != nil {
		t.Fatalf("alter secured Kafka SCRAM credential: %v", err)
	}
	response, exists := responses[username]
	if !exists {
		t.Fatalf("secured Kafka SCRAM response omitted %q", username)
	}
	if response.Err != nil {
		t.Fatalf("alter secured Kafka SCRAM credential for %q: %v", username, response.Err)
	}
}

func rotateSecureKafkaServerCertificate(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	pki secureKafkaPKI,
) {
	t.Helper()

	copySecureKafkaFile(t, ctx, broker.container, "/tmp/rotated-ca.pem", pki.caPEM, 0o644)
	copySecureKafkaFile(
		t,
		ctx,
		broker.container,
		"/tmp/rotated-server.pem",
		pki.serverPEM,
		0o644,
	)
	copySecureKafkaFile(
		t,
		ctx,
		broker.container,
		"/tmp/rotated-server-key.pem",
		pki.serverKeyPEM,
		0o600,
	)
	exitCode, output, err := broker.container.Exec(
		ctx,
		[]string{
			"sh",
			"-c",
			"openssl pkcs12 -export -name broker " +
				"-inkey /tmp/rotated-server-key.pem " +
				"-in /tmp/rotated-server.pem -certfile /tmp/rotated-ca.pem " +
				"-out /tmp/rotated-server.p12 " +
				"-passout file:/tmp/store-password >/dev/null 2>&1 && " +
				"chown 1000:1000 /tmp/rotated-server.p12 && " +
				"chmod 0600 /tmp/rotated-server.p12",
		},
		tcexec.Multiplexed(),
	)
	if err != nil {
		t.Fatalf("create rotated secured Kafka keystore: %v", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(output, 1024))
	if readErr != nil {
		t.Fatalf("read rotated secured Kafka keystore command: %v", readErr)
	}
	if exitCode != 0 {
		diagnostic := strings.ReplaceAll(
			string(data),
			broker.storePassword,
			"[redacted]",
		)
		t.Fatalf(
			"create rotated secured Kafka keystore: exit %d: %s",
			exitCode,
			strings.TrimSpace(diagnostic),
		)
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker.endpoint),
		kgo.ClientID("golib-secure-certificate-rotation-admin"),
		kgo.DialTLSConfig(broker.staticMutualTLSConfig()),
	)
	if err != nil {
		t.Fatalf("construct secured Kafka certificate rotation admin: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).AlterBrokerConfigs(
		ctx,
		[]kadm.AlterConfig{{
			Op:    kadm.SetConfig,
			Name:  "listener.name.ssl.ssl.keystore.location",
			Value: kadm.StringPtr("/tmp/rotated-server.p12"),
		}},
		1,
	)
	if err != nil {
		t.Fatalf("alter secured Kafka listener keystore: %v", err)
	}
	response, responseErr := responses.On("1", nil)
	if responseErr != nil {
		t.Fatalf("secured Kafka listener keystore response: %v", responseErr)
	}
	if response.Err != nil {
		t.Fatalf("alter secured Kafka listener keystore: %v", response.Err)
	}
}

func waitForSecureKafkaServerCertificate(
	t *testing.T,
	ctx context.Context,
	broker *secureKafkaBroker,
	pki secureKafkaPKI,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	host, _, err := net.SplitHostPort(broker.endpoint)
	if err != nil {
		t.Fatalf("parse secured Kafka endpoint for certificate verification: %v", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		connection, dialErr := (&tls.Dialer{
			NetDialer: &net.Dialer{Timeout: time.Second},
			Config: &tls.Config{
				MinVersion:   tls.VersionTLS12,
				RootCAs:      pki.roots.Clone(),
				Certificates: []tls.Certificate{broker.pki.clientIdentity},
				ServerName:   host,
			},
		}).DialContext(waitCtx, "tcp", broker.endpoint)
		if dialErr == nil {
			state := connection.(*tls.Conn).ConnectionState()
			closeErr := connection.Close()
			if closeErr != nil {
				t.Fatalf("close rotated secured Kafka TLS connection: %v", closeErr)
			}
			if len(state.PeerCertificates) != 0 &&
				bytes.Equal(state.PeerCertificates[0].Raw, pki.serverDER) {
				return
			}
			lastErr = errors.New("secured Kafka returned the previous certificate")
		} else {
			lastErr = dialErr
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"wait for rotated secured Kafka server certificate: %v; last error: %v",
				context.Cause(waitCtx),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitForSecureKafkaIdleExpiry(t *testing.T, ctx context.Context) {
	t.Helper()

	idleExpiry := time.NewTimer(3 * time.Second)
	defer idleExpiry.Stop()
	select {
	case <-idleExpiry.C:
	case <-ctx.Done():
		t.Fatal("secured Kafka context expired before idle connection expiry")
	}
}

func publishSecureRecord(
	t *testing.T,
	ctx context.Context,
	broker string,
	topic string,
	value string,
	security kafka.ClientSecurity,
	observers kafka.ObserverPolicy,
) {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-secure-producer-" + value,
		AllowedTopics: []string{topic},
		Security:      security,
		Observers:     observers,
	})
	if err != nil {
		t.Fatalf("construct secured Kafka producer: %v", err)
	}
	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte(value),
		Value: []byte(value),
	})
	if result.Err != nil || result.Topic != topic || result.Timestamp.IsZero() {
		_ = producer.Close()
		t.Fatalf("secured Kafka delivery = %#v", result)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close secured Kafka producer: %v", err)
	}
}

func consumeSecureRecords(
	t *testing.T,
	ctx context.Context,
	broker string,
	topic string,
	groupID string,
	want int,
	security kafka.ClientSecurity,
) []string {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           []string{broker},
		ClientID:          groupID,
		GroupID:           groupID,
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		MaxPollRecords:    want,
		FetchMaxWait:      100 * time.Millisecond,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  10 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    3 * time.Second,
		CommitTimeout:     2 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          security,
	})
	if err != nil {
		t.Fatalf("construct secured Kafka consumer: %v", err)
	}
	values := make([]string, 0, want)
	transientFailures := 0
	for len(values) < want {
		result, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			values = append(values, string(message.Value))
			return nil
		}))
		if runErr != nil {
			var consumerErr *kafka.ConsumerError
			if errors.As(runErr, &consumerErr) &&
				consumerErr.Retryable() &&
				transientFailures < secureKafkaTransientRetries &&
				ctx.Err() == nil {
				transientFailures++
				continue
			}
			_ = consumer.Close()
			t.Fatalf("consume secured Kafka records: %v", runErr)
		}
		transientFailures = 0
		if result.Polled == 0 && ctx.Err() != nil {
			_ = consumer.Close()
			t.Fatalf("consume secured Kafka records: %v", ctx.Err())
		}
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close secured Kafka consumer: %v", err)
	}

	return values
}

func usernamePasswordSecurity(
	broker *secureKafkaBroker,
	username string,
	password string,
	build func(kafka.UsernamePasswordProvider) kafka.Authentication,
) (kafka.ClientSecurity, *atomic.Int64) {
	var calls atomic.Int64
	provider := kafka.UsernamePasswordProviderFunc(func(
		context.Context,
	) (kafka.UsernamePassword, error) {
		calls.Add(1)
		return kafka.UsernamePassword{
			Username: username,
			Password: []byte(password),
		}, nil
	})

	return kafka.ClientSecurity{
		TLS:               broker.serverTLSConfig(),
		Authentication:    build(provider),
		CredentialTimeout: time.Second,
	}, &calls
}

func assertSecureKafkaHealthFailure(
	t *testing.T,
	ctx context.Context,
	broker string,
	security kafka.ClientSecurity,
	forbidden []string,
) {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-secure-rejected-producer",
		AllowedTopics: []string{"golib-secure-rejected"},
		DialTimeout:   2 * time.Second,
		Security:      security,
	})
	if err != nil {
		t.Fatalf("construct rejected secured Kafka producer: %v", err)
	}
	healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	healthErr := producer.Health(healthCtx)
	if healthErr == nil {
		_ = producer.Close()
		t.Fatal("secured Kafka broker accepted invalid authentication")
	}
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(healthErr.Error(), secret) {
			_ = producer.Close()
			t.Fatal("secured Kafka authentication failure disclosed a credential")
		}
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close rejected secured Kafka producer: %v", err)
	}
}
