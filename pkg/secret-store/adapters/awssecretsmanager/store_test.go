package awssecretsmanager

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

func TestPutVersionCreatesOrIdempotentlyAddsImmutableVersion(t *testing.T) {
	t.Parallel()

	const (
		secretName = "track/carrier-account/01K4ZVHQ8R7XH2WTB9F2T9G6HY"
		versionID  = "6e45f31f4601a3af2e99f2f479d691650fb71cd2d93a76872f4e9f5fa7b42135"
		stage      = "track-migration-" + versionID
		secretARN  = "arn:aws:secretsmanager:eu-north-1:000000000000:secret:synthetic"
	)
	value := []byte(`{"client_id":"synthetic","client_secret":"synthetic"}`)

	tests := map[string]struct {
		client *stubClient
	}{
		"create": {
			client: &stubClient{
				createOutput: &secretsmanager.CreateSecretOutput{
					ARN:       aws.String(secretARN),
					VersionId: aws.String(versionID),
				},
			},
		},
		"existing": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
				putOutput: &secretsmanager.PutSecretValueOutput{
					ARN:       aws.String(secretARN),
					VersionId: aws.String(versionID),
				},
			},
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := New(testCase.client, "alias/track-secrets")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			reference, err := store.PutVersion(
				context.Background(),
				PutVersionRequest{
					Name:      secretName,
					VersionID: versionID,
					Stage:     stage,
					Value:     value,
				},
			)
			if err != nil {
				t.Fatalf("PutVersion() error = %v", err)
			}
			if reference.ARN != secretARN ||
				reference.VersionID != versionID {
				t.Fatalf("PutVersion() = %#v", reference)
			}

			if testCase.client.createInput == nil {
				t.Fatal("CreateSecret() was not called")
			}
			if aws.ToString(testCase.client.createInput.Name) != secretName ||
				aws.ToString(testCase.client.createInput.ClientRequestToken) !=
					versionID ||
				aws.ToString(testCase.client.createInput.KmsKeyId) !=
					"alias/track-secrets" ||
				!bytes.Equal(testCase.client.createValue, value) {
				t.Fatalf(
					"CreateSecret() input = %#v",
					testCase.client.createInput,
				)
			}

			if name == "existing" {
				if testCase.client.putInput == nil {
					t.Fatal("PutSecretValue() was not called")
				}
				if aws.ToString(testCase.client.putInput.SecretId) !=
					secretName ||
					aws.ToString(
						testCase.client.putInput.ClientRequestToken,
					) != versionID ||
					len(testCase.client.putInput.VersionStages) != 1 ||
					testCase.client.putInput.VersionStages[0] != stage ||
					!bytes.Equal(testCase.client.putValue, value) {
					t.Fatalf(
						"PutSecretValue() input = %#v",
						testCase.client.putInput,
					)
				}
			}
		})
	}
}

func TestNewRejectsInvalidComposition(t *testing.T) {
	t.Parallel()

	var typedNil *stubClient
	tests := map[string]struct {
		client   Client
		kmsKeyID string
		want     error
	}{
		"missing client": {
			want: ErrClientRequired,
		},
		"typed nil client": {
			client: typedNil,
			want:   ErrClientRequired,
		},
		"surrounding KMS whitespace": {
			client:   &stubClient{},
			kmsKeyID: " alias/track ",
			want:     ErrInvalidKMSKey,
		},
		"oversized KMS key": {
			client:   &stubClient{},
			kmsKeyID: strings.Repeat("k", maximumKMSKeyIDBytes+1),
			want:     ErrInvalidKMSKey,
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := New(testCase.client, testCase.kmsKeyID)
			if store != nil || !errors.Is(err, testCase.want) {
				t.Fatalf("New() = %#v, %v, want %v", store, err, testCase.want)
			}
		})
	}
}

func TestPutVersionRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	valid := validPutVersionRequest()
	tests := map[string]struct {
		store   *Store
		ctx     context.Context
		request PutVersionRequest
		want    error
	}{
		"nil store": {
			ctx:     context.Background(),
			request: valid,
			want:    ErrClientRequired,
		},
		"typed nil client": {
			store: &Store{
				client: (*stubClient)(nil),
			},
			ctx:     context.Background(),
			request: valid,
			want:    ErrClientRequired,
		},
		"nil context": {
			store:   validStore(t),
			request: valid,
			want:    ErrInvalidRequest,
		},
		"canceled context": {
			store:   validStore(t),
			ctx:     canceledContext,
			request: valid,
			want:    context.Canceled,
		},
		"empty name": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Name = ""
			}),
			want: ErrInvalidRequest,
		},
		"oversized name": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Name = strings.Repeat(
					"n",
					maximumSecretNameBytes+1,
				)
			}),
			want: ErrInvalidRequest,
		},
		"invalid name": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Name = "track secret"
			}),
			want: ErrInvalidRequest,
		},
		"short version": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.VersionID = strings.Repeat(
					"v",
					minimumVersionIDBytes-1,
				)
			}),
			want: ErrInvalidRequest,
		},
		"long version": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.VersionID = strings.Repeat(
					"v",
					maximumVersionIDBytes+1,
				)
			}),
			want: ErrInvalidRequest,
		},
		"invalid version": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.VersionID = strings.Repeat(
					"v",
					minimumVersionIDBytes-1,
				) + "/"
			}),
			want: ErrInvalidRequest,
		},
		"empty stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = ""
			}),
			want: ErrInvalidRequest,
		},
		"oversized stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = strings.Repeat(
					"s",
					maximumStageBytes+1,
				)
			}),
			want: ErrInvalidRequest,
		},
		"invalid stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = "migration stage"
			}),
			want: ErrInvalidRequest,
		},
		"current stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = "AWSCURRENT"
			}),
			want: ErrInvalidRequest,
		},
		"previous stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = "AWSPREVIOUS"
			}),
			want: ErrInvalidRequest,
		},
		"pending stage": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Stage = "AWSPENDING"
			}),
			want: ErrInvalidRequest,
		},
		"empty value": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Value = nil
			}),
			want: ErrInvalidRequest,
		},
		"oversized value": {
			store: validStore(t),
			ctx:   context.Background(),
			request: mutateRequest(valid, func(request *PutVersionRequest) {
				request.Value = make(
					[]byte,
					maximumSecretBytes+1,
				)
			}),
			want: ErrInvalidRequest,
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reference, err := testCase.store.PutVersion(
				testCase.ctx,
				testCase.request,
			)
			if reference != (Reference{}) ||
				!errors.Is(err, testCase.want) {
				t.Fatalf(
					"PutVersion() = %#v, %v, want %v",
					reference,
					err,
					testCase.want,
				)
			}
		})
	}
}

func TestValidationAcceptsExactLimitsAndASCIIEndpoints(t *testing.T) {
	t.Parallel()

	request := PutVersionRequest{
		Name:      strings.Repeat("n", maximumSecretNameBytes),
		VersionID: strings.Repeat("v", minimumVersionIDBytes),
		Stage:     strings.Repeat("s", maximumStageBytes),
		Value:     make([]byte, maximumSecretBytes),
	}
	if err := validateRequest(context.Background(), request); err != nil {
		t.Fatalf("validateRequest() at exact limits error = %v", err)
	}
	if !validVersionID(strings.Repeat("v", maximumVersionIDBytes)) {
		t.Fatal("maximum-length version ID was rejected")
	}
	if !validKMSKeyID(strings.Repeat("k", maximumKMSKeyIDBytes)) {
		t.Fatal("maximum-length KMS key ID was rejected")
	}
	for _, value := range []byte{'a', 'z', 'A', 'Z', '0', '9'} {
		if !asciiLetterOrDigit(value) {
			t.Fatalf("ASCII range endpoint %q was rejected", value)
		}
	}
}

func TestPutVersionContainsSecretSafeFailures(t *testing.T) {
	t.Parallel()

	const sensitive = "TRACK_SECRET_CANARY_NEVER_EMIT"
	operationFailure := errors.New("synthetic operation failure")
	tests := map[string]struct {
		client *stubClient
		want   error
	}{
		"create": {
			client: &stubClient{
				createErr: operationFailure,
			},
			want: operationFailure,
		},
		"put": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
				putErr:    operationFailure,
			},
			want: operationFailure,
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := New(testCase.client, "")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := validPutVersionRequest()
			request.Value = []byte(sensitive)

			reference, err := store.PutVersion(
				context.Background(),
				request,
			)
			if reference != (Reference{}) ||
				!errors.Is(err, ErrOperation) ||
				!errors.Is(err, testCase.want) {
				t.Fatalf("PutVersion() = %#v, %v", reference, err)
			}
			if strings.Contains(err.Error(), sensitive) {
				t.Fatalf("error exposes secret material: %v", err)
			}
		})
	}
}

func TestPutVersionRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	valid := validPutVersionRequest()
	validARN := aws.String(
		"arn:aws:secretsmanager:eu-north-1:000000000000:secret:synthetic",
	)
	validVersionID := aws.String(valid.VersionID)
	otherVersionID := aws.String(strings.Repeat("f", maximumVersionIDBytes))
	tests := map[string]struct {
		client *stubClient
	}{
		"nil create output": {
			client: &stubClient{},
		},
		"empty create ARN": {
			client: &stubClient{
				createOutput: &secretsmanager.CreateSecretOutput{
					VersionId: validVersionID,
				},
			},
		},
		"empty create version": {
			client: &stubClient{
				createOutput: &secretsmanager.CreateSecretOutput{
					ARN: validARN,
				},
			},
		},
		"mismatched create version": {
			client: &stubClient{
				createOutput: &secretsmanager.CreateSecretOutput{
					ARN:       validARN,
					VersionId: otherVersionID,
				},
			},
		},
		"nil put output": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
			},
		},
		"empty put ARN": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
				putOutput: &secretsmanager.PutSecretValueOutput{
					VersionId: validVersionID,
				},
			},
		},
		"empty put version": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
				putOutput: &secretsmanager.PutSecretValueOutput{
					ARN: validARN,
				},
			},
		},
		"mismatched put version": {
			client: &stubClient{
				createErr: &types.ResourceExistsException{},
				putOutput: &secretsmanager.PutSecretValueOutput{
					ARN:       validARN,
					VersionId: otherVersionID,
				},
			},
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := New(testCase.client, "")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			reference, err := store.PutVersion(
				context.Background(),
				valid,
			)
			if reference != (Reference{}) ||
				!errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("PutVersion() = %#v, %v", reference, err)
			}
		})
	}
}

func TestHelpersHandleValueAndNilKinds(t *testing.T) {
	t.Parallel()

	if nilLike(42) {
		t.Fatal("integer is nil-like")
	}
	if !nilLike((chan int)(nil)) {
		t.Fatal("nil channel is not nil-like")
	}
}

func validPutVersionRequest() PutVersionRequest {
	const versionID = "6e45f31f4601a3af2e99f2f479d691650fb71cd2d93a76872f4e9f5fa7b42135"

	return PutVersionRequest{
		Name:      "track/carrier-account/01K4ZVHQ8R7XH2WTB9F2T9G6HY",
		VersionID: versionID,
		Stage:     "track-migration-" + versionID,
		Value:     []byte(`{"client_id":"synthetic"}`),
	}
}

func validStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(&stubClient{}, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return store
}

func mutateRequest(
	request PutVersionRequest,
	mutate func(*PutVersionRequest),
) PutVersionRequest {
	mutate(&request)

	return request
}

type stubClient struct {
	createInput  *secretsmanager.CreateSecretInput
	createValue  []byte
	createOutput *secretsmanager.CreateSecretOutput
	createErr    error
	putInput     *secretsmanager.PutSecretValueInput
	putValue     []byte
	putOutput    *secretsmanager.PutSecretValueOutput
	putErr       error
}

func (client *stubClient) CreateSecret(
	_ context.Context,
	input *secretsmanager.CreateSecretInput,
	_ ...func(*secretsmanager.Options),
) (*secretsmanager.CreateSecretOutput, error) {
	client.createInput = input
	client.createValue = append([]byte(nil), input.SecretBinary...)

	return client.createOutput, client.createErr
}

func (client *stubClient) PutSecretValue(
	_ context.Context,
	input *secretsmanager.PutSecretValueInput,
	_ ...func(*secretsmanager.Options),
) (*secretsmanager.PutSecretValueOutput, error) {
	client.putInput = input
	client.putValue = append([]byte(nil), input.SecretBinary...)

	return client.putOutput, client.putErr
}
