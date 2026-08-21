//go:build interoperability

package kafka_test

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	franzoauth "github.com/twmb/franz-go/pkg/sasl/oauth"
)

var errSecureOAuthTokenEndpoint = errors.New(
	"secured OAuth token endpoint request failed",
)

const secureOAuthTokenResponseBytes = 1<<20 + 4<<10

type secureOAuthTokenEndpointProvider struct {
	client       *http.Client
	endpoint     string
	clientID     string
	clientSecret []byte
	scope        string
}

func (provider secureOAuthTokenEndpointProvider) Token(
	ctx context.Context,
) (kafka.OAuthBearerToken, error) {
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {provider.scope},
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		provider.endpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return kafka.OAuthBearerToken{}, errSecureOAuthTokenEndpoint
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(provider.clientID, string(provider.clientSecret))
	response, err := provider.client.Do(request)
	if err != nil {
		return kafka.OAuthBearerToken{}, errors.Join(
			errSecureOAuthTokenEndpoint,
			err,
		)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return kafka.OAuthBearerToken{}, errSecureOAuthTokenEndpoint
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return kafka.OAuthBearerToken{}, errSecureOAuthTokenEndpoint
	}
	body, err := io.ReadAll(io.LimitReader(
		response.Body,
		secureOAuthTokenResponseBytes+1,
	))
	if err != nil || len(body) > secureOAuthTokenResponseBytes {
		return kafka.OAuthBearerToken{}, errSecureOAuthTokenEndpoint
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &tokenResponse) != nil ||
		!strings.EqualFold(tokenResponse.TokenType, "bearer") ||
		tokenResponse.ExpiresIn <= 0 || tokenResponse.ExpiresIn > 3600 {
		return kafka.OAuthBearerToken{}, errSecureOAuthTokenEndpoint
	}

	return kafka.OAuthBearerToken{
		Token:     []byte(tokenResponse.AccessToken),
		ExpiresAt: time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second),
	}, nil
}

func TestApacheKafkaExternalOAuthTokenEndpointCompatibility(t *testing.T) {
	runExclusiveKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker := startSecureKafkaBroker(t, ctx, secureKafkaOAuth)
	broker.assertRuntimeVersions(t, ctx)
	topic := fmt.Sprintf("golib-oauth-token-endpoint-%d", time.Now().UnixNano())
	adminToken, _ := broker.issueOAuthToken(t, secureKafkaAudience, 2*time.Minute)
	createSecureKafkaTopic(
		t,
		ctx,
		broker.endpoint,
		broker.serverTLSConfig(),
		franzoauth.Auth{Token: string(adminToken)}.AsMechanism(),
		topic,
	)

	const (
		clientID     = "golib-oauth-client"
		clientSecret = "golib-oauth-client-secret"
		scope        = "golib-kafka"
	)
	type tokenSnapshot struct {
		accessToken []byte
		generation  int64
	}
	initialToken, _ := broker.issueOAuthToken(
		t,
		secureKafkaAudience,
		2*time.Minute,
	)
	var currentToken atomic.Pointer[tokenSnapshot]
	currentToken.Store(&tokenSnapshot{
		accessToken: append([]byte(nil), initialToken...),
		generation:  1,
	})
	var (
		requestCount     atomic.Int64
		servedGeneration atomic.Int64
		blockRequests    atomic.Bool
		canceledRequests atomic.Int64
	)
	tokenServer := httptest.NewUnstartedServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodPost ||
			(request.URL.Path != "/oauth/token" && request.URL.Path != "/slow") {
			http.NotFound(writer, request)

			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			http.Error(writer, "invalid request", http.StatusBadRequest)

			return
		}
		providedID, providedSecret, ok := request.BasicAuth()
		if !ok || subtle.ConstantTimeCompare(
			[]byte(providedID),
			[]byte(clientID),
		) != 1 || subtle.ConstantTimeCompare(
			[]byte(providedSecret),
			[]byte(clientSecret),
		) != 1 {
			writer.WriteHeader(http.StatusUnauthorized)

			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 4<<10)
		if err := request.ParseForm(); err != nil ||
			len(request.PostForm) != 2 ||
			request.PostForm.Get("grant_type") != "client_credentials" ||
			request.PostForm.Get("scope") != scope {
			http.Error(writer, "invalid request", http.StatusBadRequest)

			return
		}
		requestCount.Add(1)
		if request.URL.Path == "/slow" && blockRequests.Load() {
			<-request.Context().Done()
			canceledRequests.Add(1)

			return
		}
		snapshot := currentToken.Load()
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Pragma", "no-cache")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"access_token": string(snapshot.accessToken),
			"token_type":   "Bearer",
			"expires_in":   120,
		}); err != nil {
			return
		}
		servedGeneration.Store(snapshot.generation)
	}))
	tokenServer.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	tokenServer.StartTLS()
	t.Cleanup(tokenServer.Close)
	tokenClient := tokenServer.Client()
	tokenClient.Timeout = 2 * time.Second
	t.Cleanup(tokenClient.CloseIdleConnections)
	provider := secureOAuthTokenEndpointProvider{
		client:       tokenClient,
		endpoint:     tokenServer.URL + "/oauth/token",
		clientID:     clientID,
		clientSecret: []byte(clientSecret),
		scope:        scope,
	}

	untrustedProvider := provider
	untrustedProvider.client = &http.Client{Timeout: time.Second}
	if _, err := untrustedProvider.Token(ctx); !errors.Is(
		err,
		errSecureOAuthTokenEndpoint,
	) || strings.Contains(err.Error(), clientSecret) {
		t.Fatalf("untrusted token endpoint error = %v", err)
	}

	blockRequests.Store(true)
	slowProvider := provider
	slowProvider.endpoint = tokenServer.URL + "/slow"
	slowCtx, cancelSlow := context.WithTimeout(ctx, 150*time.Millisecond)
	_, slowErr := slowProvider.Token(slowCtx)
	cancelSlow()
	blockRequests.Store(false)
	if !errors.Is(slowErr, context.DeadlineExceeded) ||
		strings.Contains(slowErr.Error(), clientSecret) {
		t.Fatalf("canceled token endpoint error = %v", slowErr)
	}
	waitForSecureOAuthCanceledRequest(t, ctx, &canceledRequests)

	security := kafka.ClientSecurity{
		TLS: broker.serverTLSConfig(),
		Authentication: kafka.NewOAuthBearerAuthentication(
			provider,
		),
		CredentialTimeout: time.Second,
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{broker.endpoint},
		ClientID:        "golib-oauth-token-endpoint-producer",
		AllowedTopics:   []string{topic},
		DeliveryTimeout: 3 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 4 * time.Second,
		Security:        security,
	})
	if err != nil {
		t.Fatalf("construct external OAuth producer: %v", err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	expectedValues := []string{"before-token-refresh"}
	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte(expectedValues[0]),
		Value: []byte(expectedValues[0]),
	})
	if result.Err != nil || servedGeneration.Load() != 1 {
		t.Fatalf(
			"initial external OAuth delivery/generation = %v/%d",
			result.Err,
			servedGeneration.Load(),
		)
	}

	rotatedToken, _ := broker.issueOAuthToken(
		t,
		secureKafkaAudience,
		2*time.Minute,
	)
	currentToken.Store(&tokenSnapshot{
		accessToken: append([]byte(nil), rotatedToken...),
		generation:  2,
	})
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, 15*time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	var lastRefreshErr error
	for attempt := 1; ; attempt++ {
		select {
		case <-refreshCtx.Done():
			ticker.Stop()
			cancelRefresh()
			t.Fatalf(
				"external OAuth token did not refresh after %d requests: %v",
				requestCount.Load(),
				lastRefreshErr,
			)
		case <-ticker.C:
		}
		value := fmt.Sprintf("after-token-refresh-%d", attempt)
		result = producer.PublishRecord(refreshCtx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte(value),
			Value: []byte(value),
		})
		lastRefreshErr = result.Err
		if result.Err == nil {
			expectedValues = append(expectedValues, value)
		}
		if result.Err == nil && servedGeneration.Load() >= 2 {
			break
		}
	}
	ticker.Stop()
	cancelRefresh()
	if err := producer.Close(); err != nil {
		t.Fatalf("close external OAuth producer: %v", err)
	}

	values := consumeSecureRecords(
		t,
		ctx,
		broker.endpoint,
		topic,
		"golib-oauth-token-endpoint-group",
		len(expectedValues),
		security,
	)
	if len(values) != len(expectedValues) {
		t.Fatalf("external OAuth values = %d, want %d", len(values), len(expectedValues))
	}
	for index := range expectedValues {
		if values[index] != expectedValues[index] {
			t.Fatalf(
				"external OAuth value %d = %q, want %q",
				index,
				values[index],
				expectedValues[index],
			)
		}
	}
}

func waitForSecureOAuthCanceledRequest(
	t *testing.T,
	ctx context.Context,
	canceledRequests *atomic.Int64,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for canceledRequests.Load() == 0 {
		select {
		case <-waitCtx.Done():
			t.Fatalf("OAuth endpoint did not observe cancellation: %v", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
