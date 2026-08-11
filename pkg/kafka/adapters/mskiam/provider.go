// Package mskiam provides bounded Amazon MSK IAM authentication for kafka.
package mskiam

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	kafka "github.com/faustbrian/golib/pkg/kafka"
)

const (
	defaultTokenTimeout  = 5 * time.Second
	minTokenTimeout      = 100 * time.Millisecond
	maxTokenTimeout      = time.Minute
	minTokenValidity     = 30 * time.Second
	maxTokenLifetime     = 20 * time.Minute
	maxRegionBytes       = 64
	maxTokenBytes        = 1 << 20
	maxAccessKeyIDBytes  = 128
	maxSecretKeyBytes    = 256
	maxSessionTokenBytes = 16 << 10
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
	// ErrTokenCanceled reports canceled token generation without exposing the
	// credential provider or signer diagnostic.
	ErrTokenCanceled = errors.New("kafka/mskiam: token generation canceled")
	// ErrTokenTimeout reports that the bounded token deadline elapsed.
	ErrTokenTimeout = errors.New("kafka/mskiam: token generation timed out")
	// ErrInvalidToken is the compatibility category for rejected signer output.
	ErrInvalidToken = errors.New("kafka/mskiam: signer returned an invalid token")
	// ErrMalformedToken reports structurally invalid or unexpectedly
	// long-lived signer output.
	ErrMalformedToken error = &invalidTokenCategory{
		message: "kafka/mskiam: signer returned malformed output",
	}
	// ErrTokenExpired reports a token whose effective validity is insufficient
	// for a new authentication session.
	ErrTokenExpired error = &invalidTokenCategory{
		message: "kafka/mskiam: token is expired or expires too soon",
	}
)

type invalidTokenCategory struct {
	message string
}

func (category *invalidTokenCategory) Error() string {
	return category.message
}

func (category *invalidTokenCategory) Is(target error) bool {
	return target == ErrInvalidToken
}

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
		return nil, newContextError(err)
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
		return kafka.OAuthBearerToken{}, newContextError(err)
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
	if err := tokenCtx.Err(); err != nil {
		return kafka.OAuthBearerToken{}, newContextError(err)
	}
	validatedAt := provider.now()
	expiresAt := time.UnixMilli(expiresAtMilliseconds)
	if credentials.CanExpire && credentials.Expires.Before(expiresAt) {
		expiresAt = credentials.Expires
	}
	if !expiresAt.After(validatedAt.Add(minTokenValidity)) {
		return kafka.OAuthBearerToken{}, ErrTokenExpired
	}
	if expiresAt.After(validatedAt.Add(maxTokenLifetime)) ||
		!validToken(value, provider.region, expiresAtMilliseconds) {
		return kafka.OAuthBearerToken{}, ErrMalformedToken
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
	if err := ctx.Err(); err != nil {
		return aws.Credentials{}, newContextError(err)
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
	if err := ctx.Err(); err != nil {
		return aws.Credentials{}, newContextError(err)
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
		len(credentials.AccessKeyID) <= maxAccessKeyIDBytes &&
		credentials.SecretAccessKey != "" &&
		len(credentials.SecretAccessKey) <= maxSecretKeyBytes &&
		len(credentials.SessionToken) <= maxSessionTokenBytes &&
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
	category        error
	contextCategory error
	cause           error
}

// Error implements error without exposing AWS provider or signer diagnostics.
func (err *ProviderError) Error() string {
	if err == nil {
		return ErrTokenGeneration.Error()
	}
	if err.contextCategory != nil {
		return err.contextCategory.Error()
	}
	if err.category == nil {
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
	if err.contextCategory == nil {
		if err.cause != nil {
			return []error{err.category, err.cause}
		}

		return []error{err.category}
	}
	unwrapped := []error{err.category}
	if err.contextCategory != err.category {
		unwrapped = append(unwrapped, err.contextCategory)
	}
	if err.cause != nil {
		unwrapped = append(unwrapped, err.cause)
	}

	return unwrapped
}

func newProviderError(category error, cause error) error {
	var safeCause error
	var contextCategory error
	switch {
	case errors.Is(cause, context.Canceled):
		safeCause = context.Canceled
		contextCategory = ErrTokenCanceled
	case errors.Is(cause, context.DeadlineExceeded):
		safeCause = context.DeadlineExceeded
		contextCategory = ErrTokenTimeout
	}

	return &ProviderError{
		category:        category,
		contextCategory: contextCategory,
		cause:           safeCause,
	}
}

func newContextError(cause error) error {
	category := ErrTokenCanceled
	if errors.Is(cause, context.DeadlineExceeded) {
		category = ErrTokenTimeout
	}

	return &ProviderError{category: category, cause: cause}
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
		!decimalDigits(segments[len(segments)-1]) ||
		segments[len(segments)-1][0] == '0' {
		return false
	}
	prefixLength := awsRegionPrefixLength(segments)
	if prefixLength == 0 || len(segments) != prefixLength+2 ||
		!lowercaseLetters(segments[prefixLength]) {
		return false
	}

	return true
}

func awsRegionPrefixLength(segments []string) int {
	if len(segments) < 3 {
		return 0
	}
	if len(segments) >= 4 {
		prefix := segments[0] + "-" + segments[1]
		switch prefix {
		case "eusc-de", "eu-isoe", "us-gov", "us-iso", "us-isob", "us-isof":
			return 2
		}
	}
	switch segments[0] {
	case "af", "ap", "ca", "cn", "eu", "il", "me", "mx", "sa", "us":
		return 1
	default:
		return 0
	}
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

func validToken(token string, region string, expiresAtMilliseconds int64) bool {
	if len(token) == 0 {
		return false
	}
	if len(token) > maxTokenBytes {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	signedURL, err := url.ParseRequestURI(string(decoded))
	if err != nil {
		return false
	}
	if signedURL.Scheme != "https" {
		return false
	}
	if signedURL.Host != "kafka."+region+".amazonaws.com" {
		return false
	}
	if signedURL.Path != "/" {
		return false
	}
	if signedURL.User != nil {
		return false
	}
	query, err := url.ParseQuery(signedURL.RawQuery)
	if err != nil {
		return false
	}
	if !validQueryKeys(query) {
		return false
	}
	if !exactQueryValue(query, "Action", "kafka-cluster:Connect") {
		return false
	}
	if !exactQueryValue(query, "X-Amz-Algorithm", "AWS4-HMAC-SHA256") {
		return false
	}
	if !exactQueryValue(query, "X-Amz-SignedHeaders", "host") {
		return false
	}
	if !validUserAgent(query) {
		return false
	}
	if !validSignature(query) {
		return false
	}
	signedAt, err := time.Parse("20060102T150405Z", query.Get("X-Amz-Date"))
	if err != nil {
		return false
	}
	if len(query["X-Amz-Date"]) != 1 {
		return false
	}
	if !validCredentialScope(query, region, signedAt) {
		return false
	}
	expiresSeconds, err := strconv.ParseInt(query.Get("X-Amz-Expires"), 10, 64)
	if err != nil {
		return false
	}
	if len(query["X-Amz-Expires"]) != 1 {
		return false
	}
	if expiresSeconds <= 0 {
		return false
	}
	if expiresSeconds > int64(maxTokenLifetime/time.Second) {
		return false
	}
	wantExpiry := signedAt.Add(time.Duration(expiresSeconds) * time.Second)

	return wantExpiry.UnixMilli() == expiresAtMilliseconds
}

func validQueryKeys(query url.Values) bool {
	if len(query) < 8 {
		return false
	}
	if len(query) > 9 {
		return false
	}
	for key := range query {
		switch key {
		case "Action", "User-Agent", "X-Amz-Algorithm", "X-Amz-Credential",
			"X-Amz-Date", "X-Amz-Expires", "X-Amz-Signature",
			"X-Amz-SignedHeaders":
		case "X-Amz-Security-Token":
			values := query[key]
			if len(values) != 1 {
				return false
			}
			if values[0] == "" {
				return false
			}
		default:
			return false
		}
	}

	return true
}

func exactQueryValue(query url.Values, key string, value string) bool {
	values, ok := query[key]
	if !ok {
		return false
	}
	if len(values) != 1 {
		return false
	}

	return values[0] == value
}

func validUserAgent(query url.Values) bool {
	values := query["User-Agent"]
	if len(values) != 1 {
		return false
	}

	return strings.HasPrefix(values[0], "aws-msk-iam-sasl-signer-go/")
}

func validSignature(query url.Values) bool {
	values := query["X-Amz-Signature"]
	if len(values) != 1 {
		return false
	}
	if len(values[0]) != 64 {
		return false
	}
	for _, current := range values[0] {
		if (current < '0' || current > '9') &&
			(current < 'a' || current > 'f') {
			return false
		}
	}

	return true
}

func validCredentialScope(
	query url.Values,
	region string,
	signedAt time.Time,
) bool {
	values := query["X-Amz-Credential"]
	if len(values) != 1 {
		return false
	}
	parts := strings.Split(values[0], "/")
	if len(parts) != 5 {
		return false
	}
	if parts[0] == "" {
		return false
	}
	if len(parts[0]) > 128 {
		return false
	}
	if parts[1] != signedAt.UTC().Format("20060102") {
		return false
	}
	if parts[2] != region {
		return false
	}
	if parts[3] != "kafka-cluster" {
		return false
	}

	return parts[4] == "aws4_request"
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
