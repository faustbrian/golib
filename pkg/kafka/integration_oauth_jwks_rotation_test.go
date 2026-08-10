//go:build interoperability

package kafka_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/testcontainers/testcontainers-go"
	"github.com/twmb/franz-go/pkg/kerr"
	franzoauth "github.com/twmb/franz-go/pkg/sasl/oauth"
)

func TestApacheKafkaHTTPSJWKSRotationCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const (
		initialKeyID = "golib-jwks-initial"
		rotatedKeyID = "golib-jwks-rotated"
	)
	initialKey := newSecureKafkaRSAKey(t)
	rotatedKey := newSecureKafkaRSAKey(t)
	type jwksSnapshot struct {
		body       []byte
		generation int64
	}
	var (
		currentJWKS      atomic.Pointer[jwksSnapshot]
		servedGeneration atomic.Int64
		requestCount     atomic.Int64
	)
	currentJWKS.Store(&jwksSnapshot{
		body: secureKafkaJWKSForKeys(t, []secureKafkaJWK{{
			keyID: initialKeyID,
			key:   &initialKey.PublicKey,
		}}),
		generation: 1,
	})
	jwksPKI := newSecureKafkaPKI(t, testcontainers.HostInternal)
	jwksCertificate, err := tls.X509KeyPair(
		jwksPKI.serverPEM,
		jwksPKI.serverKeyPEM,
	)
	if err != nil {
		t.Fatalf("parse HTTPS JWKS server identity: %v", err)
	}
	jwksServer := httptest.NewUnstartedServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodGet || request.URL.Path != "/jwks" {
			http.NotFound(writer, request)

			return
		}
		snapshot := currentJWKS.Load()
		requestCount.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		if _, writeErr := writer.Write(snapshot.body); writeErr != nil {
			return
		}
		servedGeneration.Store(snapshot.generation)
	}))
	jwksServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{jwksCertificate},
	}
	jwksServer.StartTLS()
	t.Cleanup(jwksServer.Close)
	jwksPort := jwksServer.Listener.Addr().(*net.TCPAddr).Port
	jwksURL := fmt.Sprintf(
		"https://%s:%d/jwks",
		testcontainers.HostInternal,
		jwksPort,
	)

	broker := startSecureKafkaBrokerWithOptions(
		t,
		ctx,
		secureKafkaOAuth,
		secureKafkaBrokerOptions{
			oauthJWKSURL:      jwksURL,
			oauthJWKSTrustPEM: append([]byte(nil), jwksPKI.caPEM...),
			hostAccessPorts:   []int{jwksPort},
			oauthKey:          initialKey,
		},
	)
	broker.assertRuntimeVersions(t, ctx)
	if requestCount.Load() == 0 || servedGeneration.Load() != 1 {
		t.Fatalf(
			"initial HTTPS JWKS requests/generation = %d/%d",
			requestCount.Load(),
			servedGeneration.Load(),
		)
	}
	topic := fmt.Sprintf("golib-oauth-jwks-rotation-%d", time.Now().UnixNano())
	adminToken, _ := issueSecureKafkaOAuthToken(
		t,
		initialKey,
		initialKeyID,
		secureKafkaIssuer,
		secureKafkaAudience,
		2*time.Minute,
	)
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzoauth.Auth{Token: string(adminToken)}.AsMechanism(),
		topic,
	)

	type oauthCredential struct {
		token      []byte
		expiresAt  time.Time
		generation int64
	}
	initialToken, initialExpiry := issueSecureKafkaOAuthToken(
		t,
		initialKey,
		initialKeyID,
		secureKafkaIssuer,
		secureKafkaAudience,
		2*time.Minute,
	)
	var currentCredential atomic.Pointer[oauthCredential]
	currentCredential.Store(&oauthCredential{
		token:      append([]byte(nil), initialToken...),
		expiresAt:  initialExpiry,
		generation: 1,
	})
	var (
		providerCalls      atomic.Int64
		providerGeneration atomic.Int64
	)
	provider := kafka.OAuthBearerProviderFunc(func(
		context.Context,
	) (kafka.OAuthBearerToken, error) {
		providerCalls.Add(1)
		credential := currentCredential.Load()
		providerGeneration.Store(credential.generation)

		return kafka.OAuthBearerToken{
			Token:     append([]byte(nil), credential.token...),
			ExpiresAt: credential.expiresAt,
		}, nil
	})
	security := kafka.ClientSecurity{
		TLS:               broker.serverTLSConfig(),
		Authentication:    kafka.NewOAuthBearerAuthentication(provider),
		CredentialTimeout: time.Second,
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{broker.endpoint},
		ClientID:        "golib-oauth-jwks-rotation-producer",
		AllowedTopics:   []string{topic},
		DeliveryTimeout: 3 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 4 * time.Second,
		Security:        security,
	})
	if err != nil {
		t.Fatalf("construct HTTPS JWKS rotation producer: %v", err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	expectedValues := []string{"before-signing-key-rollover"}
	initialResult := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte(expectedValues[0]),
		Value: []byte(expectedValues[0]),
	})
	if initialResult.Err != nil || providerCalls.Load() == 0 {
		t.Fatalf(
			"initial HTTPS JWKS delivery/calls = %v/%d",
			initialResult.Err,
			providerCalls.Load(),
		)
	}

	currentJWKS.Store(&jwksSnapshot{
		body: secureKafkaJWKSForKeys(t, []secureKafkaJWK{
			{keyID: initialKeyID, key: &initialKey.PublicKey},
			{keyID: rotatedKeyID, key: &rotatedKey.PublicKey},
		}),
		generation: 2,
	})
	waitForSecureKafkaJWKSGeneration(t, ctx, &servedGeneration, 2)
	rotatedToken, rotatedExpiry := issueSecureKafkaOAuthToken(
		t,
		rotatedKey,
		rotatedKeyID,
		secureKafkaIssuer,
		secureKafkaAudience,
		2*time.Minute,
	)
	currentCredential.Store(&oauthCredential{
		token:      append([]byte(nil), rotatedToken...),
		expiresAt:  rotatedExpiry,
		generation: 2,
	})
	rolloverCtx, cancelRollover := context.WithTimeout(ctx, 15*time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	var lastRolloverErr error
	for attempt := 1; ; attempt++ {
		select {
		case <-rolloverCtx.Done():
			ticker.Stop()
			cancelRollover()
			t.Fatalf(
				"OAuth provider did not use the rotated signing key after %d calls: %v",
				providerCalls.Load(),
				lastRolloverErr,
			)
		case <-ticker.C:
		}
		value := fmt.Sprintf("after-signing-key-rollover-%d", attempt)
		result := producer.PublishRecord(rolloverCtx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		lastRolloverErr = result.Err
		if result.Err == nil {
			expectedValues = append(expectedValues, value)
		}
		if result.Err == nil && providerGeneration.Load() >= 2 {
			break
		}
	}
	ticker.Stop()
	cancelRollover()

	currentJWKS.Store(&jwksSnapshot{
		body: secureKafkaJWKSForKeys(t, []secureKafkaJWK{{
			keyID: rotatedKeyID,
			key:   &rotatedKey.PublicKey,
		}}),
		generation: 3,
	})
	waitForSecureKafkaJWKSGeneration(t, ctx, &servedGeneration, 3)
	retiredSecurity := oauthSecurityForSecureKafkaToken(
		broker,
		initialToken,
		initialExpiry,
	)
	waitForSecureKafkaOAuthAuthenticationFailure(
		t,
		ctx,
		broker.endpoint,
		retiredSecurity,
		string(initialToken),
	)
	if err := producer.Close(); err != nil {
		t.Fatalf("close HTTPS JWKS rotation producer: %v", err)
	}
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-oauth-jwks-rotation-group",
		len(expectedValues),
		security,
	)
	if len(values) != len(expectedValues) {
		t.Fatalf("HTTPS JWKS rotation values = %d, want %d", len(values), len(expectedValues))
	}
	for index := range expectedValues {
		if values[index] != expectedValues[index] {
			t.Fatalf(
				"HTTPS JWKS rotation value %d = %q, want %q",
				index,
				values[index],
				expectedValues[index],
			)
		}
	}
}

func waitForSecureKafkaJWKSGeneration(
	t *testing.T,
	ctx context.Context,
	servedGeneration *atomic.Int64,
	want int64,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if servedGeneration.Load() >= want {
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"HTTPS JWKS served generation = %d, want at least %d: %v",
				servedGeneration.Load(),
				want,
				context.Cause(waitCtx),
			)
		case <-ticker.C:
		}
	}
}

func oauthSecurityForSecureKafkaToken(
	broker *secureKafkaBroker,
	token []byte,
	expiresAt time.Time,
) kafka.ClientSecurity {
	return kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		Authentication: kafka.NewOAuthBearerAuthentication(
			kafka.OAuthBearerProviderFunc(func(
				context.Context,
			) (kafka.OAuthBearerToken, error) {
				return kafka.OAuthBearerToken{
					Token:     append([]byte(nil), token...),
					ExpiresAt: expiresAt,
				}, nil
			}),
		),
		CredentialTimeout: time.Second,
	}
}

func waitForSecureKafkaOAuthAuthenticationFailure(
	t *testing.T,
	ctx context.Context,
	endpoint string,
	security kafka.ClientSecurity,
	secret string,
) {
	t.Helper()

	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("parse retired-key OAuth endpoint: %v", err)
	}
	if host == "localhost" {
		endpoint = net.JoinHostPort("127.0.0.1", port)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for attempt := 1; ; attempt++ {
		inspector, err := kafka.NewInspector(kafka.InspectorConfig{
			Brokers:  []string{endpoint},
			ClientID: fmt.Sprintf("golib-retired-oauth-key-%d", attempt),
			Security: security,
		})
		if err != nil {
			t.Fatalf("construct retired-key OAuth inspector: %v", err)
		}
		healthCtx, cancelHealth := context.WithTimeout(waitCtx, 3*time.Second)
		healthErr := inspector.DependencyHealth(healthCtx)
		cancelHealth()
		if closeErr := inspector.Close(); closeErr != nil {
			t.Fatalf("close retired-key OAuth inspector: %v", closeErr)
		}
		lastErr = healthErr
		if errors.Is(healthErr, kerr.SaslAuthenticationFailed) {
			if strings.Contains(healthErr.Error(), secret) {
				t.Fatal("retired-key OAuth error disclosed its token")
			}

			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"retired OAuth signing key remained accepted: %v; last error: %v",
				context.Cause(waitCtx),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}
