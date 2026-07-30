// Package awssecretsmanager stores immutable, explicitly versioned secret
// payloads in AWS Secrets Manager.
package awssecretsmanager

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

const (
	maximumSecretNameBytes = 512
	maximumVersionIDBytes  = 64
	minimumVersionIDBytes  = 32
	maximumStageBytes      = 256
	maximumSecretBytes     = 65_536
	maximumKMSKeyIDBytes   = 2_048
)

var (
	// ErrClientRequired identifies missing AWS Secrets Manager composition.
	ErrClientRequired = errors.New(
		"AWS Secrets Manager client is required",
	)
	// ErrInvalidKMSKey identifies an unsafe configured KMS key identifier.
	ErrInvalidKMSKey = errors.New(
		"AWS Secrets Manager KMS key identifier is invalid",
	)
	// ErrInvalidRequest identifies an unsafe or unsupported secret version.
	ErrInvalidRequest = errors.New(
		"AWS Secrets Manager version request is invalid",
	)
	// ErrOperation identifies a failed AWS Secrets Manager API operation.
	ErrOperation = errors.New(
		"AWS Secrets Manager operation failed",
	)
	// ErrInvalidResponse identifies an incomplete or contradictory AWS result.
	ErrInvalidResponse = errors.New(
		"AWS Secrets Manager response is invalid",
	)
)

// Client is the least-privilege AWS Secrets Manager surface used by Store.
type Client interface {
	CreateSecret(
		context.Context,
		*secretsmanager.CreateSecretInput,
		...func(*secretsmanager.Options),
	) (*secretsmanager.CreateSecretOutput, error)
	PutSecretValue(
		context.Context,
		*secretsmanager.PutSecretValueInput,
		...func(*secretsmanager.Options),
	) (*secretsmanager.PutSecretValueOutput, error)
}

// PutVersionRequest names one immutable secret value and its unique staging
// label. VersionID must be a stable 32-64 byte AWS client request token. Stage
// must be unique to this version so writing historical versions never moves a
// shared label such as AWSCURRENT.
type PutVersionRequest struct {
	Name      string
	VersionID string
	Stage     string
	Value     []byte
}

// Reference is the opaque AWS secret and immutable version identity returned
// to application persistence.
type Reference struct {
	ARN       string
	VersionID string
}

// Store creates one secret container or adds one immutable version to an
// existing container. Store is safe for concurrent use when its Client is.
type Store struct {
	client   Client
	kmsKeyID string
}

// New constructs a version store. An empty KMS key identifier selects the AWS
// Secrets Manager service key; a nonempty identifier is sent only when the
// secret container is first created.
func New(client Client, kmsKeyID string) (*Store, error) {
	if nilLike(client) {
		return nil, ErrClientRequired
	}
	if !validKMSKeyID(kmsKeyID) {
		return nil, ErrInvalidKMSKey
	}

	return &Store{
		client:   client,
		kmsKeyID: kmsKeyID,
	}, nil
}

// PutVersion creates or idempotently confirms one immutable version.
//
// AWS ClientRequestToken becomes VersionId. AWS accepts an exact retry and
// rejects reuse of the token with different secret material. Existing
// containers receive a unique caller-owned staging label, preventing an older
// import from moving AWSCURRENT or another version's label.
func (store *Store) PutVersion(
	ctx context.Context,
	request PutVersionRequest,
) (Reference, error) {
	if store == nil || nilLike(store.client) {
		return Reference{}, ErrClientRequired
	}
	if err := validateRequest(ctx, request); err != nil {
		return Reference{}, err
	}

	value := append([]byte(nil), request.Value...)
	defer zero(value)

	output, err := store.client.CreateSecret(
		ctx,
		&secretsmanager.CreateSecretInput{
			ClientRequestToken: aws.String(request.VersionID),
			KmsKeyId:           optionalString(store.kmsKeyID),
			Name:               aws.String(request.Name),
			SecretBinary:       value,
		},
	)
	if err == nil {
		return createReference(output, request.VersionID)
	}
	if !resourceExists(err) {
		return Reference{}, operationError{
			operation: "create",
			cause:     err,
		}
	}

	putOutput, err := store.client.PutSecretValue(
		ctx,
		&secretsmanager.PutSecretValueInput{
			ClientRequestToken: aws.String(request.VersionID),
			SecretBinary:       value,
			SecretId:           aws.String(request.Name),
			VersionStages:      []string{request.Stage},
		},
	)
	if err != nil {
		return Reference{}, operationError{
			operation: "put",
			cause:     err,
		}
	}

	return putReference(putOutput, request.VersionID)
}

func validateRequest(
	ctx context.Context,
	request PutVersionRequest,
) error {
	if ctx == nil {
		return ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validSecretName(request.Name) ||
		!validVersionID(request.VersionID) ||
		!validStage(request.Stage) ||
		len(request.Value) == 0 ||
		len(request.Value) > maximumSecretBytes {
		return ErrInvalidRequest
	}

	return nil
}

func validSecretName(value string) bool {
	if len(value) == 0 || len(value) > maximumSecretNameBytes {
		return false
	}
	for index := range len(value) {
		if !validNameByte(value[index]) {
			return false
		}
	}

	return true
}

func validVersionID(value string) bool {
	if len(value) < minimumVersionIDBytes ||
		len(value) > maximumVersionIDBytes {
		return false
	}
	for index := range len(value) {
		current := value[index]
		if !asciiLetterOrDigit(current) && current != '-' {
			return false
		}
	}

	return true
}

func validStage(value string) bool {
	if len(value) == 0 || len(value) > maximumStageBytes {
		return false
	}
	switch value {
	case "AWSCURRENT", "AWSPREVIOUS", "AWSPENDING":
		return false
	}
	for index := range len(value) {
		if !validNameByte(value[index]) {
			return false
		}
	}

	return true
}

func validKMSKeyID(value string) bool {
	return len(value) <= maximumKMSKeyIDBytes &&
		strings.TrimSpace(value) == value
}

func validNameByte(value byte) bool {
	return asciiLetterOrDigit(value) ||
		value == '/' ||
		value == '_' ||
		value == '+' ||
		value == '=' ||
		value == '.' ||
		value == '@' ||
		value == '-'
}

func asciiLetterOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}

func createReference(
	output *secretsmanager.CreateSecretOutput,
	versionID string,
) (Reference, error) {
	if output == nil {
		return Reference{}, ErrInvalidResponse
	}

	return newReference(
		aws.ToString(output.ARN),
		aws.ToString(output.VersionId),
		versionID,
	)
}

func putReference(
	output *secretsmanager.PutSecretValueOutput,
	versionID string,
) (Reference, error) {
	if output == nil {
		return Reference{}, ErrInvalidResponse
	}

	return newReference(
		aws.ToString(output.ARN),
		aws.ToString(output.VersionId),
		versionID,
	)
}

func newReference(
	arn string,
	returnedVersionID string,
	expectedVersionID string,
) (Reference, error) {
	if arn == "" ||
		returnedVersionID == "" ||
		returnedVersionID != expectedVersionID {
		return Reference{}, ErrInvalidResponse
	}

	return Reference{
		ARN:       arn,
		VersionID: returnedVersionID,
	}, nil
}

func resourceExists(err error) bool {
	var target *types.ResourceExistsException

	return errors.As(err, &target)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return aws.String(value)
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type operationError struct {
	operation string
	cause     error
}

func (err operationError) Error() string {
	return "AWS Secrets Manager " + err.operation + " failed"
}

func (err operationError) Unwrap() []error {
	return []error{ErrOperation, err.cause}
}

var _ Client = (*secretsmanager.Client)(nil)
