// Package mskiam provides bounded Amazon MSK IAM authentication for kafka.
package mskiam

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	kafka "github.com/faustbrian/golib/pkg/kafka"
)

const (
	defaultTokenTimeout = 5 * time.Second
	minTokenTimeout     = 100 * time.Millisecond
	maxTokenTimeout     = time.Minute
	minTokenValidity    = 30 * time.Second
	maxTokenLifetime    = 20 * time.Minute
	maxRegionBytes      = 64
	maxTokenBytes       = 1 << 20
)

var (
	// ErrInvalidConfig reports an invalid or unbounded adapter configuration.
	ErrInvalidConfig = errors.New("kafka/mskiam: configuration is invalid")
	// ErrContextRequired reports a nil construction or token context.
	ErrContextRequired = errors.New("kafka/mskiam: context is required")
	// ErrCredentialLoad reports that the AWS default credential chain could
	// not be constructed.
	ErrCredentialLoad = errors.New(
		"kafka/mskiam: AWS credential chain could not be loaded",
	)
	// ErrTokenGeneration reports a signer failure.
	ErrTokenGeneration = errors.New(
		"kafka/mskiam: token generation failed",
	)
	// ErrCredentialRetrieve reports that the selected provider could not
	// return credentials for one token.
	ErrCredentialRetrieve = errors.New(
		"kafka/mskiam: AWS credentials could not be retrieved",
	)
	// ErrInvalidCredentials reports an empty or structurally invalid AWS
	// credential result.
	ErrInvalidCredentials = errors.New(
		"kafka/mskiam: AWS credential provider returned invalid credentials",
	)
	// ErrExpiringCredentials reports credentials too close to expiry for a
	// new MSK authentication session.
	ErrExpiringCredentials = errors.New(
		"kafka/mskiam: AWS credentials expire too soon",
	)
	// ErrTokenProviderPanic reports a contained signer or provider panic.
	ErrTokenProviderPanic = errors.New(
		"kafka/mskiam: token provider panicked",
	)
	// ErrInvalidToken reports an empty, malformed, expired, or unexpectedly
	// long-lived signer result.
	ErrInvalidToken = errors.New("kafka/mskiam: signer returned an invalid token")
)

// Config selects one AWS region, credential source, and bounded token
// generation deadline. A nil CredentialsProvider selects the standard AWS SDK
// default credential chain. A non-nil provider remains caller-owned and must
// be concurrency-safe, honor context cancellation, and rotate credentials.
type Config struct {
	Region              string
	CredentialsProvider aws.CredentialsProvider
	TokenTimeout        time.Duration
}

// Validate checks the complete configuration without loading credentials.
func (config Config) Validate() error {
	if !validRegion(config.Region) ||
		(config.TokenTimeout != 0 &&
			(config.TokenTimeout < minTokenTimeout ||
				config.TokenTimeout > maxTokenTimeout)) ||
		(config.CredentialsProvider != nil &&
			nilInterface(config.CredentialsProvider)) {
		return ErrInvalidConfig
	}

	return nil
}

// String returns a stable redacted representation.
func (config Config) String() string {
	return "mskiam.Config{region:" + config.Region + ",credentials:redacted}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (config Config) GoString() string {
	return config.String()
}

// Provider generates AWS-signed SASL/OAUTHBEARER tokens. It starts no
// goroutines and is safe for concurrent use when its credential provider is.
type Provider struct {
	region      string
	credentials aws.CredentialsProvider
	timeout     time.Duration
	generator   tokenGenerator
	now         func() time.Time
}

// New validates configuration before loading the default AWS credential chain.
// It creates no Kafka client, connection, or background goroutine.
func New(ctx context.Context, adapterConfig Config) (*Provider, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if err := adapterConfig.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credentials := adapterConfig.CredentialsProvider
	if credentials == nil {
		awsConfig, err := config.LoadDefaultConfig(
			ctx,
			config.WithRegion(adapterConfig.Region),
		)
		if err != nil {
			return nil, newProviderError(ErrCredentialLoad, err)
		}
		credentials = awsConfig.Credentials
	}
	timeout := adapterConfig.TokenTimeout
	if timeout == 0 {
		timeout = defaultTokenTimeout
	}

	return &Provider{
		region:      adapterConfig.Region,
		credentials: credentials,
		timeout:     timeout,
		generator:   awsTokenGenerator{},
		now:         time.Now,
	}, nil
}

// String returns a stable redacted representation.
func (provider *Provider) String() string {
	if provider == nil {
		return "mskiam.Provider{nil}"
	}

	return "mskiam.Provider{region:" + provider.region + ",credentials:redacted}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (provider *Provider) GoString() string {
	return provider.String()
}

// Token generates one owned Amazon MSK IAM token. Its effective expiry is the
// earlier of the signer expiry and the selected AWS credential expiry.
func (provider *Provider) Token(
	ctx context.Context,
) (token kafka.OAuthBearerToken, err error) {
	if ctx == nil {
		return kafka.OAuthBearerToken{}, ErrContextRequired
	}
	if provider == nil ||
		!validRegion(provider.region) ||
		nilInterface(provider.credentials) ||
		provider.timeout < minTokenTimeout ||
		provider.timeout > maxTokenTimeout ||
		provider.generator == nil ||
		provider.now == nil {
		return kafka.OAuthBearerToken{}, ErrInvalidConfig
	}
	if err := ctx.Err(); err != nil {
		return kafka.OAuthBearerToken{}, err
	}
	defer func() {
		if recover() != nil {
			token = kafka.OAuthBearerToken{}
			err = newProviderError(
				ErrTokenProviderPanic,
				ErrTokenProviderPanic,
			)
		}
	}()

	tokenCtx, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	now := provider.now()
	credentials, credentialErr := provider.retrieveCredentials(tokenCtx, now)
	if credentialErr != nil {
		return kafka.OAuthBearerToken{}, credentialErr
	}
	value, expiresAtMilliseconds, generationErr := provider.generator.generate(
		tokenCtx,
		provider.region,
		credentials,
	)
	if generationErr != nil {
		return kafka.OAuthBearerToken{},
			newProviderError(ErrTokenGeneration, generationErr)
	}
	validatedAt := provider.now()
	expiresAt := time.UnixMilli(expiresAtMilliseconds)
	if credentials.CanExpire && credentials.Expires.Before(expiresAt) {
		expiresAt = credentials.Expires
	}
	if !validToken(value) ||
		!expiresAt.After(validatedAt.Add(minTokenValidity)) ||
		expiresAt.After(validatedAt.Add(maxTokenLifetime)) {
		return kafka.OAuthBearerToken{}, ErrInvalidToken
	}

	return kafka.OAuthBearerToken{
		Token:     append([]byte(nil), value...),
		ExpiresAt: expiresAt,
	}, nil
}

func (provider *Provider) retrieveCredentials(
	ctx context.Context,
	now time.Time,
) (aws.Credentials, error) {
	credentials, err := provider.credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{},
			newProviderError(ErrCredentialRetrieve, err)
	}
	if !validCredentials(credentials) {
		return aws.Credentials{}, ErrInvalidCredentials
	}
	if !credentials.CanExpire ||
		credentials.Expires.After(now.Add(minTokenValidity)) {
		return credentials, nil
	}
	invalidator, ok := provider.credentials.(interface{ Invalidate() })
	if !ok {
		return aws.Credentials{}, ErrExpiringCredentials
	}
	invalidator.Invalidate()
	credentials, err = provider.credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{},
			newProviderError(ErrCredentialRetrieve, err)
	}
	if !validCredentials(credentials) {
		return aws.Credentials{}, ErrInvalidCredentials
	}
	if credentials.CanExpire &&
		!credentials.Expires.After(now.Add(minTokenValidity)) {
		return aws.Credentials{}, ErrExpiringCredentials
	}

	return credentials, nil
}

func validCredentials(credentials aws.Credentials) bool {
	return credentials.AccessKeyID != "" &&
		credentials.SecretAccessKey != "" &&
		(!credentials.CanExpire || !credentials.Expires.IsZero())
}

type tokenGenerator interface {
	generate(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error)
}

type awsTokenGenerator struct{}

func (awsTokenGenerator) generate(
	ctx context.Context,
	region string,
	credentials aws.Credentials,
) (string, int64, error) {
	return signer.GenerateAuthTokenFromCredentialsProvider(
		ctx,
		region,
		resolvedCredentialsProvider{credentials: credentials},
	)
}

type resolvedCredentialsProvider struct {
	credentials aws.Credentials
}

func (provider resolvedCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	return provider.credentials, nil
}

// ProviderError preserves a stable category and safe context cancellation
// identity while returning a redacted diagnostic.
type ProviderError struct {
	category error
	cause    error
}

// Error implements error without exposing AWS provider or signer diagnostics.
func (err *ProviderError) Error() string {
	if err == nil || err.category == nil {
		return ErrTokenGeneration.Error()
	}

	return err.category.Error()
}

// GoString returns the same redacted diagnostic for %#v formatting.
func (err *ProviderError) GoString() string {
	return err.Error()
}

// Unwrap preserves the stable category and any safe context cancellation.
func (err *ProviderError) Unwrap() []error {
	if err == nil || err.category == nil {
		return []error{ErrTokenGeneration}
	}
	if err.cause == nil {
		return []error{err.category}
	}

	return []error{err.category, err.cause}
}

func newProviderError(category error, cause error) error {
	var safeCause error
	switch {
	case errors.Is(cause, context.Canceled):
		safeCause = context.Canceled
	case errors.Is(cause, context.DeadlineExceeded):
		safeCause = context.DeadlineExceeded
	}

	return &ProviderError{category: category, cause: safeCause}
}

func validRegion(region string) bool {
	if len(region) == 0 {
		return false
	}
	if len(region) > maxRegionBytes {
		return false
	}
	if !utf8.ValidString(region) {
		return false
	}
	if strings.TrimSpace(region) != region {
		return false
	}
	segments := strings.Split(region, "-")
	if len(segments) < 3 ||
		len(segments[0]) != 2 ||
		!lowercaseLetters(segments[0]) ||
		!decimalDigits(segments[len(segments)-1]) ||
		segments[len(segments)-1][0] == '0' {
		return false
	}
	for _, segment := range segments[1 : len(segments)-1] {
		if segment == "" {
			return false
		}
		for _, current := range segment {
			if (current < 'a' || current > 'z') &&
				(current < '0' || current > '9') {
				return false
			}
		}
	}

	return true
}

func lowercaseLetters(value string) bool {
	for _, current := range value {
		if current < 'a' || current > 'z' {
			return false
		}
	}

	return true
}

func decimalDigits(value string) bool {
	for _, current := range value {
		if current < '0' || current > '9' {
			return false
		}
	}

	return value != ""
}

func validToken(token string) bool {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(
		strings.TrimRight(token, "="),
	)

	return err == nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
