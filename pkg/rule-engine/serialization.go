package ruleengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"sort"
	"time"
)

const definitionVersion = "1"

type jsonDefinition struct {
	Version   string     `json:"version"`
	ID        string     `json:"id"`
	Namespace string     `json:"namespace,omitempty"`
	Strategy  string     `json:"strategy"`
	Rules     []jsonRule `json:"rules"`
}

type jsonRule struct {
	ID        RuleID     `json:"id"`
	Namespace string     `json:"namespace,omitempty"`
	Priority  int        `json:"priority"`
	Tags      []string   `json:"tags"`
	When      jsonNode   `json:"when"`
	Derive    []jsonFact `json:"derive"`
}

type jsonNode struct {
	Kind     string       `json:"kind"`
	Operator OperatorName `json:"operator,omitempty"`
	Path     []string     `json:"path,omitempty"`
	Left     *jsonOperand `json:"left,omitempty"`
	Right    *jsonOperand `json:"right,omitempty"`
	Child    *jsonNode    `json:"child,omitempty"`
	Children []jsonNode   `json:"children,omitempty"`
}

type jsonOperand struct {
	Kind  string     `json:"kind"`
	Path  []string   `json:"path,omitempty"`
	Value *jsonValue `json:"value,omitempty"`
}

type jsonFact struct {
	Path  []string  `json:"path"`
	Owner string    `json:"owner"`
	Value jsonValue `json:"value"`
}

type jsonValue struct {
	Type     string      `json:"type"`
	Bool     *bool       `json:"bool,omitempty"`
	Int      *int64      `json:"int,omitempty"`
	Float    *float64    `json:"float,omitempty"`
	String   *string     `json:"string,omitempty"`
	Time     *string     `json:"time,omitempty"`
	Duration *int64      `json:"duration,omitempty"`
	List     []jsonValue `json:"list,omitempty"`
}

// MarshalCanonical serializes a definition with stable ordering and field
// representation. Custom predicates cannot be serialized.
func MarshalCanonical(set RuleSet) ([]byte, error) {
	plan, _, err := NewCompiler(DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		return nil, err
	}
	definition := jsonDefinition{
		Version: definitionVersion,
		ID:      set.ID, Namespace: set.Namespace,
		Strategy: strategyName(set.Strategy),
		Rules:    make([]jsonRule, len(plan.rules)),
	}
	for index, rule := range plan.rules {
		encodedRule, encodeErr := encodeRule(rule)
		if encodeErr != nil {
			return nil, encodeErr
		}
		definition.Rules[index] = encodedRule
	}
	return json.Marshal(definition)
}

// CanonicalHash returns the lowercase SHA-256 digest of MarshalCanonical.
func CanonicalHash(set RuleSet) (string, error) {
	encoded, err := MarshalCanonical(set)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ParseJSON parses the versioned JSON AST with strict unknown-field handling.
func ParseJSON(data []byte, limits Limits) (RuleSet, []Diagnostic, error) {
	if err := limits.validate(); err != nil {
		return RuleSet{}, nil, err
	}
	if len(data) == 0 || len(data) > limits.MaxDefinitionBytes {
		return parseFailure(CodeLimitExceeded, "JSON definition size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definition jsonDefinition
	if err := decoder.Decode(&definition); err != nil {
		return parseFailure(CodeInvalidJSON, "JSON definition is invalid")
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return parseFailure(CodeInvalidJSON, "JSON definition has trailing data")
	}
	if definition.Version != definitionVersion {
		return parseFailure(CodeInvalidJSON, "JSON definition version is unsupported")
	}
	strategy, err := parseStrategy(definition.Strategy)
	if err != nil {
		return parseFailure(CodeInvalidJSON, "conflict strategy is invalid")
	}
	set := RuleSet{ID: definition.ID, Namespace: definition.Namespace, Strategy: strategy, Rules: make([]Rule, len(definition.Rules))}
	for index, encodedRule := range definition.Rules {
		rule, parseErr := decodeRule(encodedRule, limits)
		if parseErr != nil {
			return parseFailureForRule(encodedRule.ID, parseErr)
		}
		set.Rules[index] = rule
	}
	_, diagnostics, err := NewCompiler(limits).Compile(context.Background(), set)
	if err != nil {
		return RuleSet{}, diagnostics, err
	}
	return set, nil, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	return newError(CodeInvalidJSON, "trailing JSON value")
}

func parseFailure(code Code, message string) (RuleSet, []Diagnostic, error) {
	return parseFailureForRule(RuleID("rule-set"), newError(code, message))
}

func parseFailureForRule(ruleID RuleID, err error) (RuleSet, []Diagnostic, error) {
	code := errorCode(err, CodeInvalidJSON)
	diagnostic := Diagnostic{RuleID: ruleID, Code: code, Severity: SeverityError, Message: "definition could not be parsed"}
	return RuleSet{}, []Diagnostic{diagnostic}, err
}

func encodeRule(rule Rule) (jsonRule, error) {
	when, err := encodePredicate(rule.When)
	if err != nil {
		return jsonRule{}, err
	}
	tags := append([]string(nil), rule.Tags...)
	sort.Strings(tags)
	tags = slices.Compact(tags)
	encoded := jsonRule{ID: rule.ID, Namespace: rule.Namespace, Priority: rule.Priority, Tags: tags, When: when, Derive: make([]jsonFact, len(rule.Derive))}
	for index, fact := range rule.Derive {
		encoded.Derive[index] = jsonFact{Path: fact.Path.Segments(), Owner: ownerName(fact.Owner), Value: encodeValue(fact.Value)}
	}
	sort.Slice(encoded.Derive, func(left, right int) bool {
		return joinPath(encoded.Derive[left].Path) < joinPath(encoded.Derive[right].Path)
	})
	return encoded, nil
}

func encodePredicate(predicate Predicate) (jsonNode, error) {
	switch typed := predicate.(type) {
	case constantPredicate:
		if bool(typed) {
			return jsonNode{Kind: "true"}, nil
		}
		return jsonNode{Kind: "false"}, nil
	case existsPredicate:
		return jsonNode{Kind: "exists", Path: typed.path.Segments()}, nil
	case comparisonPredicate:
		left, err := encodeOperand(typed.left)
		if err != nil {
			return jsonNode{}, err
		}
		right, err := encodeOperand(typed.right)
		if err != nil {
			return jsonNode{}, err
		}
		return jsonNode{Kind: "compare", Operator: typed.operator, Left: &left, Right: &right}, nil
	case allPredicate:
		return encodeChildren("all", typed.children)
	case anyPredicate:
		return encodeChildren("any", typed.children)
	case notPredicate:
		child, err := encodePredicate(typed.child)
		return jsonNode{Kind: "not", Child: &child}, err
	default:
		return jsonNode{}, newError(CodeNotSerializable, "custom predicate cannot be serialized")
	}
}

func encodeChildren(kind string, predicates []Predicate) (jsonNode, error) {
	children := make([]jsonNode, len(predicates))
	for index, predicate := range predicates {
		child, err := encodePredicate(predicate)
		if err != nil {
			return jsonNode{}, err
		}
		children[index] = child
	}
	return jsonNode{Kind: kind, Children: children}, nil
}

func encodeOperand(operand Operand) (jsonOperand, error) {
	switch typed := operand.(type) {
	case variableOperand:
		return jsonOperand{Kind: "variable", Path: typed.path.Segments()}, nil
	case literalOperand:
		value := encodeValue(typed.value)
		return jsonOperand{Kind: "literal", Value: &value}, nil
	default:
		return jsonOperand{}, newError(CodeNotSerializable, "custom operand cannot be serialized")
	}
}

func encodeValue(value Value) jsonValue {
	encoded := jsonValue{Type: kindName(value.kind)}
	switch value.kind {
	case KindMissing, KindNull:
	case KindBool:
		item, _ := value.BoolValue()
		encoded.Bool = &item
	case KindInt:
		item, _ := value.IntValue()
		encoded.Int = &item
	case KindFloat:
		item, _ := value.FloatValue()
		encoded.Float = &item
	case KindString:
		item, _ := value.StringValue()
		encoded.String = &item
	case KindTime:
		item, _ := value.TimeValue()
		text := item.UTC().Format(time.RFC3339Nano)
		encoded.Time = &text
	case KindDuration:
		item, _ := value.DurationValue()
		nanos := int64(item)
		encoded.Duration = &nanos
	case KindList:
		items, _ := value.ListValue()
		encoded.List = make([]jsonValue, len(items))
		for index, item := range items {
			encoded.List[index] = encodeValue(item)
		}
	}
	return encoded
}

func decodeRule(encoded jsonRule, limits Limits) (Rule, error) {
	when, err := decodePredicate(encoded.When, limits, 1)
	if err != nil {
		return Rule{}, err
	}
	rule := Rule{ID: encoded.ID, Namespace: encoded.Namespace, Priority: encoded.Priority, Tags: append([]string(nil), encoded.Tags...), When: when, Derive: make([]Fact, len(encoded.Derive))}
	for index, encodedFact := range encoded.Derive {
		path, pathErr := NewPath(limits, encodedFact.Path...)
		if pathErr != nil {
			return Rule{}, pathErr
		}
		value, valueErr := decodeValue(encodedFact.Value, limits, 0)
		if valueErr != nil {
			return Rule{}, valueErr
		}
		owner, ownerErr := parseOwner(encodedFact.Owner)
		if ownerErr != nil {
			return Rule{}, ownerErr
		}
		rule.Derive[index] = Fact{Path: path, Owner: owner, Value: value}
	}
	return rule, nil
}

func decodePredicate(encoded jsonNode, limits Limits, depth int) (Predicate, error) {
	if depth > limits.MaxASTDepth {
		return nil, newError(CodeLimitExceeded, "JSON AST is too deep")
	}
	if !validNodeShape(encoded) {
		return nil, newError(CodeInvalidJSON, "predicate fields are ambiguous")
	}
	switch encoded.Kind {
	case "true":
		return True(), nil
	case "false":
		return False(), nil
	case "exists":
		path, err := NewPath(limits, encoded.Path...)
		if err != nil {
			return nil, err
		}
		return Exists(path), nil
	case "compare":
		left, err := decodeOperand(*encoded.Left, limits)
		if err != nil {
			return nil, err
		}
		right, err := decodeOperand(*encoded.Right, limits)
		if err != nil {
			return nil, err
		}
		return Compare(encoded.Operator, left, right), nil
	case "all", "any":
		children := make([]Predicate, len(encoded.Children))
		for index, child := range encoded.Children {
			parsed, err := decodePredicate(child, limits, depth+1)
			if err != nil {
				return nil, err
			}
			children[index] = parsed
		}
		if encoded.Kind == "all" {
			return All(children...), nil
		}
		return Any(children...), nil
	case "not":
		child, err := decodePredicate(*encoded.Child, limits, depth+1)
		if err != nil {
			return nil, err
		}
		return Not(child), nil
	default:
		return nil, newError(CodeInvalidJSON, "predicate kind is invalid")
	}
}

func decodeOperand(encoded jsonOperand, limits Limits) (Operand, error) {
	if !validOperandShape(encoded) {
		return nil, newError(CodeInvalidJSON, "operand fields are ambiguous")
	}
	switch encoded.Kind {
	case "variable":
		path, err := NewPath(limits, encoded.Path...)
		if err != nil {
			return nil, err
		}
		return Variable(path), nil
	case "literal":
		value, err := decodeValue(*encoded.Value, limits, 0)
		if err != nil {
			return nil, err
		}
		return Literal(value), nil
	default:
		return nil, newError(CodeInvalidJSON, "operand kind is invalid")
	}
}

func decodeValue(encoded jsonValue, limits Limits, depth int) (Value, error) {
	if depth > limits.MaxASTDepth {
		return Value{}, newError(CodeLimitExceeded, "value is too deep")
	}
	if !validValueShape(encoded) {
		return Value{}, newError(CodeInvalidJSON, "value fields are ambiguous")
	}
	var value Value
	switch encoded.Type {
	case "missing":
		value = Missing()
	case "null":
		value = Null()
	case "bool":
		value = Bool(*encoded.Bool)
	case "int":
		value = Int(*encoded.Int)
	case "float":
		value = Float(*encoded.Float)
	case "string":
		value = String(*encoded.String)
	case "time":
		parsed, err := time.Parse(time.RFC3339Nano, *encoded.Time)
		if err != nil {
			return Value{}, newError(CodeInvalidJSON, "time value is invalid")
		}
		value = Time(parsed)
	case "duration":
		value = Duration(time.Duration(*encoded.Duration))
	case "list":
		items := make([]Value, len(encoded.List))
		for index, item := range encoded.List {
			parsed, err := decodeValue(item, limits, depth+1)
			if err != nil {
				return Value{}, err
			}
			items[index] = parsed
		}
		value = List(items...)
	}
	if err := validateValue(value, limits, depth); err != nil {
		return Value{}, err
	}
	return value, nil
}

func validNodeShape(node jsonNode) bool {
	noOperands := node.Left == nil && node.Right == nil
	noTree := node.Child == nil && node.Children == nil
	switch node.Kind {
	case "true", "false":
		return node.Operator == "" && len(node.Path) == 0 && noOperands && noTree
	case "exists":
		return node.Operator == "" && len(node.Path) > 0 && noOperands && noTree
	case "compare":
		return node.Operator != "" && len(node.Path) == 0 && node.Left != nil &&
			node.Right != nil && noTree
	case "all", "any":
		return node.Operator == "" && len(node.Path) == 0 && noOperands &&
			node.Child == nil && node.Children != nil
	case "not":
		return node.Operator == "" && len(node.Path) == 0 && noOperands &&
			node.Child != nil && node.Children == nil
	default:
		return true
	}
}

func validOperandShape(operand jsonOperand) bool {
	switch operand.Kind {
	case "variable":
		return len(operand.Path) > 0 && operand.Value == nil
	case "literal":
		return len(operand.Path) == 0 && operand.Value != nil
	default:
		return true
	}
}

func validValueShape(value jsonValue) bool {
	scalarCount := 0
	for _, present := range []bool{
		value.Bool != nil, value.Int != nil, value.Float != nil,
		value.String != nil, value.Time != nil, value.Duration != nil,
	} {
		if present {
			scalarCount++
		}
	}
	switch value.Type {
	case "missing", "null":
		return scalarCount == 0 && value.List == nil
	case "bool":
		return value.Bool != nil && scalarCount == 1 && value.List == nil
	case "int":
		return value.Int != nil && scalarCount == 1 && value.List == nil
	case "float":
		return value.Float != nil && scalarCount == 1 && value.List == nil
	case "string":
		return value.String != nil && scalarCount == 1 && value.List == nil
	case "time":
		return value.Time != nil && scalarCount == 1 && value.List == nil
	case "duration":
		return value.Duration != nil && scalarCount == 1 && value.List == nil
	case "list":
		return scalarCount == 0
	default:
		return false
	}
}

func strategyName(strategy ConflictStrategy) string {
	if strategy == CollectAll {
		return "collect_all"
	}
	if strategy == ErrorOnMultiple {
		return "error_on_multiple"
	}
	return "first_match"
}
func parseStrategy(value string) (ConflictStrategy, error) {
	switch value {
	case "first_match":
		return FirstMatch, nil
	case "collect_all":
		return CollectAll, nil
	case "error_on_multiple":
		return ErrorOnMultiple, nil
	default:
		return 0, newError(CodeInvalidJSON, "invalid strategy")
	}
}
func ownerName(owner Owner) string {
	switch owner {
	case OwnerUnspecified:
		return "unspecified"
	case OwnerSubject:
		return "subject"
	case OwnerResource:
		return "resource"
	case OwnerEnvironment:
		return "environment"
	default:
		return "invalid"
	}
}
func parseOwner(value string) (Owner, error) {
	switch value {
	case "unspecified":
		return OwnerUnspecified, nil
	case "subject":
		return OwnerSubject, nil
	case "resource":
		return OwnerResource, nil
	case "environment":
		return OwnerEnvironment, nil
	default:
		return 0, newError(CodeInvalidJSON, "invalid owner")
	}
}
func kindName(kind Kind) string {
	names := [...]string{"missing", "null", "bool", "int", "float", "string", "time", "duration", "list"}
	if int(kind) >= len(names) {
		return "invalid"
	}
	return names[kind]
}
func joinPath(segments []string) string {
	var buffer bytes.Buffer
	for _, segment := range segments {
		buffer.WriteByte(0)
		buffer.WriteString(segment)
	}
	return buffer.String()
}
