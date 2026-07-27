package mskiam_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

type pointerCredentialsProvider struct{}

func (*pointerCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	return aws.Credentials{}, nil
}

func TestConfigRejectsNoncanonicalAWSRegion(t *testing.T) {
	t.Parallel()

	err := (mskiam.Config{Region: "123"}).Validate()
	if !errors.Is(err, mskiam.ErrInvalidConfig) {
		t.Fatalf("validate noncanonical region: %v", err)
	}
}

func TestConfigValidatesRegionProviderAndTimeoutBounds(t *testing.T) {
	t.Parallel()

	validRegions := []string{
		"eu-north-1",
		"us-gov-east-1",
		"us-iso-east-1",
		"ap-southeast-7",
	}
	for _, region := range validRegions {
		config := mskiam.Config{Region: region}
		if err := config.Validate(); err != nil {
			t.Fatalf("validate region %q: %v", region, err)
		}
	}

	invalidRegions := []string{
		"",
		"EU-NORTH-1",
		" eu-north-1",
		"eu-north-1 ",
		"e-north-1",
		"eur-north-1",
		"eu--north-1",
		"eu_north_1",
		"eu-no_th-1",
		"eu-north",
		"eu-north-zero",
		"eu-north-0",
		string([]byte{'e', 'u', '-', 0xff, '-', '1'}),
		strings.Repeat("e", 65),
	}
	for _, region := range invalidRegions {
		config := mskiam.Config{Region: region}
		if !errors.Is(config.Validate(), mskiam.ErrInvalidConfig) {
			t.Fatalf("accepted invalid region %q", region)
		}
	}

	validTimeouts := []time.Duration{
		0,
		100 * time.Millisecond,
		time.Second,
		time.Minute,
	}
	for _, timeout := range validTimeouts {
		config := mskiam.Config{
			Region:       "eu-north-1",
			TokenTimeout: timeout,
		}
		if err := config.Validate(); err != nil {
			t.Fatalf("validate timeout %s: %v", timeout, err)
		}
	}
	invalidTimeouts := []time.Duration{
		-time.Second,
		time.Millisecond,
		time.Minute + time.Nanosecond,
	}
	for _, timeout := range invalidTimeouts {
		config := mskiam.Config{
			Region:       "eu-north-1",
			TokenTimeout: timeout,
		}
		if !errors.Is(config.Validate(), mskiam.ErrInvalidConfig) {
			t.Fatalf("accepted invalid timeout %s", timeout)
		}
	}

	var typedNil *pointerCredentialsProvider
	if !errors.Is((mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: typedNil,
	}).Validate(), mskiam.ErrInvalidConfig) {
		t.Fatal("accepted typed-nil credentials provider")
	}
}

func TestNewValidatesBeforeLoadingCredentials(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if _, err := mskiam.New(nilContext, mskiam.Config{
		Region: "eu-north-1",
	}); !errors.Is(err, mskiam.ErrContextRequired) {
		t.Fatalf("nil context: %v", err)
	}
	if _, err := mskiam.New(context.Background(), mskiam.Config{
		Region: "invalid",
	}); !errors.Is(err, mskiam.ErrInvalidConfig) {
		t.Fatalf("invalid config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mskiam.New(ctx, mskiam.Config{
		Region: "eu-north-1",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled construction: %v", err)
	}
}

func TestNewUsesAWSDefaultCredentialChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "test-session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region: "eu-north-1",
	})
	if err != nil {
		t.Fatalf("load default chain: %v", err)
	}
	token, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("generate default-chain token: %v", err)
	}
	if len(token.Token) == 0 {
		t.Fatal("default chain returned an empty token")
	}
}

func TestNewRedactsDefaultCredentialLoadFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(
		configPath,
		[]byte("[profile broken\nsecret-content"),
		0o600,
	); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SDK_LOAD_CONFIG", "true")
	t.Setenv("AWS_PROFILE", "broken")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := mskiam.New(context.Background(), mskiam.Config{
		Region: "eu-north-1",
	})
	if !errors.Is(err, mskiam.ErrCredentialLoad) ||
		strings.Contains(err.Error(), "secret-content") {
		t.Fatalf("unredacted credential load failure: %v", err)
	}
}

func TestConfigurationAndProviderFormattingIsRedacted(t *testing.T) {
	t.Parallel()

	config := mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: staticCredentialsProvider{},
	}
	if strings.Contains(config.String(), "access-key") ||
		config.GoString() != config.String() {
		t.Fatalf("unredacted config: %s", config.String())
	}
	provider, err := mskiam.New(context.Background(), config)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if strings.Contains(provider.String(), "access-key") ||
		provider.GoString() != provider.String() {
		t.Fatalf("unredacted provider: %s", provider.String())
	}
	var nilProvider *mskiam.Provider
	if nilProvider.String() != "mskiam.Provider{nil}" {
		t.Fatalf("nil provider formatting: %s", nilProvider.String())
	}
}
