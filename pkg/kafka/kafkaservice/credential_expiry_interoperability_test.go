//go:build interoperability

package kafkaservice_test

import (
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
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

const (
	credentialExpiryKafkaImage = "apache/kafka:4.3.1@" +
		"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"
	credentialExpiryKafkaClusterID = "4L6g3nShT-e" + "MCtK--X86sw"
	credentialExpiryIssuer         = "https://issuer.kafkaservice.test"
	credentialExpiryAudience       = "kafkaservice"
	credentialExpiryKeyID          = "kafkaservice-expiry-test-key"
	credentialExpiryInternalHost   = "localhost/127.0.0.1:19092"
)

type credentialExpiryBroker struct {
	endpoint   string
	container  string
	tlsConfig  *tls.Config
	signingKey *rsa.PrivateKey
}

type credentialExpiryToken struct {
	token      []byte
	expiresAt  time.Time
	generation int64
}

// TestLifecycleAdapterRecoversFromExpiredOAuthCredential proves that a
// concrete producer resource survives an Apache Kafka OAUTHBEARER expiry and
// returns to service through the lifecycle adapter. The expired JWT has a
// future provider expiry so that Apache Kafka, rather than the local provider
// validation, is the boundary that rejects its expired claim.
func TestLifecycleAdapterRecoversFromExpiredOAuthCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker := startCredentialExpiryKafka(t, ctx)
	topic := fmt.Sprintf("golib-kafkaservice-oauth-expiry-%d", time.Now().UnixNano())
	credentialExpiryDockerExec(
		t,
		ctx,
		broker.container,
		"",
		broker.endpoint,
		"/opt/kafka/bin/kafka-topics.sh",
		"--bootstrap-server",
		"localhost:19092",
		"--create",
		"--if-not-exists",
		"--partitions",
		"1",
		"--replication-factor",
		"1",
		"--topic",
		topic,
	)

	initialToken, initialExpiry := broker.issueToken(t, time.Minute)
	var current atomic.Pointer[credentialExpiryToken]
	current.Store(&credentialExpiryToken{
		token: initialToken, expiresAt: initialExpiry, generation: 1,
	})
	var observedGeneration atomic.Int64
	provider := kafka.OAuthBearerProviderFunc(func(
		context.Context,
	) (kafka.OAuthBearerToken, error) {
		credential := current.Load()
		observedGeneration.Store(credential.generation)

		return kafka.OAuthBearerToken{
			Token:     append([]byte(nil), credential.token...),
			ExpiresAt: credential.expiresAt,
		}, nil
	})
	security := kafka.ClientSecurity{
		TLS:               broker.tlsConfig.Clone(),
		Authentication:    kafka.NewOAuthBearerAuthentication(provider),
		CredentialTimeout: time.Second,
	}

	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatal("construct correlation factory")
	}
	resource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{broker.endpoint},
		ClientID:        "golib-kafkaservice-oauth-expiry",
		AllowedTopics:   []string{topic},
		DeliveryTimeout: 5 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 6 * time.Second,
		Security:        security,
	})
	if err != nil {
		t.Fatal("construct OAuth producer resource")
	}
	t.Cleanup(func() { _ = resource.Close() })
	adapter, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-oauth-expiry",
			Resource:    resource,
			Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Readiness: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatal("construct OAuth producer adapter")
	}
	component := adapter.Component()
	if err := component.Start(ctx); err != nil {
		t.Fatal("start OAuth producer adapter")
	}
	readiness, ok := adapter.Readiness()
	if !ok {
		t.Fatal("OAuth producer readiness is absent")
	}
	if err := readiness.Run(ctx); err != nil {
		t.Fatal("initial OAuth producer readiness failed")
	}
	parent, err := factory.Start()
	if err != nil {
		t.Fatal("create OAuth producer parent correlation")
	}
	publishCredentialExpiryRecord(t, ctx, adapter, parent, topic, "before-expiry")

	// A future ExpiresAt intentionally bypasses the root provider's local
	// expired-token guard. Apache Kafka must reject the signed, expired exp
	// claim during the next SASL handshake.
	expiredJWT, _ := broker.issueToken(t, -time.Minute)
	expiredProviderExpiry := time.Now().Add(time.Minute)
	current.Store(&credentialExpiryToken{
		token: expiredJWT, expiresAt: expiredProviderExpiry, generation: 2,
	})
	waitForCredentialExpiryBrokerRejection(t, ctx, broker, security, expiredJWT)
	failure := waitForCredentialExpiryAdapterFailure(
		t,
		ctx,
		readiness.Run,
		&observedGeneration,
		2,
	)
	assertCredentialExpiryDiagnostic(t, failure.Error(), string(expiredJWT), broker.endpoint)

	recoveredJWT, recoveredExpiry := broker.issueToken(t, time.Minute)
	current.Store(&credentialExpiryToken{
		token: recoveredJWT, expiresAt: recoveredExpiry, generation: 3,
	})
	waitForCredentialExpiryRecovery(t, ctx, readiness.Run, &observedGeneration, 3)
	publishCredentialExpiryRecord(t, ctx, adapter, parent, topic, "after-recovery")

	if err := component.Stop(ctx); err != nil {
		t.Fatal("stop recovered OAuth producer adapter")
	}
	if _, _, err := adapter.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{Topic: topic, Key: []byte("after-stop")},
	); !errors.Is(err, kafkaservice.ErrUnavailable) {
		t.Fatalf("publish after OAuth adapter stop classification = %t", errors.Is(err, kafkaservice.ErrUnavailable))
	}
}

func publishCredentialExpiryRecord(
	t *testing.T,
	ctx context.Context,
	adapter *kafkaservice.Producer[*kafka.Producer],
	parent correlation.Values,
	topic string,
	value string,
) {
	t.Helper()

	_, delivery, err := adapter.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		},
	)
	if err != nil || delivery.Err != nil || delivery.Topic != topic ||
		delivery.Timestamp.IsZero() {
		var callbackErr *kafkaservice.CallbackError
		var classifiedDelivery *kafka.DeliveryError
		errors.As(err, &callbackErr)
		errors.As(delivery.Err, &classifiedDelivery)
		category := kafka.ErrorUnknown
		if classifiedDelivery != nil {
			category = classifiedDelivery.Category()
		}
		t.Fatalf(
			"OAuth adapter delivery accepted=%t topic=%t timestamp=%t callback=%t category=%s",
			err == nil && delivery.Err == nil,
			delivery.Topic == topic,
			!delivery.Timestamp.IsZero(),
			callbackErr != nil && callbackErr.Operation == kafkaservice.CallbackPublish,
			category,
		)
	}
}

func waitForCredentialExpiryBrokerRejection(
	t *testing.T,
	ctx context.Context,
	broker *credentialExpiryBroker,
	security kafka.ClientSecurity,
	expiredJWT []byte,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		probe, err := kafka.NewProducer(kafka.ProducerConfig{
			Brokers:         []string{broker.endpoint},
			ClientID:        fmt.Sprintf("golib-kafkaservice-oauth-expiry-probe-%d", time.Now().UnixNano()),
			AllowedTopics:   []string{"golib-kafkaservice-oauth-expiry-probe"},
			DeliveryTimeout: 3 * time.Second,
			ShutdownTimeout: 4 * time.Second,
			RequestTimeout:  time.Second,
			DialTimeout:     time.Second,
			Security:        security,
		})
		if err != nil {
			t.Fatal("construct expired OAuth broker probe")
		}
		probeCtx, cancelProbe := context.WithTimeout(waitCtx, 4*time.Second)
		probeErr := probe.Health(probeCtx)
		cancelProbe()
		if closeErr := probe.Close(); closeErr != nil {
			t.Fatal("close expired OAuth broker probe")
		}
		if probeErr != nil {
			assertCredentialExpiryDiagnostic(t, probeErr.Error(), string(expiredJWT), broker.endpoint)

			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("Apache Kafka did not reject the expired OAuth JWT")
		case <-ticker.C:
		}
	}
}

func waitForCredentialExpiryAdapterFailure(
	t *testing.T,
	ctx context.Context,
	readiness func(context.Context) error,
	observed *atomic.Int64,
	wantGeneration int64,
) error {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		attemptCtx, cancelAttempt := context.WithTimeout(waitCtx, 2*time.Second)
		err := readiness(attemptCtx)
		cancelAttempt()
		if observed.Load() >= wantGeneration && err != nil {
			return err
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("OAuth adapter did not surface the broker expiry rejection")
		case <-ticker.C:
		}
	}
}

func waitForCredentialExpiryRecovery(
	t *testing.T,
	ctx context.Context,
	readiness func(context.Context) error,
	observed *atomic.Int64,
	wantGeneration int64,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		attemptCtx, cancelAttempt := context.WithTimeout(waitCtx, 2*time.Second)
		err := readiness(attemptCtx)
		cancelAttempt()
		if err == nil && observed.Load() >= wantGeneration {
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("OAuth adapter did not recover with the replacement credential")
		case <-ticker.C:
		}
	}
}

func assertCredentialExpiryDiagnostic(t *testing.T, diagnostic string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if value != "" && strings.Contains(diagnostic, value) {
			t.Fatal("credential lifecycle diagnostic disclosed protected material")
		}
	}
}

func TestCredentialExpiryOutputRedactsBeforeBounding(t *testing.T) {
	const protected = "fixture-protected-LEAK!"
	output := []byte(protected + strings.Repeat("x", (16<<10)-5))

	diagnostic := redactCredentialExpiryOutput(output, protected)
	if strings.Contains(diagnostic, "LEAK!") {
		t.Fatal("bounded fixture diagnostic disclosed a credential fragment")
	}
}

func startCredentialExpiryKafka(t *testing.T, ctx context.Context) *credentialExpiryBroker {
	t.Helper()

	port := reserveCredentialExpiryPort(t)
	container := fmt.Sprintf("golib-kafkaservice-oauth-expiry-%d-%d", os.Getpid(), time.Now().UnixNano())
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	start := exec.CommandContext(
		ctx,
		"docker",
		"run", "--detach", "--user", "0", "--name", container,
		"--publish", "127.0.0.1:"+strconv.Itoa(port)+":9094",
		"--entrypoint", "sh",
		credentialExpiryKafkaImage,
		"-c", "while [ ! -f /tmp/golib-kafkaservice-oauth-ready ]; do sleep 0.05; done; exec /bin/bash /tmp/golib-kafkaservice-oauth-start.sh",
	)
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("create secured Apache Kafka fixture: %s", redactCredentialExpiryOutput(output, endpoint, "localhost:19092", credentialExpiryInternalHost))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if output, err := exec.CommandContext(cleanupCtx, "docker", "rm", "--force", container).CombinedOutput(); err != nil {
			t.Errorf("remove secured Apache Kafka fixture: %s", redactCredentialExpiryOutput(output, endpoint, "localhost:19092", credentialExpiryInternalHost))
		}
	})

	pki := newCredentialExpiryPKI(t)
	signingKey := newCredentialExpiryRSAKey(t)
	storePassword := randomCredentialExpiryValue(t)
	fixtureDir := t.TempDir()
	writeCredentialExpiryFixture(t, fixtureDir, "ca.pem", pki.caPEM, 0o644)
	writeCredentialExpiryFixture(t, fixtureDir, "server.pem", pki.serverPEM, 0o644)
	writeCredentialExpiryFixture(t, fixtureDir, "server-key.pem", pki.serverKeyPEM, 0o600)
	writeCredentialExpiryFixture(t, fixtureDir, "store.properties", []byte("password="+storePassword+"\n"), 0o600)
	writeCredentialExpiryFixture(t, fixtureDir, "jwks.json", credentialExpiryJWKS(t, &signingKey.PublicKey), 0o644)
	writeCredentialExpiryFixture(t, fixtureDir, "server.properties", []byte(credentialExpiryKafkaProperties(endpoint)), 0o644)
	writeCredentialExpiryFixture(t, fixtureDir, "start.sh", []byte(credentialExpiryKafkaStartScript()), 0o755)
	for _, name := range []string{"ca.pem", "server.pem", "server-key.pem", "store.properties", "jwks.json", "server.properties"} {
		copyCredentialExpiryFixture(t, ctx, fixtureDir, name, container, "/tmp/"+name, storePassword, endpoint)
	}
	copyCredentialExpiryFixture(t, ctx, fixtureDir, "start.sh", container, "/tmp/golib-kafkaservice-oauth-start.sh", storePassword, endpoint)
	copyCredentialExpiryFixture(t, ctx, fixtureDir, "ca.pem", container, "/tmp/golib-kafkaservice-oauth-ready", storePassword, endpoint)
	waitForCredentialExpiryKafka(t, ctx, container, storePassword, endpoint)
	version := credentialExpiryDockerExec(t, ctx, container, storePassword, endpoint,
		"/opt/kafka/bin/kafka-broker-api-versions.sh", "--version")
	if fields := strings.Fields(version); len(fields) == 0 || fields[0] != "4.3.1" {
		t.Fatal("secured Apache Kafka fixture runtime version is not 4.3.1")
	}

	return &credentialExpiryBroker{
		endpoint: endpoint, container: container, tlsConfig: pki.tlsConfig(), signingKey: signingKey,
	}
}

type credentialExpiryPKI struct {
	caPEM        []byte
	serverPEM    []byte
	serverKeyPEM []byte
	roots        *x509.CertPool
}

func newCredentialExpiryPKI(t *testing.T) credentialExpiryPKI {
	t.Helper()

	now := time.Now()
	caKey := newCredentialExpiryRSAKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "golib-kafkaservice-test-ca"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("create Kafka fixture CA")
	}
	serverKey := newCredentialExpiryRSAKey(t)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "golib-kafkaservice-oauth"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("create Kafka fixture server certificate")
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("add Kafka fixture CA root")
	}
	return credentialExpiryPKI{
		caPEM:        caPEM,
		serverPEM:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		serverKeyPEM: credentialExpiryPrivateKeyPEM(t, serverKey),
		roots:        roots,
	}
}

func (pki credentialExpiryPKI) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pki.roots.Clone()}
}

func (broker *credentialExpiryBroker) issueToken(t *testing.T, lifetime time.Duration) ([]byte, time.Time) {
	t.Helper()

	now := time.Now()
	expiresAt := now.Add(lifetime)
	issuedAt := now.Add(-time.Second)
	if !issuedAt.Before(expiresAt) {
		issuedAt = expiresAt.Add(-time.Second)
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": credentialExpiryKeyID})
	if err != nil {
		t.Fatal("encode OAuth JWT header")
	}
	claims, err := json.Marshal(map[string]any{
		"iss": credentialExpiryIssuer, "aud": credentialExpiryAudience, "sub": "golib-kafkaservice-client",
		"iat": issuedAt.Unix(), "exp": expiresAt.Unix(), "jti": randomCredentialExpiryValue(t),
	})
	if err != nil {
		t.Fatal("encode OAuth JWT claims")
	}
	encoded := base64.RawURLEncoding
	signed := encoded.EncodeToString(header) + "." + encoded.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(rand.Reader, broker.signingKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal("sign OAuth JWT")
	}

	return []byte(signed + "." + encoded.EncodeToString(signature)), expiresAt
}

func credentialExpiryJWKS(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": credentialExpiryKeyID, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal("encode OAuth JWKS")
	}

	return encoded
}

func credentialExpiryKafkaProperties(endpoint string) string {
	return "process.roles=broker,controller\n" +
		"node.id=1\n" +
		"controller.quorum.voters=1@localhost:29093\n" +
		"controller.listener.names=CONTROLLER\n" +
		"listeners=SASL_SSL://:9094,INTERNAL://:19092,CONTROLLER://:29093\n" +
		"advertised.listeners=SASL_SSL://" + endpoint + ",INTERNAL://localhost:19092\n" +
		"listener.security.protocol.map=SASL_SSL:SASL_SSL,INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT\n" +
		"inter.broker.listener.name=INTERNAL\n" +
		"log.dirs=/tmp/golib-kafkaservice-oauth-data\n" +
		"num.partitions=1\n" +
		"offsets.topic.replication.factor=1\n" +
		"transaction.state.log.replication.factor=1\n" +
		"transaction.state.log.min.isr=1\n" +
		"group.initial.rebalance.delay.ms=0\n" +
		"auto.create.topics.enable=true\n" +
		"config.providers=file\n" +
		"config.providers.file.class=org.apache.kafka.common.config.provider.FileConfigProvider\n" +
		"ssl.keystore.location=/tmp/server.p12\n" +
		"ssl.keystore.type=PKCS12\n" +
		"ssl.keystore.password=${file:/tmp/store.properties:password}\n" +
		"ssl.key.password=${file:/tmp/store.properties:password}\n" +
		"ssl.enabled.protocols=TLSv1.2,TLSv1.3\n" +
		"ssl.client.auth=none\n" +
		"sasl.enabled.mechanisms=OAUTHBEARER\n" +
		"listener.name.sasl_ssl.oauthbearer.connections.max.reauth.ms=3000\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.jaas.config=org.apache.kafka.common.security.oauthbearer.OAuthBearerLoginModule required;\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.oauthbearer.expected.audience=" + credentialExpiryAudience + "\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.oauthbearer.expected.issuer=" + credentialExpiryIssuer + "\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.oauthbearer.clock.skew.seconds=0\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.oauthbearer.jwks.endpoint.url=file:///tmp/jwks.json\n" +
		"listener.name.sasl_ssl.oauthbearer.sasl.server.callback.handler.class=org.apache.kafka.common.security.oauthbearer.OAuthBearerValidatorCallbackHandler\n"
}

func credentialExpiryKafkaStartScript() string {
	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  chown 1000:1000 /tmp/server-key.pem /tmp/store.properties\n" +
		"  chmod 0600 /tmp/server-key.pem /tmp/store.properties\n" +
		"  exec su appuser -s /bin/bash -c 'exec /bin/bash /tmp/golib-kafkaservice-oauth-start.sh'\n" +
		"fi\n" +
		"umask 077\n" +
		"sed -n 's/^password=//p' /tmp/store.properties > /tmp/store.password\n" +
		"chmod 0600 /tmp/store.password\n" +
		"openssl pkcs12 -export -name broker -inkey /tmp/server-key.pem -in /tmp/server.pem -certfile /tmp/ca.pem -out /tmp/server.p12 -passout file:/tmp/store.password >/dev/null 2>&1\n" +
		"export KAFKA_OPTS=\"-Dorg.apache.kafka.sasl.oauthbearer.allowed.urls=file:///tmp/jwks.json\"\n" +
		"/opt/kafka/bin/kafka-storage.sh format --ignore-formatted --cluster-id " + credentialExpiryKafkaClusterID + " --config /tmp/server.properties >/dev/null\n" +
		"exec /opt/kafka/bin/kafka-server-start.sh /tmp/server.properties\n"
}

func waitForCredentialExpiryKafka(t *testing.T, ctx context.Context, container, secret, endpoint string) {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, err := exec.CommandContext(ctx, "docker", "exec", container,
			"/opt/kafka/bin/kafka-broker-api-versions.sh", "--bootstrap-server", "localhost:19092").CombinedOutput()
		if err == nil {
			return
		}
		state, stateErr := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", container).CombinedOutput()
		if stateErr == nil && strings.TrimSpace(string(state)) != "true" {
			logs, _ := exec.CommandContext(ctx, "docker", "logs", container).CombinedOutput()
			t.Fatalf("secured Apache Kafka fixture exited: %s", redactCredentialExpiryOutput(logs, secret, endpoint, "localhost:19092", credentialExpiryInternalHost))
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for secured Apache Kafka fixture: %s", redactCredentialExpiryOutput(output, secret, endpoint, "localhost:19092", credentialExpiryInternalHost))
		case <-ticker.C:
		}
	}
}

func reserveCredentialExpiryPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve secured Apache Kafka port")
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

func writeCredentialExpiryFixture(t *testing.T, directory string, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), data, mode); err != nil {
		t.Fatal("write secured Apache Kafka fixture")
	}
}

func credentialExpiryDockerExec(t *testing.T, ctx context.Context, container, secret, endpoint string, args ...string) string {
	t.Helper()
	command := append([]string{"exec", container}, args...)
	output, err := exec.CommandContext(ctx, "docker", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("execute secured Apache Kafka fixture command: %s", redactCredentialExpiryOutput(output, secret, endpoint, "localhost:19092", credentialExpiryInternalHost))
	}

	return strings.TrimSpace(string(output))
}

func copyCredentialExpiryFixture(t *testing.T, ctx context.Context, directory, name, container, destination, secret, endpoint string) {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", "cp", filepath.Join(directory, name), container+":"+destination).CombinedOutput()
	if err != nil {
		t.Fatalf("copy secured Apache Kafka fixture: %s", redactCredentialExpiryOutput(output, secret, endpoint, "localhost:19092", credentialExpiryInternalHost))
	}
}

func newCredentialExpiryRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate secured Kafka RSA key")
	}

	return key
}

func credentialExpiryPrivateKeyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal("encode secured Kafka private key")
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}

func randomCredentialExpiryValue(t *testing.T) string {
	t.Helper()
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatal("generate secured Kafka random value")
	}

	return base64.RawURLEncoding.EncodeToString(value)
}

func redactCredentialExpiryOutput(output []byte, secrets ...string) string {
	const maximum = 16 << 10
	diagnostic := string(output)
	for _, secret := range secrets {
		if secret != "" {
			diagnostic = strings.ReplaceAll(diagnostic, secret, "[redacted]")
		}
	}
	if len(diagnostic) > maximum {
		diagnostic = diagnostic[len(diagnostic)-maximum:]
	}

	return strings.TrimSpace(diagnostic)
}
