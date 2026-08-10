package httpsignature

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dunglas/httpsfv"
)

var (
	// ErrInvalidSignatureInput reports a malformed or semantically invalid
	// Signature-Input field.
	ErrInvalidSignatureInput = errors.New("http signature: invalid Signature-Input field")
	// ErrInvalidSignature reports a malformed or semantically invalid Signature
	// field.
	ErrInvalidSignature = errors.New("http signature: invalid Signature field")
	// ErrInvalidAcceptSignature reports a malformed or semantically invalid
	// Accept-Signature field.
	ErrInvalidAcceptSignature = errors.New("http signature: invalid Accept-Signature field")
)

// SFToken is an RFC 8941 Structured Fields Token parameter value.
type SFToken string

// Parameter is an ordered Structured Fields parameter. Value is one of bool,
// string, int64, float64, []byte, or SFToken. Byte values are always copied at
// API boundaries.
type Parameter struct {
	Name  string
	Value any
}

// ComponentIdentifier identifies one ordered covered message component.
type ComponentIdentifier struct {
	Name       string
	Parameters []Parameter
}

// Parameter returns a copy of the named component parameter.
func (component ComponentIdentifier) Parameter(name string) (any, bool) {
	return parameter(component.Parameters, name)
}

// SignatureInput is one labeled Signature-Input dictionary member.
type SignatureInput struct {
	Label      string
	Components []ComponentIdentifier
	Parameters []Parameter
}

// Parameter returns a copy of the named signature parameter.
func (input SignatureInput) Parameter(name string) (any, bool) {
	return parameter(input.Parameters, name)
}

// SignatureInputs is an immutable ordered Signature-Input field.
type SignatureInputs struct {
	entries []SignatureInput
}

// Entries returns a deep copy in combined-field wire order.
func (inputs SignatureInputs) Entries() []SignatureInput {
	entries := make([]SignatureInput, len(inputs.entries))
	for index, entry := range inputs.entries {
		entries[index] = cloneSignatureInput(entry)
	}

	return entries
}

// String returns the canonical Structured Fields serialization.
func (inputs SignatureInputs) String() string {
	dictionary := httpsfv.NewDictionary()
	for _, entry := range inputs.entries {
		items := make([]httpsfv.Item, len(entry.Components))
		for index, component := range entry.Components {
			items[index] = httpsfv.NewItem(component.Name)
			addParameters(items[index].Params, component.Parameters)
		}

		parameters := httpsfv.NewParams()
		addParameters(parameters, entry.Parameters)
		dictionary.Add(entry.Label, httpsfv.InnerList{Items: items, Params: parameters})
	}

	value, err := httpsfv.Marshal(dictionary)
	if err != nil {
		panic(fmt.Errorf("serialize validated Signature-Input: %w", err))
	}

	return value
}

// ParseSignatureInputs parses and semantically validates combined
// Signature-Input field lines. It preserves dictionary, component, and
// parameter order and rejects duplicate labels before Structured Fields
// dictionary replacement semantics could hide them.
func ParseSignatureInputs(values []string) (SignatureInputs, error) {
	return ParseSignatureInputsWithLimits(values, DefaultSyntaxLimits())
}

// ParseSignatureInputsWithLimits parses Signature-Input under explicit
// resource bounds.
func ParseSignatureInputsWithLimits(values []string, limits SyntaxLimits) (SignatureInputs, error) {
	entries, err := parseSignatureMetadata(values, limits, ErrInvalidSignatureInput, validSignatureParameters)
	if err != nil {
		return SignatureInputs{}, err
	}

	return SignatureInputs{entries: entries}, nil
}

func parseSignatureMetadata(
	values []string,
	limits SyntaxLimits,
	invalidError error,
	validateParameters func([]Parameter) bool,
) ([]SignatureInput, error) {
	if err := enforceRawSyntaxLimits(values, limits); err != nil {
		if errors.Is(err, ErrInvalidSyntaxLimits) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", invalidError, ErrSyntaxLimit)
	}
	if err := rejectDuplicateDictionaryKeys(values); err != nil {
		return nil, fmt.Errorf("%w: %v", invalidError, err)
	}

	dictionary, err := unmarshalStructuredDictionary(normalizeStructuredFieldOWS(values))
	if err != nil || len(dictionary.Names()) == 0 {
		return nil, fmt.Errorf("%w: malformed structured field", invalidError)
	}
	if len(dictionary.Names()) > limits.MaxDictionaryMembers {
		return nil, fmt.Errorf("%w: %w", invalidError, ErrSyntaxLimit)
	}

	entries := make([]SignatureInput, 0, len(dictionary.Names()))
	for _, label := range dictionary.Names() {
		member, _ := dictionary.Get(label)
		innerList, ok := member.(httpsfv.InnerList)
		if !ok {
			return nil, fmt.Errorf("%w: label %s is not an inner list", invalidError, label)
		}

		components := make([]ComponentIdentifier, 0, len(innerList.Items))
		if len(innerList.Items) > limits.MaxComponentsPerSignature || len(innerList.Params.Names()) > limits.MaxParametersPerItem {
			return nil, fmt.Errorf("%w: %w", invalidError, ErrSyntaxLimit)
		}
		seenComponents := make(map[string]struct{}, len(innerList.Items))
		for _, item := range innerList.Items {
			name, ok := item.Value.(string)
			if !ok || !validComponentName(name) {
				return nil, fmt.Errorf("%w: invalid component", invalidError)
			}

			parameters, paramErr := copyParameters(item.Params)
			if item.Params != nil && len(item.Params.Names()) > limits.MaxParametersPerItem {
				return nil, fmt.Errorf("%w: %w", invalidError, ErrSyntaxLimit)
			}
			if paramErr != nil || !validComponentParameters(parameters) {
				return nil, fmt.Errorf("%w: invalid component parameters", invalidError)
			}

			comparisonKey, _ := componentComparisonKey(ComponentIdentifier{Name: name, Parameters: parameters})
			if _, duplicate := seenComponents[comparisonKey]; duplicate {
				return nil, fmt.Errorf("%w: duplicate component", invalidError)
			}
			seenComponents[comparisonKey] = struct{}{}
			components = append(components, ComponentIdentifier{Name: name, Parameters: parameters})
		}

		parameters, paramErr := copyParameters(innerList.Params)
		if paramErr != nil || !validateParameters(parameters) {
			return nil, fmt.Errorf("%w: invalid signature parameters", invalidError)
		}

		entries = append(entries, SignatureInput{
			Label:      label,
			Components: components,
			Parameters: parameters,
		})
	}

	return entries, nil
}

// SignatureRequest is one requested signature from an Accept-Signature field.
type SignatureRequest = SignatureInput

// AcceptSignatures is an immutable ordered Accept-Signature field.
type AcceptSignatures struct {
	entries []SignatureInput
}

// ParseAcceptSignatures parses requested covered components and metadata. The
// created and expires parameters are Boolean requests rather than timestamps.
func ParseAcceptSignatures(values []string) (AcceptSignatures, error) {
	return ParseAcceptSignaturesWithLimits(values, DefaultSyntaxLimits())
}

// ParseAcceptSignaturesWithLimits parses Accept-Signature under explicit
// resource bounds.
func ParseAcceptSignaturesWithLimits(values []string, limits SyntaxLimits) (AcceptSignatures, error) {
	entries, err := parseSignatureMetadata(values, limits, ErrInvalidAcceptSignature, validAcceptSignatureParameters)
	if err != nil {
		return AcceptSignatures{}, err
	}

	return AcceptSignatures{entries: entries}, nil
}

// Entries returns a deep copy in combined-field wire order.
func (requests AcceptSignatures) Entries() []SignatureRequest {
	entries := make([]SignatureRequest, len(requests.entries))
	for index, entry := range requests.entries {
		entries[index] = cloneSignatureInput(entry)
	}

	return entries
}

// String returns the canonical Structured Fields serialization.
func (requests AcceptSignatures) String() string {
	return SignatureInputs(requests).String()
}

// SignatureValue is one labeled Signature dictionary member.
type SignatureValue struct {
	Label      string
	Value      []byte
	Parameters []Parameter
}

// Signatures is an immutable ordered Signature field.
type Signatures struct {
	entries []SignatureValue
}

// Entries returns a deep copy in combined-field wire order.
func (signatures Signatures) Entries() []SignatureValue {
	entries := make([]SignatureValue, len(signatures.entries))
	for index, entry := range signatures.entries {
		entries[index] = SignatureValue{
			Label:      entry.Label,
			Value:      append([]byte(nil), entry.Value...),
			Parameters: cloneParameters(entry.Parameters),
		}
	}

	return entries
}

// String returns the canonical Structured Fields serialization.
func (signatures Signatures) String() string {
	dictionary := httpsfv.NewDictionary()
	for _, entry := range signatures.entries {
		item := httpsfv.NewItem(append([]byte(nil), entry.Value...))
		addParameters(item.Params, entry.Parameters)
		dictionary.Add(entry.Label, item)
	}

	value, err := httpsfv.Marshal(dictionary)
	if err != nil {
		panic(fmt.Errorf("serialize validated Signature: %w", err))
	}

	return value
}

// ParseSignatures parses and semantically validates combined Signature field
// lines, preserving label order and rejecting duplicate labels.
func ParseSignatures(values []string) (Signatures, error) {
	return ParseSignaturesWithLimits(values, DefaultSyntaxLimits())
}

// ParseSignaturesWithLimits parses Signature under explicit resource bounds.
func ParseSignaturesWithLimits(values []string, limits SyntaxLimits) (Signatures, error) {
	if err := enforceRawSyntaxLimits(values, limits); err != nil {
		if errors.Is(err, ErrInvalidSyntaxLimits) {
			return Signatures{}, err
		}
		return Signatures{}, fmt.Errorf("%w: %w", ErrInvalidSignature, ErrSyntaxLimit)
	}
	if err := rejectDuplicateDictionaryKeys(values); err != nil {
		return Signatures{}, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	dictionary, err := unmarshalStructuredDictionary(normalizeStructuredFieldOWS(values))
	if err != nil || len(dictionary.Names()) == 0 {
		return Signatures{}, fmt.Errorf("%w: malformed structured field", ErrInvalidSignature)
	}
	if len(dictionary.Names()) > limits.MaxDictionaryMembers {
		return Signatures{}, fmt.Errorf("%w: %w", ErrInvalidSignature, ErrSyntaxLimit)
	}

	entries := make([]SignatureValue, 0, len(dictionary.Names()))
	for _, label := range dictionary.Names() {
		member, _ := dictionary.Get(label)
		item, ok := member.(httpsfv.Item)
		if !ok {
			return Signatures{}, fmt.Errorf("%w: label %s is not an item", ErrInvalidSignature, label)
		}

		value, ok := item.Value.([]byte)
		if !ok {
			return Signatures{}, fmt.Errorf("%w: label %s is not a byte sequence", ErrInvalidSignature, label)
		}

		parameters, paramErr := copyParameters(item.Params)
		if paramErr != nil {
			return Signatures{}, fmt.Errorf("%w: invalid parameters", ErrInvalidSignature)
		}
		if len(value) > limits.MaxBinaryBytes || len(parameters) > limits.MaxParametersPerItem {
			return Signatures{}, fmt.Errorf("%w: %w", ErrInvalidSignature, ErrSyntaxLimit)
		}

		entries = append(entries, SignatureValue{
			Label:      label,
			Value:      append([]byte(nil), value...),
			Parameters: parameters,
		})
	}

	return Signatures{entries: entries}, nil
}

func parameter(parameters []Parameter, name string) (any, bool) {
	for _, current := range parameters {
		if current.Name == name {
			return cloneParameterValue(current.Value), true
		}
	}

	return nil, false
}

func copyParameters(parameters *httpsfv.Params) ([]Parameter, error) {
	if parameters == nil {
		return nil, errors.New("missing parameters")
	}

	result := make([]Parameter, 0, len(parameters.Names()))
	for _, name := range parameters.Names() {
		value, _ := parameters.Get(name)
		converted, ok := fromStructuredValue(value)
		if !ok {
			return nil, errors.New("RFC 9651-only parameter value")
		}
		result = append(result, Parameter{Name: name, Value: converted})
	}

	return result, nil
}

func fromStructuredValue(value any) (any, bool) {
	switch typed := value.(type) {
	case bool, string, int64, float64:
		return typed, true
	case []byte:
		return append([]byte(nil), typed...), true
	case httpsfv.Token:
		return SFToken(typed), true
	default:
		return nil, false
	}
}

func addParameters(target *httpsfv.Params, parameters []Parameter) {
	for _, parameter := range parameters {
		value := parameter.Value
		if token, ok := value.(SFToken); ok {
			value = httpsfv.Token(token)
		}
		if binary, ok := value.([]byte); ok {
			value = append([]byte(nil), binary...)
		}
		target.Add(parameter.Name, value)
	}
}

func validSignatureParameters(parameters []Parameter) bool {
	for _, parameter := range parameters {
		switch parameter.Name {
		case "created", "expires":
			if _, ok := parameter.Value.(int64); !ok {
				return false
			}
		case "nonce", "alg", "keyid", "tag":
			if _, ok := parameter.Value.(string); !ok {
				return false
			}
		}
	}

	return true
}

func validAcceptSignatureParameters(parameters []Parameter) bool {
	for _, parameter := range parameters {
		switch parameter.Name {
		case "created", "expires":
			if value, ok := parameter.Value.(bool); !ok || !value {
				return false
			}
		case "nonce", "alg", "keyid", "tag":
			if _, ok := parameter.Value.(string); !ok {
				return false
			}
		}
	}

	return true
}

func validComponentParameters(parameters []Parameter) bool {
	for _, parameter := range parameters {
		switch parameter.Name {
		case "sf", "bs", "tr", "req":
			if value, ok := parameter.Value.(bool); !ok || !value {
				return false
			}
		case "key", "name":
			if _, ok := parameter.Value.(string); !ok {
				return false
			}
		}
	}

	return true
}

func validComponentName(name string) bool {
	if name == "" {
		return false
	}

	if name[0] == '@' {
		if len(name) == 1 {
			return false
		}
		for index := 1; index < len(name); index++ {
			character := name[index]
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				continue
			}

			return false
		}

		return true
	}

	for index := range len(name) {
		if !isLowerFieldNameCharacter(name[index]) {
			return false
		}
	}

	return true
}

func isLowerFieldNameCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
		strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))
}

func rejectDuplicateDictionaryKeys(values []string) error {
	members, err := splitDictionaryMembers(strings.Join(values, ","))
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		label := dictionaryMemberKey(member)
		if label == "" {
			return errors.New("invalid dictionary member")
		}
		if _, duplicate := seen[label]; duplicate {
			return errors.New("duplicate dictionary key")
		}
		seen[label] = struct{}{}
	}

	return nil
}

func splitDictionaryMembers(value string) ([]string, error) {
	var members []string
	start := 0
	parentheses := 0
	quoted := false
	escaped := false

	for index := range len(value) {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		switch character {
		case '(':
			parentheses++
		case ')':
			parentheses--
			if parentheses < 0 {
				return nil, errors.New("unbalanced inner list")
			}
		case ',':
			if parentheses == 0 {
				members = append(members, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}

	if quoted || escaped || parentheses != 0 {
		return nil, errors.New("unterminated dictionary member")
	}
	members = append(members, strings.TrimSpace(value[start:]))

	return members, nil
}

func dictionaryMemberKey(member string) string {
	index := 0
	for index < len(member) && (member[index] == ' ' || member[index] == '\t') {
		index++
	}
	start := index
	if index >= len(member) || !isKeyStart(member[index]) {
		return ""
	}
	index++
	for index < len(member) && isKeyCharacter(member[index]) {
		index++
	}

	return member[start:index]
}

func cloneSignatureInput(input SignatureInput) SignatureInput {
	components := make([]ComponentIdentifier, len(input.Components))
	for index, component := range input.Components {
		components[index] = ComponentIdentifier{
			Name:       component.Name,
			Parameters: cloneParameters(component.Parameters),
		}
	}

	return SignatureInput{
		Label:      input.Label,
		Components: components,
		Parameters: cloneParameters(input.Parameters),
	}
}

func componentComparisonKey(component ComponentIdentifier) (string, error) {
	parameters := cloneParameters(component.Parameters)
	slices.SortFunc(parameters, func(left, right Parameter) int { return cmp.Compare(left.Name, right.Name) })
	return serializeComponentIdentifier(ComponentIdentifier{Name: component.Name, Parameters: parameters})
}

func cloneParameters(parameters []Parameter) []Parameter {
	result := make([]Parameter, len(parameters))
	for index, parameter := range parameters {
		result[index] = Parameter{Name: parameter.Name, Value: cloneParameterValue(parameter.Value)}
	}

	return result
}

func cloneParameterValue(value any) any {
	if binary, ok := value.([]byte); ok {
		return append([]byte(nil), binary...)
	}

	return value
}

func validSFStringValue(value string) bool {
	for index := range len(value) {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}

	return true
}

func normalizeStructuredFieldOWS(values []string) []string {
	normalized := make([]string, len(values))
	for valueIndex, value := range values {
		buffer := []byte(value)
		for index, character := range buffer {
			if character != '\t' {
				continue
			}
			previous := index - 1
			for previous >= 0 && (buffer[previous] == ' ' || buffer[previous] == '\t') {
				previous--
			}
			next := index + 1
			for next < len(buffer) && (buffer[next] == ' ' || buffer[next] == '\t') {
				next++
			}
			if previous < 0 || buffer[previous] == ',' || next == len(buffer) || buffer[next] == ',' {
				buffer[index] = ' '
			}
		}
		normalized[valueIndex] = string(buffer)
	}
	return normalized
}
