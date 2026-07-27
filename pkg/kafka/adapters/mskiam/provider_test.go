package mskiam_test

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	kafka "github.com/faustbrian/golib/pkg/kafka"
	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

type staticCredentialsProvider struct{}

func (staticCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		SessionToken:    "session-token",
		CanExpire:       true,
		Expires:         time.Now().Add(time.Hour),
	}, nil
}

type rotatingCredentialsProvider struct {
	mutex         sync.Mutex
	now           time.Time
	retrievals    int
	invalidations int
}

func (provider *rotatingCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.retrievals++
	expires := provider.now.Add(10 * time.Second)
	if provider.retrievals > 1 {
		expires = provider.now.Add(5 * time.Minute)
	}

	return aws.Credentials{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		SessionToken:    "session-token",
		CanExpire:       true,
		Expires:         expires,
	}, nil
}

func (provider *rotatingCredentialsProvider) Invalidate() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.invalidations++
}

func (provider *rotatingCredentialsProvider) counts() (int, int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()

	return provider.retrievals, provider.invalidations
}

func TestProviderGeneratesOwnedExpiringMSKIAMToken(t *testing.T) {
	t.Parallel()

	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: staticCredentialsProvider{},
		TokenTimeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	var contract kafka.OAuthBearerProvider = provider
	security := kafka.ClientSecurity{
		Authentication:    kafka.NewOAuthBearerAuthentication(contract),
		CredentialTimeout: time.Second,
	}
	if err := security.Validate(); err != nil {
		t.Fatalf("validate Kafka security policy: %v", err)
	}
	token, err := contract.Token(context.Background())
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if len(token.Token) == 0 || !token.ExpiresAt.After(time.Now()) {
		t.Fatalf("unexpected token metadata: %#v", token)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(
		strings.TrimRight(string(token.Token), "="),
	)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	signedURL, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if signedURL.Host != "kafka.eu-north-1.amazonaws.com" ||
		signedURL.Query().Get("Action") != "kafka-cluster:Connect" ||
		signedURL.Query().Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		t.Fatalf("unexpected signed token")
	}
}

func TestProviderRefreshesExpiringCredentialsAndCapsTokenExpiry(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Millisecond)
	credentials := &rotatingCredentialsProvider{now: now}
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: credentials,
		TokenTimeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	token, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	retrievals, invalidations := credentials.counts()
	if retrievals != 2 || invalidations != 1 {
		t.Fatalf(
			"credential refresh counts = retrievals:%d invalidations:%d",
			retrievals,
			invalidations,
		)
	}
	if want := now.Add(5 * time.Minute); !token.ExpiresAt.Equal(want) {
		t.Fatalf("effective token expiry = %s, want %s", token.ExpiresAt, want)
	}
}
