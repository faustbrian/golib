package mskiam_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

func benchmarkProvider(b *testing.B) *mskiam.Provider {
	b.Helper()
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: staticCredentialsProvider{},
		TokenTimeout:        time.Second,
	})
	if err != nil {
		b.Fatalf("new provider: %v", err)
	}

	return provider
}

func BenchmarkTokenGeneration(b *testing.B) {
	provider := benchmarkProvider(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, tokenErr := provider.Token(ctx); tokenErr != nil {
			b.Fatalf("generate token: %v", tokenErr)
		}
	}
}

func BenchmarkTokenContention(b *testing.B) {
	provider := benchmarkProvider(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if _, err := provider.Token(ctx); err != nil {
				b.Errorf("generate contended token: %v", err)
			}
		}
	})
}

type benchmarkHTTPProvider struct {
	client   *http.Client
	endpoint string
}

func (provider benchmarkHTTPProvider) Retrieve(
	ctx context.Context,
) (aws.Credentials, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.endpoint, nil)
	if err != nil {
		return aws.Credentials{}, err
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return aws.Credentials{}, err
	}
	var credentials struct {
		AccessKeyID     string
		SecretAccessKey string
		SessionToken    string
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&credentials)
	closeErr := response.Body.Close()
	if decodeErr != nil {
		return aws.Credentials{}, decodeErr
	}
	if closeErr != nil {
		return aws.Credentials{}, closeErr
	}

	return aws.Credentials{
		AccessKeyID:     credentials.AccessKeyID,
		SecretAccessKey: credentials.SecretAccessKey,
		SessionToken:    credentials.SessionToken,
	}, nil
}

func BenchmarkExternalCredentialRetrieval(b *testing.B) {
	digest := sha256.Sum256([]byte(b.Name()))
	encoded := hex.EncodeToString(digest[:])
	credentials := aws.Credentials{
		AccessKeyID:     "AKIA" + encoded[:16],
		SecretAccessKey: encoded[16:56],
		SessionToken:    "session-" + encoded[56:],
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		if err := json.NewEncoder(writer).Encode(credentials); err != nil {
			b.Errorf("encode generated credentials: %v", err)
		}
	}))
	b.Cleanup(server.Close)
	provider := benchmarkHTTPProvider{
		client:   server.Client(),
		endpoint: server.URL,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := provider.Retrieve(ctx); err != nil {
			b.Fatalf("retrieve external credentials: %v", err)
		}
	}
}
