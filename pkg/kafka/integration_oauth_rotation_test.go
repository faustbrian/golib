//go:build interoperability

package kafka_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	franzoauth "github.com/twmb/franz-go/pkg/sasl/oauth"
)

func TestApacheKafkaOAuthBearerCredentialRotationCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaOAuth)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-oauth-rotation-%d", time.Now().UnixNano())
	adminToken, _ := broker.issueOAuthToken(t, secureKafkaAudience, 2*time.Minute)
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
	initialToken, initialExpiry := broker.issueOAuthToken(
		t,
		secureKafkaAudience,
		2*time.Minute,
	)
	var currentCredential atomic.Pointer[oauthCredential]
	currentCredential.Store(&oauthCredential{
		token:     append([]byte(nil), initialToken...),
		expiresAt: initialExpiry,
	})

	const (
		oauthClientCount   = 3
		oauthRotationCount = 3
	)
	type rotatingOAuthClient struct {
		producer           *kafka.Producer
		providerCalls      atomic.Int64
		observedGeneration atomic.Int64
	}
	clients := make([]*rotatingOAuthClient, 0, oauthClientCount)
	expectedValues := make([]string, 0, oauthClientCount*(oauthRotationCount+1))
	for clientIndex := range oauthClientCount {
		client := &rotatingOAuthClient{}
		provider := kafka.OAuthBearerProviderFunc(func(
			context.Context,
		) (kafka.OAuthBearerToken, error) {
			client.providerCalls.Add(1)
			credential := currentCredential.Load()
			client.observedGeneration.Store(credential.generation)

			return kafka.OAuthBearerToken{
				Token:     append([]byte(nil), credential.token...),
				ExpiresAt: credential.expiresAt,
			}, nil
		})
		producer, err := kafka.NewProducer(kafka.ProducerConfig{
			Brokers:         []string{broker.endpoint},
			ClientID:        fmt.Sprintf("golib-oauth-rotation-producer-%d", clientIndex),
			AllowedTopics:   []string{topic},
			DeliveryTimeout: 3 * time.Second,
			RequestTimeout:  2 * time.Second,
			ShutdownTimeout: 4 * time.Second,
			Security: kafka.ClientSecurity{
				TLS:               broker.serverTLSConfig(),
				Authentication:    kafka.NewOAuthBearerAuthentication(provider),
				CredentialTimeout: time.Second,
			},
		})
		if err != nil {
			t.Fatalf("construct rotating OAuth producer %d: %v", clientIndex, err)
		}
		client.producer = producer
		t.Cleanup(func() { _ = client.producer.Close() })
		value := fmt.Sprintf("client-%d-before-rotation", clientIndex)
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("initial OAuth delivery for client %d: %v", clientIndex, result.Err)
		}
		if client.providerCalls.Load() == 0 {
			t.Fatalf("initial OAuth provider %d was not used", clientIndex)
		}
		clients = append(clients, client)
		expectedValues = append(expectedValues, value)
	}

	for rotation := int64(1); rotation <= oauthRotationCount; rotation++ {
		token, expiresAt := broker.issueOAuthToken(
			t,
			secureKafkaAudience,
			2*time.Minute,
		)
		currentCredential.Store(&oauthCredential{
			token:      append([]byte(nil), token...),
			expiresAt:  expiresAt,
			generation: rotation,
		})

		for clientIndex, client := range clients {
			rotationCtx, cancelRotation := context.WithTimeout(ctx, 15*time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			var lastErr error
			for attempt := 1; ; attempt++ {
				select {
				case <-rotationCtx.Done():
					ticker.Stop()
					cancelRotation()
					t.Fatalf(
						"OAuth provider %d did not observe generation %d after %d calls: %v",
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
				if result.Err == nil &&
					client.observedGeneration.Load() >= rotation {
					break
				}
			}
			ticker.Stop()
			cancelRotation()
		}
	}
	for clientIndex, client := range clients {
		if err := client.producer.Close(); err != nil {
			t.Fatalf("close rotating OAuth producer %d: %v", clientIndex, err)
		}
	}

	consumerProvider := kafka.OAuthBearerProviderFunc(func(
		context.Context,
	) (kafka.OAuthBearerToken, error) {
		credential := currentCredential.Load()

		return kafka.OAuthBearerToken{
			Token:     append([]byte(nil), credential.token...),
			ExpiresAt: credential.expiresAt,
		}, nil
	})
	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-oauth-rotation-group",
		len(expectedValues),
		kafka.ClientSecurity{
			TLS: broker.serverTLSConfig(),
			Authentication: kafka.NewOAuthBearerAuthentication(
				consumerProvider,
			),
			CredentialTimeout: time.Second,
		},
	)
	if len(values) != len(expectedValues) {
		t.Fatalf("OAuth rotation values = %d, want %d", len(values), len(expectedValues))
	}
	for index := range expectedValues {
		if values[index] != expectedValues[index] {
			t.Fatalf(
				"OAuth rotation value %d = %q, want %q",
				index,
				values[index],
				expectedValues[index],
			)
		}
	}
}
