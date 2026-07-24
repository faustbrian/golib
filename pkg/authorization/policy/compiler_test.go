package policy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type fixedEvaluator struct {
	decision authorization.Decision
}

func (evaluator fixedEvaluator) Evaluate(
	context.Context,
	authorization.Request,
) (authorization.Decision, error) {
	return evaluator.decision, nil
}

func TestCompilerBuildsSnapshotWithModelDecoder(t *testing.T) {
	t.Parallel()

	activeFrom := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	activeUntil := activeFrom.Add(time.Hour)
	decoded := json.RawMessage(nil)
	compiler, err := NewCompiler(map[Model]Decoder{
		ModelACL: DecoderFunc(func(document json.RawMessage) (authorization.Evaluator, error) {
			decoded = append(decoded[:0], document...)
			return fixedEvaluator{decision: authorization.Decision{
				Outcome: authorization.Allow,
				Reason:  "compiled",
			}}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	manifest := Manifest{
		Format:    FormatV1,
		Revision:  9,
		Algorithm: AlgorithmDenyOverrides,
		Policies: []Record{{
			ID: "acl", Revision: 3, Model: ModelACL, Priority: 10,
			ActiveFrom: &activeFrom, ActiveUntil: &activeUntil,
			Metadata: map[string]string{"owner": "security"},
			Document: json.RawMessage(`{"entries":[]}`),
		}},
	}
	snapshot, err := compiler.Compile(manifest)
	if err != nil {
		t.Fatalf("Compiler.Compile() error = %v", err)
	}
	if snapshot.Revision() != 9 || snapshot.Algorithm() != authorization.DenyOverrides {
		t.Errorf("compiled snapshot = revision %d algorithm %d", snapshot.Revision(), snapshot.Algorithm())
	}
	policies := snapshot.Policies()
	if len(policies) != 1 || policies[0].ID != "acl" ||
		policies[0].Revision != 3 || policies[0].Priority != 10 ||
		!policies[0].ActiveFrom.Equal(activeFrom) ||
		!policies[0].ActiveUntil.Equal(activeUntil) ||
		policies[0].Metadata["owner"] != "security" {
		t.Errorf("compiled policies = %+v", policies)
	}
	if string(decoded) != `{"entries":[]}` {
		t.Errorf("decoded document = %s", decoded)
	}
}

func TestCompilerMapsEveryAlgorithm(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler(map[Model]Decoder{})
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	tests := map[Algorithm]authorization.CombiningAlgorithm{
		AlgorithmDenyOverrides:   authorization.DenyOverrides,
		AlgorithmAllowOverrides:  authorization.AllowOverrides,
		AlgorithmFirstApplicable: authorization.FirstApplicable,
		AlgorithmPriorityOrder:   authorization.PriorityOrder,
	}
	for algorithm, want := range tests {
		manifest := Manifest{Format: FormatV1, Revision: 1, Algorithm: algorithm, Policies: []Record{}}
		snapshot, err := compiler.Compile(manifest)
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", algorithm, err)
		}
		if snapshot.Algorithm() != want {
			t.Errorf("Compile(%q) algorithm = %d, want %d", algorithm, snapshot.Algorithm(), want)
		}
	}
}

func TestCompilerFailsClosed(t *testing.T) {
	t.Parallel()

	decoderError := errors.New("invalid model document")
	tests := map[string]struct {
		decoders map[Model]Decoder
		options  []CompilerOption
		manifest Manifest
		want     error
	}{
		"invalid manifest": {
			decoders: map[Model]Decoder{},
			manifest: Manifest{},
			want:     ErrInvalidManifest,
		},
		"missing decoder": {
			decoders: map[Model]Decoder{},
			manifest: compilerManifest(ModelACL, `{}`),
			want:     ErrMissingDecoder,
		},
		"decoder error": {
			decoders: map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
				return nil, decoderError
			})},
			manifest: compilerManifest(ModelACL, `{}`),
			want:     decoderError,
		},
		"nil evaluator": {
			decoders: map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
				return nil, nil
			})},
			manifest: compilerManifest(ModelACL, `{}`),
			want:     ErrNilEvaluator,
		},
		"document limit": {
			decoders: map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
				return fixedEvaluator{}, nil
			})},
			options:  []CompilerOption{WithCompilerLimits(CompilerLimits{MaxDocumentBytes: 1})},
			manifest: compilerManifest(ModelACL, `{}`),
			want:     ErrDocumentLimitExceeded,
		},
		"policy limit": {
			decoders: map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
				return fixedEvaluator{}, nil
			})},
			options: []CompilerOption{WithCompilerLimits(CompilerLimits{MaxPolicies: 1})},
			manifest: func() Manifest {
				manifest := compilerManifest(ModelACL, `{}`)
				second := manifest.Policies[0]
				second.ID = "second"
				manifest.Policies = append(manifest.Policies, second)
				return manifest
			}(),
			want: ErrPolicyLimitExceeded,
		},
		"aggregate document limit": {
			decoders: map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
				return fixedEvaluator{}, nil
			})},
			options:  []CompilerOption{WithCompilerLimits(CompilerLimits{MaxTotalDocumentBytes: 1})},
			manifest: compilerManifest(ModelACL, `{}`),
			want:     ErrTotalDocumentLimitExceeded,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := NewCompiler(tt.decoders, tt.options...)
			if err != nil {
				t.Fatalf("NewCompiler() error = %v", err)
			}
			_, err = compiler.Compile(tt.manifest)
			if !errors.Is(err, tt.want) {
				t.Errorf("Compile() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCompilerAcceptsExactConfiguredLimits(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler(
		map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
			return fixedEvaluator{}, nil
		})},
		WithCompilerLimits(CompilerLimits{
			MaxDocumentBytes:      2,
			MaxPolicies:           2,
			MaxTotalDocumentBytes: 4,
		}),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	manifest := compilerManifest(ModelACL, `{}`)
	second := manifest.Policies[0]
	second.ID = "second"
	manifest.Policies = append(manifest.Policies, second)

	if _, err := compiler.Compile(manifest); err != nil {
		t.Errorf("Compile(at exact limits) error = %v", err)
	}

	defaultCompiler, err := NewCompiler(
		map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
			return fixedEvaluator{}, nil
		})},
		WithCompilerLimits(CompilerLimits{}),
	)
	if err != nil {
		t.Fatalf("NewCompiler(zero limits) error = %v", err)
	}
	if _, err := defaultCompiler.Compile(compilerManifest(ModelACL, `{}`)); err != nil {
		t.Errorf("Compile(with zero-value limit option) error = %v", err)
	}

	compiler, err = NewCompiler(
		map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
			return fixedEvaluator{}, nil
		})},
		WithCompilerLimits(CompilerLimits{MaxTotalDocumentBytes: 3}),
	)
	if err != nil {
		t.Fatalf("NewCompiler(aggregate limit) error = %v", err)
	}
	if _, err := compiler.Compile(manifest); !errors.Is(err, ErrTotalDocumentLimitExceeded) {
		t.Errorf("Compile(over aggregate limit) error = %v, want ErrTotalDocumentLimitExceeded", err)
	}
}

func TestNewCompilerRejectsNilDecoderAndCopiesRegistry(t *testing.T) {
	t.Parallel()

	if _, err := NewCompiler(map[Model]Decoder{ModelACL: nil}); !errors.Is(err, ErrNilDecoder) {
		t.Errorf("NewCompiler(nil decoder) error = %v, want ErrNilDecoder", err)
	}
	decoders := map[Model]Decoder{ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
		return fixedEvaluator{}, nil
	})}
	compiler, err := NewCompiler(decoders)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	delete(decoders, ModelACL)
	if _, err := compiler.Compile(compilerManifest(ModelACL, `{}`)); err != nil {
		t.Errorf("Compile() after caller registry mutation error = %v", err)
	}
}

func compilerManifest(model Model, document string) Manifest {
	return Manifest{
		Format: FormatV1, Revision: 1, Algorithm: AlgorithmDenyOverrides,
		Policies: []Record{{
			ID: "policy", Revision: 1, Model: model,
			Document: json.RawMessage(document),
		}},
	}
}
