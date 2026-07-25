package abac

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

const DocumentVersion uint64 = 1
const maxEncodedDocumentBytes = 1 << 20

const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

const (
	SourceSubject     = "subject"
	SourceResource    = "resource"
	SourceRequest     = "request"
	SourceEnvironment = "environment"
)

const (
	OperatorEqual          = "equal"
	OperatorExists         = "exists"
	OperatorIsNull         = "is_null"
	OperatorAll            = "all"
	OperatorAny            = "any"
	OperatorNot            = "not"
	OperatorGreaterThan    = "greater_than"
	OperatorLessThan       = "less_than"
	OperatorIn             = "in"
	OperatorSetContains    = "set_contains"
	OperatorHasPrefix      = "has_prefix"
	OperatorHasSuffix      = "has_suffix"
	OperatorStringContains = "string_contains"
	OperatorIPIn           = "ip_in"
)

const (
	ValueNull      = "null"
	ValueString    = "string"
	ValueBool      = "bool"
	ValueInt       = "int"
	ValueFloat     = "float"
	ValueTime      = "time"
	ValueIP        = "ip"
	ValueStringSet = "string_set"
)

var (
	ErrInvalidDocument            = errors.New("invalid ABAC document")
	ErrUnsupportedDocumentVersion = errors.New("unsupported ABAC document version")
	ErrDocumentLimitExceeded      = errors.New("ABAC document size limit exceeded")
)

type ValueDocument struct {
	Kind      string   `json:"kind"`
	String    string   `json:"string,omitempty"`
	Bool      bool     `json:"bool,omitempty"`
	Int       int64    `json:"int,omitempty"`
	Float     float64  `json:"float,omitempty"`
	Time      string   `json:"time,omitempty"`
	IP        string   `json:"ip,omitempty"`
	StringSet []string `json:"string_set,omitempty"`
}

func (document ValueDocument) build() (authorization.Value, error) {
	switch document.Kind {
	case ValueNull:
		return authorization.NullValue(), nil
	case ValueString:
		return authorization.StringValue(document.String), nil
	case ValueBool:
		return authorization.BoolValue(document.Bool), nil
	case ValueInt:
		return authorization.IntValue(document.Int), nil
	case ValueFloat:
		return authorization.FloatValue(document.Float)
	case ValueTime:
		value, err := time.Parse(time.RFC3339Nano, document.Time)
		if err != nil {
			return authorization.Value{}, ErrInvalidDocument
		}
		return authorization.TimeValue(value), nil
	case ValueIP:
		value, err := netip.ParseAddr(document.IP)
		if err != nil {
			return authorization.Value{}, ErrInvalidDocument
		}
		return authorization.IPValue(value), nil
	case ValueStringSet:
		return authorization.StringSetValue(document.StringSet), nil
	default:
		return authorization.Value{}, ErrInvalidDocument
	}
}

type ConditionDocument struct {
	Operator   string                      `json:"operator"`
	Source     string                      `json:"source,omitempty"`
	Attribute  authorization.AttributeName `json:"attribute,omitempty"`
	Value      *ValueDocument              `json:"value,omitempty"`
	Values     []ValueDocument             `json:"values,omitempty"`
	Conditions []ConditionDocument         `json:"conditions,omitempty"`
	Condition  *ConditionDocument          `json:"condition,omitempty"`
	Text       string                      `json:"text,omitempty"`
	Prefix     string                      `json:"prefix,omitempty"`
}

func (document ConditionDocument) build() (Condition, error) {
	switch document.Operator {
	case OperatorAll, OperatorAny:
		conditions := make([]Condition, len(document.Conditions))
		for index, child := range document.Conditions {
			condition, err := child.build()
			if err != nil {
				return nil, err
			}
			conditions[index] = condition
		}
		if document.Operator == OperatorAll {
			return All(conditions...), nil
		}
		return Any(conditions...), nil
	case OperatorNot:
		if document.Condition == nil {
			return nil, ErrInvalidDocument
		}
		condition, err := document.Condition.build()
		if err != nil {
			return nil, err
		}
		return Not(condition), nil
	}

	reference, err := document.reference()
	if err != nil {
		return nil, err
	}
	switch document.Operator {
	case OperatorExists:
		return Exists(reference), nil
	case OperatorIsNull:
		return IsNull(reference), nil
	case OperatorEqual, OperatorGreaterThan, OperatorLessThan:
		if document.Value == nil {
			return nil, ErrInvalidDocument
		}
		value, err := document.Value.build()
		if err != nil {
			return nil, err
		}
		if document.Operator == OperatorEqual {
			return Equal(reference, value), nil
		}
		if document.Operator == OperatorGreaterThan {
			return GreaterThan(reference, value), nil
		}
		return LessThan(reference, value), nil
	case OperatorIn:
		values := make([]authorization.Value, len(document.Values))
		for index, valueDocument := range document.Values {
			value, err := valueDocument.build()
			if err != nil {
				return nil, err
			}
			values[index] = value
		}
		return In(reference, values...), nil
	case OperatorSetContains:
		return SetContains(reference, document.Text), nil
	case OperatorHasPrefix:
		return HasPrefix(reference, document.Text), nil
	case OperatorHasSuffix:
		return HasSuffix(reference, document.Text), nil
	case OperatorStringContains:
		return StringContains(reference, document.Text), nil
	case OperatorIPIn:
		prefix, err := netip.ParsePrefix(document.Prefix)
		if err != nil {
			return nil, ErrInvalidDocument
		}
		return IPIn(reference, prefix), nil
	default:
		return nil, ErrInvalidDocument
	}
}

func (document ConditionDocument) reference() (Reference, error) {
	var source Source
	switch document.Source {
	case SourceSubject:
		source = Subject
	case SourceResource:
		source = Resource
	case SourceRequest:
		source = Request
	case SourceEnvironment:
		source = Environment
	default:
		return Reference{}, ErrInvalidDocument
	}
	if document.Attribute == "" {
		return Reference{}, ErrInvalidDocument
	}
	return Reference{Source: source, Name: document.Attribute}, nil
}

type RuleDocument struct {
	ID               authorization.PolicyID     `json:"id"`
	Priority         int                        `json:"priority,omitempty"`
	Tenant           authorization.TenantID     `json:"tenant,omitempty"`
	Action           authorization.Action       `json:"action"`
	ResourceType     authorization.ResourceType `json:"resource_type"`
	ResourceID       authorization.ResourceID   `json:"resource_id,omitempty"`
	Effect           string                     `json:"effect"`
	Condition        *ConditionDocument         `json:"condition,omitempty"`
	ConditionName    string                     `json:"condition_name,omitempty"`
	ConditionVersion uint64                     `json:"condition_version,omitempty"`
}

type NamedConditionDocument struct {
	Name      string            `json:"name"`
	Version   uint64            `json:"version"`
	Condition ConditionDocument `json:"condition"`
}

type Document struct {
	Version         uint64                   `json:"version"`
	Limits          Limits                   `json:"limits,omitempty"`
	Rules           []RuleDocument           `json:"rules"`
	NamedConditions []NamedConditionDocument `json:"named_conditions"`
}

func (document Document) Build() (*Evaluator, error) {
	if document.Version != DocumentVersion {
		return nil, ErrUnsupportedDocumentVersion
	}
	limits := normalizedLimits(document.Limits)
	for index := range document.NamedConditions {
		if err := validateConditionDocumentLimits(&document.NamedConditions[index].Condition, limits); err != nil {
			return nil, fmt.Errorf("named condition %d: %w", index, err)
		}
	}
	for index := range document.Rules {
		if document.Rules[index].Condition == nil {
			continue
		}
		if err := validateConditionDocumentLimits(document.Rules[index].Condition, limits); err != nil {
			return nil, fmt.Errorf("rule %d: %w", index, err)
		}
	}
	named := make([]NamedCondition, len(document.NamedConditions))
	for index, definition := range document.NamedConditions {
		condition, err := definition.Condition.build()
		if err != nil {
			return nil, fmt.Errorf("named condition %d: %w", index, err)
		}
		named[index] = NamedCondition{
			Name: definition.Name, Version: definition.Version,
			Condition: condition,
		}
	}
	rules := make([]Rule, len(document.Rules))
	for index, ruleDocument := range document.Rules {
		effect, err := parseDocumentEffect(ruleDocument.Effect)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", index, err)
		}
		var condition Condition
		if ruleDocument.Condition != nil {
			condition, err = ruleDocument.Condition.build()
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", index, err)
			}
		}
		rules[index] = Rule{
			ID: ruleDocument.ID, Priority: ruleDocument.Priority,
			Tenant: ruleDocument.Tenant, Action: ruleDocument.Action,
			ResourceType: ruleDocument.ResourceType,
			ResourceID:   ruleDocument.ResourceID, Effect: effect,
			Condition: condition, ConditionName: ruleDocument.ConditionName,
			ConditionVersion: ruleDocument.ConditionVersion,
		}
	}
	return New(rules, named, WithLimits(document.Limits))
}

func validateConditionDocumentLimits(root *ConditionDocument, limits Limits) error {
	type frame struct {
		condition *ConditionDocument
		depth     int
	}
	stack := []frame{{condition: root, depth: 1}}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.depth > limits.MaxDepth {
			return ErrDepthExceeded
		}

		children := make([]*ConditionDocument, 0)
		switch current.condition.Operator {
		case OperatorAll, OperatorAny:
			for index := range current.condition.Conditions {
				children = append(children, &current.condition.Conditions[index])
			}
		case OperatorNot:
			if current.condition.Condition != nil {
				children = append(children, current.condition.Condition)
			}
		}
		for index := len(children) - 1; index >= 0; index-- {
			stack = append(stack, frame{condition: children[index], depth: current.depth + 1})
		}
	}
	return nil
}

func DecodeDocument(encoded []byte) (*Evaluator, error) {
	if len(encoded) > maxEncodedDocumentBytes {
		return nil, ErrDocumentLimitExceeded
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidDocument
	}
	return document.Build()
}

func EncodeDocument(document Document) ([]byte, error) {
	if _, err := document.Build(); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if len(encoded) > maxEncodedDocumentBytes {
		return nil, ErrDocumentLimitExceeded
	}
	return encoded, err
}

type Decoder struct{}

func (Decoder) Decode(document json.RawMessage) (authorization.Evaluator, error) {
	return DecodeDocument(document)
}

func parseDocumentEffect(effect string) (authorization.Outcome, error) {
	switch effect {
	case EffectAllow:
		return authorization.Allow, nil
	case EffectDeny:
		return authorization.Deny, nil
	default:
		return authorization.NotApplicable, ErrInvalidDocument
	}
}
