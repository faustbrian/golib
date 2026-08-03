package awssecretsmanager

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	config "github.com/faustbrian/golib/pkg/config"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
)

type stubClient struct {
	input  *secretsmanager.GetSecretValueInput
	output *secretsmanager.GetSecretValueOutput
	err    error
	calls  int
}

type valueClient struct{}

func (valueClient) GetSecretValue(
	context.Context,
	*secretsmanager.GetSecretValueInput,
	...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{}, nil
}

func (client *stubClient) GetSecretValue(
	_ context.Context,
	input *secretsmanager.GetSecretValueInput,
	_ ...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	client.calls++
	client.input = input

	return client.output, client.err
}

func TestSourceLoadsSensitiveJSONFromCurrentSecretVersion(t *testing.T) {
	t.Parallel()

	payload := `{"database":{"url":"postgres://synthetic"},"workers":4}`
	client := &stubClient{
		output: &secretsmanager.GetSecretValueOutput{SecretString: &payload},
	}
	source, err := New(client, Options{
		Name:     "runtime-secrets",
		SecretID: "track/production/runtime",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := map[string]any{
		"database": map[string]any{"url": "postgres://synthetic"},
		"workers":  int64(4),
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Load() tree = %#v, want %#v", document.Tree, want)
	}
	if client.calls != 1 || client.input == nil ||
		value(client.input.SecretId) != "track/production/runtime" ||
		value(client.input.VersionStage) != "AWSCURRENT" ||
		client.input.VersionId != nil {
		t.Fatalf("GetSecretValue() input = %#v", client.input)
	}
	if got := source.Info(); got != (config.SourceInfo{
		Name: "runtime-secrets", Priority: config.PriorityDiscoveredProfile,
		Sensitive: true,
	}) {
		t.Fatalf("Info() = %#v", got)
	}
}

func TestSourceLoadsBinaryJSONAtExplicitImmutableVersion(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		output: &secretsmanager.GetSecretValueOutput{
			SecretBinary: []byte(`{"token":"synthetic"}`),
		},
	}
	source, err := New(client, Options{
		Name:         "runtime-secrets",
		Priority:     config.PriorityExplicitFiles,
		Optional:     true,
		SecretID:     "track/staging/runtime",
		VersionID:    "immutable-version",
		VersionStage: "candidate",
		MaximumBytes: 128,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if document.Tree["token"] != "synthetic" {
		t.Fatalf("Load() tree = %#v", document.Tree)
	}
	if value(client.input.VersionId) != "immutable-version" ||
		value(client.input.VersionStage) != "candidate" {
		t.Fatalf("GetSecretValue() version input = %#v", client.input)
	}
	if got := source.Info(); got.Priority != config.PriorityExplicitFiles ||
		!got.Optional || !got.Sensitive {
		t.Fatalf("Info() = %#v", got)
	}
}

func TestSourceAcceptsExactResourceLimits(t *testing.T) {
	t.Parallel()

	payload := `{"value":"` + strings.Repeat("x", MaximumSecretBytes-12) + `"}`
	client := &stubClient{output: &secretsmanager.GetSecretValueOutput{
		SecretString: &payload,
	}}
	source, err := New(client, Options{
		Name:         "runtime-secrets",
		SecretID:     strings.Repeat("s", MaximumSecretIDBytes),
		MaximumBytes: MaximumSecretBytes,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := document.Tree["value"]; got != strings.Repeat("x", MaximumSecretBytes-12) {
		t.Fatalf("Load() value length = %d, want %d", len(got.(string)), MaximumSecretBytes-12)
	}
}

func TestSourcePreservesEachExplicitVersionSelector(t *testing.T) {
	t.Parallel()

	tests := map[string]Options{
		"version ID":    {VersionID: "immutable-version"},
		"version stage": {VersionStage: "candidate"},
	}
	for name, version := range tests {
		name, version := name, version

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload := `{}`
			client := &stubClient{output: &secretsmanager.GetSecretValueOutput{
				SecretString: &payload,
			}}
			version.Name = "runtime-secrets"
			version.SecretID = "track/runtime"
			source, err := New(client, version)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := value(client.input.VersionId); got != version.VersionID {
				t.Fatalf("GetSecretValue() version ID = %q, want %q", got, version.VersionID)
			}
			if got := value(client.input.VersionStage); got != version.VersionStage {
				t.Fatalf("GetSecretValue() version stage = %q, want %q", got, version.VersionStage)
			}
		})
	}
}

func TestNewRejectsUnsafeComposition(t *testing.T) {
	t.Parallel()

	client := &stubClient{}
	var typedNil *stubClient
	tests := map[string]Options{
		"blank name":          {SecretID: "track/runtime"},
		"blank secret id":     {Name: "runtime"},
		"oversized secret id": {Name: "runtime", SecretID: strings.Repeat("s", MaximumSecretIDBytes+1)},
		"negative limit":      {Name: "runtime", SecretID: "track/runtime", MaximumBytes: -1},
		"oversized limit":     {Name: "runtime", SecretID: "track/runtime", MaximumBytes: MaximumSecretBytes + 1},
	}

	for name, options := range tests {
		name, options := name, options

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := New(client, options); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
			}
		})
	}
	for name, candidate := range map[string]Client{"nil": nil, "typed nil": typedNil} {
		name, candidate := name, candidate

		t.Run(name+" client", func(t *testing.T) {
			t.Parallel()

			_, err := New(candidate, Options{Name: "runtime", SecretID: "track/runtime"})
			if !errors.Is(err, ErrClientRequired) {
				t.Fatalf("New() error = %v, want ErrClientRequired", err)
			}
		})
	}
	if _, err := New(valueClient{}, Options{
		Name: "runtime", SecretID: "track/runtime",
	}); err != nil {
		t.Fatalf("New() value client error = %v", err)
	}
}

func TestSourceRejectsInvalidProviderResponses(t *testing.T) {
	t.Parallel()

	textPayload := `{"value":true}`
	tests := map[string]*secretsmanager.GetSecretValueOutput{
		"nil":          nil,
		"empty":        {},
		"both formats": {SecretString: &textPayload, SecretBinary: []byte(`{}`)},
		"too large":    {SecretBinary: []byte(strings.Repeat("x", 17))},
	}

	for name, output := range tests {
		name, output := name, output

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source, err := New(&stubClient{output: output}, Options{
				Name: "runtime", SecretID: "track/runtime", MaximumBytes: 16,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := source.Load(context.Background()); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Load() error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestSourceMapsMissingSecretAndRedactsProviderFailures(t *testing.T) {
	t.Parallel()

	missing := &types.ResourceNotFoundException{Message: stringPointer("provider detail")}
	source, err := New(&stubClient{err: missing}, Options{
		Name: "runtime", SecretID: "track/runtime", Optional: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := source.Load(context.Background()); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Load() error = %v, want config.ErrNotFound", err)
	}

	providerMarker := "provider-secret-marker"
	providerErr := errors.New(providerMarker)
	source, err = New(&stubClient{err: providerErr}, Options{
		Name: "runtime", SecretID: "track/runtime",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if !errors.Is(err, providerErr) || !errors.Is(err, ErrOperation) {
		t.Fatalf("Load() error = %v, want wrapped operation error", err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), providerMarker) {
		t.Fatal("Load() error exposed provider details")
	}
}

func TestSourceHonorsCancellationAndPropagatesSafeJSONFailures(t *testing.T) {
	t.Parallel()

	client := &stubClient{}
	source, err := New(client, Options{Name: "runtime", SecretID: "track/runtime"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
	if client.calls != 0 {
		t.Fatalf("GetSecretValue() calls = %d, want 0", client.calls)
	}

	invalid := "not-json"
	source, err = New(&stubClient{output: &secretsmanager.GetSecretValueOutput{
		SecretString: &invalid,
	}}, Options{Name: "runtime", SecretID: "track/runtime"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("Load() error = nil, want JSON failure")
	}

	expected := errors.New("synthetic parser construction failure")
	source, err = New(&stubClient{output: &secretsmanager.GetSecretValueOutput{
		SecretString: stringPointer(`{}`),
	}}, Options{Name: "runtime", SecretID: "track/runtime"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	concrete := source.(*secretsManagerSource)
	concrete.parseJSON = func(
		[]byte,
		jsonsource.Options,
	) (config.Source, error) {
		return nil, expected
	}
	if _, err := concrete.Load(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("Load() error = %v, want parser construction failure", err)
	}
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}

	return *pointer
}
