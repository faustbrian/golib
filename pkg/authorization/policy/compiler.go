package policy

import (
	"encoding/json"
	"errors"
	"fmt"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

const (
	defaultMaxDocumentBytes      = 1 << 20
	defaultMaxPolicies           = 1000
	defaultMaxTotalDocumentBytes = 16 << 20
)

var (
	ErrNilDecoder                 = errors.New("policy decoder is nil")
	ErrMissingDecoder             = errors.New("policy decoder is not registered")
	ErrNilEvaluator               = errors.New("policy decoder returned a nil evaluator")
	ErrDocumentLimitExceeded      = errors.New("policy document size limit exceeded")
	ErrPolicyLimitExceeded        = errors.New("policy compiler policy limit exceeded")
	ErrTotalDocumentLimitExceeded = errors.New("policy compiler total document size limit exceeded")
)

// Decoder converts one validated model document into an immutable evaluator.
type Decoder interface {
	Decode(json.RawMessage) (authorization.Evaluator, error)
}

type DecoderFunc func(json.RawMessage) (authorization.Evaluator, error)

func (decode DecoderFunc) Decode(
	document json.RawMessage,
) (authorization.Evaluator, error) {
	return decode(document)
}

type CompilerLimits struct {
	MaxDocumentBytes      int
	MaxPolicies           int
	MaxTotalDocumentBytes int
}

type CompilerOption func(*Compiler)

func WithCompilerLimits(limits CompilerLimits) CompilerOption {
	return func(compiler *Compiler) {
		if limits.MaxDocumentBytes > 0 {
			compiler.limits.MaxDocumentBytes = limits.MaxDocumentBytes
		}
		if limits.MaxPolicies > 0 {
			compiler.limits.MaxPolicies = limits.MaxPolicies
		}
		if limits.MaxTotalDocumentBytes > 0 {
			compiler.limits.MaxTotalDocumentBytes = limits.MaxTotalDocumentBytes
		}
	}
}

// Compiler activates portable manifests through an explicit model decoder
// registry. The registry is copied at construction and is safe for concurrent
// Compile calls when its decoders are safe for concurrent use.
type Compiler struct {
	decoders map[Model]Decoder
	limits   CompilerLimits
}

func NewCompiler(
	decoders map[Model]Decoder,
	options ...CompilerOption,
) (*Compiler, error) {
	compiler := &Compiler{
		decoders: make(map[Model]Decoder, len(decoders)),
		limits: CompilerLimits{
			MaxDocumentBytes:      defaultMaxDocumentBytes,
			MaxPolicies:           defaultMaxPolicies,
			MaxTotalDocumentBytes: defaultMaxTotalDocumentBytes,
		},
	}
	for model, decoder := range decoders {
		if decoder == nil {
			return nil, fmt.Errorf("model %q: %w", model, ErrNilDecoder)
		}
		compiler.decoders[model] = decoder
	}
	for _, option := range options {
		option(compiler)
	}
	return compiler, nil
}

func (compiler *Compiler) Compile(
	manifest Manifest,
) (*authorization.Snapshot, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if len(manifest.Policies) > compiler.limits.MaxPolicies {
		return nil, ErrPolicyLimitExceeded
	}

	definitions := make([]authorization.PolicyDefinition, len(manifest.Policies))
	totalDocumentBytes := 0
	for index, record := range manifest.Policies {
		if len(record.Document) > compiler.limits.MaxDocumentBytes {
			return nil, fmt.Errorf("policy %q: %w", record.ID, ErrDocumentLimitExceeded)
		}
		if len(record.Document) > compiler.limits.MaxTotalDocumentBytes-totalDocumentBytes {
			return nil, ErrTotalDocumentLimitExceeded
		}
		totalDocumentBytes += len(record.Document)
		decoder, exists := compiler.decoders[record.Model]
		if !exists {
			return nil, fmt.Errorf("policy %q model %q: %w", record.ID, record.Model, ErrMissingDecoder)
		}
		evaluator, err := decoder.Decode(record.Document)
		if err != nil {
			return nil, fmt.Errorf("policy %q model %q: %w", record.ID, record.Model, err)
		}
		if evaluator == nil {
			return nil, fmt.Errorf("policy %q model %q: %w", record.ID, record.Model, ErrNilEvaluator)
		}

		definition := authorization.PolicyDefinition{
			ID:        record.ID,
			Revision:  record.Revision,
			Priority:  record.Priority,
			Metadata:  record.Metadata,
			Evaluator: evaluator,
		}
		if record.ActiveFrom != nil {
			definition.ActiveFrom = *record.ActiveFrom
		}
		if record.ActiveUntil != nil {
			definition.ActiveUntil = *record.ActiveUntil
		}
		definitions[index] = definition
	}

	algorithms := map[Algorithm]authorization.CombiningAlgorithm{
		AlgorithmDenyOverrides:   authorization.DenyOverrides,
		AlgorithmAllowOverrides:  authorization.AllowOverrides,
		AlgorithmFirstApplicable: authorization.FirstApplicable,
		AlgorithmPriorityOrder:   authorization.PriorityOrder,
	}
	return authorization.NewSnapshot(
		manifest.Revision,
		algorithms[manifest.Algorithm],
		definitions...,
	)
}
