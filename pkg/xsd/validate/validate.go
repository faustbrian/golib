package validate

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

var ErrLimitExceeded = errors.New("xsd validate: resource limit exceeded")

const schemaInstanceNamespace = "http://www.w3.org/2001/XMLSchema-instance"

const (
	defaultMaxBytes          = 64 << 20
	defaultMaxDepth          = 256
	defaultMaxTextBytes      = 16 << 20
	defaultMaxDiagnostics    = 1000
	defaultMaxNodes          = 1000000
	defaultMaxAttributes     = 1000000
	defaultMaxXPathSteps     = 1000000
	defaultMaxIdentityValues = 1000000
)

// Limits bounds work retained while validating one instance.
type Limits struct {
	MaxBytes          int64
	MaxDepth          int
	MaxTextBytes      int
	MaxDiagnostics    int
	MaxNodes          int
	MaxAttributes     int
	MaxXPathSteps     int
	MaxIdentityValues int
}

// Options configures a Validator.
type Options struct {
	SystemID string
	Limits   Limits
}

// Validator is immutable and safe for concurrent use.
type Validator struct {
	set      *compile.Set
	systemID string
	limits   Limits
}

// Result contains deterministic diagnostics in document order.
type Result struct {
	Valid       bool
	Diagnostics []xsd.Diagnostic
}

// New creates a validator for a compiled set.
func New(set *compile.Set, options Options) (*Validator, error) {
	if set == nil {
		return nil, fmt.Errorf("xsd validate: schema set is nil")
	}
	limits := options.Limits
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxTextBytes == 0 {
		limits.MaxTextBytes = defaultMaxTextBytes
	}
	if limits.MaxDiagnostics == 0 {
		limits.MaxDiagnostics = defaultMaxDiagnostics
	}
	if limits.MaxNodes == 0 {
		limits.MaxNodes = defaultMaxNodes
	}
	if limits.MaxAttributes == 0 {
		limits.MaxAttributes = defaultMaxAttributes
	}
	if limits.MaxXPathSteps == 0 {
		limits.MaxXPathSteps = defaultMaxXPathSteps
	}
	if limits.MaxIdentityValues == 0 {
		limits.MaxIdentityValues = defaultMaxIdentityValues
	}
	if limits.MaxBytes < 0 || limits.MaxDepth < 0 || limits.MaxTextBytes < 0 ||
		limits.MaxDiagnostics < 0 || limits.MaxNodes < 0 || limits.MaxAttributes < 0 ||
		limits.MaxXPathSteps < 0 || limits.MaxIdentityValues < 0 {
		return nil, fmt.Errorf("xsd validate: limits must not be negative")
	}
	return &Validator{set: set, systemID: options.SystemID, limits: limits}, nil
}

// Validate checks one XML instance without DTD, file, or network processing.
func (v *Validator) Validate(ctx context.Context, source []byte) (Result, error) {
	root, err := v.parseInstance(ctx, source)
	if err != nil {
		return Result{}, err
	}
	return v.validateRoot(root)
}

// ValidateReader validates one XML instance read incrementally from reader.
// Input, parser depth, retained nodes, attributes, text, and validation work
// remain bounded by the validator limits.
func (v *Validator) ValidateReader(ctx context.Context, reader io.Reader) (Result, error) {
	if reader == nil {
		return Result{}, errors.New("xsd validate: instance reader is nil")
	}
	root, err := v.parseInstanceReader(ctx, reader)
	if err != nil {
		return Result{}, err
	}
	return v.validateRoot(root)
}

// Node is a caller-owned expanded-name XML tree used by ValidateTree.
type Node struct {
	Name       xsd.QName
	Attributes map[xsd.QName]string
	Namespaces map[string]string
	Children   []Node
	Text       string
	Location   xsd.Location
}

// ValidateTree validates a bounded caller-provided tree without modifying it.
func (v *Validator) ValidateTree(ctx context.Context, root Node) (Result, error) {
	state := treeCloneState{validator: v}
	cloned, err := state.clone(ctx, root, 1)
	if err != nil {
		return Result{}, err
	}
	return v.validateRoot(cloned)
}

func (v *Validator) validateRoot(root *instanceNode) (Result, error) {
	name := root.Name
	element, ok := v.set.Element(name)
	state := validationState{validator: v}
	path := "/" + name.Local
	var err error
	if !ok {
		_, hasInstanceType := root.Attributes[xsd.QName{
			Namespace: schemaInstanceNamespace,
			Local:     "type",
		}]
		if hasInstanceType {
			err = state.validateElement(root, xsd.Element{}, path)
		} else {
			err = state.add(root.Location, path, "cvc-elt.1.a", fmt.Sprintf(
				"no global declaration for {%s}%s",
				name.Namespace,
				name.Local,
			))
		}
	} else if element.Abstract {
		err = state.add(root.Location, path, "cvc-elt.2", fmt.Sprintf(
			"element {%s}%s is abstract",
			name.Namespace,
			name.Local,
		))
	} else {
		err = state.validateElement(root, element, path)
	}
	if err != nil {
		return Result{}, err
	}
	if err := state.validateIDReferences(); err != nil {
		return Result{}, err
	}
	return Result{
		Valid:       len(state.diagnostics) == 0,
		Diagnostics: state.diagnostics,
	}, nil
}

type treeCloneState struct {
	validator  *Validator
	nodes      int
	attributes int
	textBytes  int
}

func (s *treeCloneState) clone(
	ctx context.Context,
	source Node,
	depth int,
) (*instanceNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > s.validator.limits.MaxDepth {
		return nil, fmt.Errorf(
			"%w: element depth exceeds %d",
			ErrLimitExceeded,
			s.validator.limits.MaxDepth,
		)
	}
	s.nodes++
	if s.nodes > s.validator.limits.MaxNodes {
		return nil, fmt.Errorf(
			"%w: node count exceeds %d",
			ErrLimitExceeded,
			s.validator.limits.MaxNodes,
		)
	}
	if source.Name.Local == "" {
		return nil, fmt.Errorf("xsd validate: tree node has no local name")
	}
	s.textBytes += len(source.Text)
	if s.textBytes > s.validator.limits.MaxTextBytes {
		return nil, fmt.Errorf(
			"%w: text bytes exceed %d",
			ErrLimitExceeded,
			s.validator.limits.MaxTextBytes,
		)
	}
	node := &instanceNode{
		Name:           source.Name,
		Attributes:     make(map[xsd.QName]string, len(source.Attributes)),
		AttributeTypes: make(map[xsd.QName]xsd.QName),
		Namespaces:     cloneNamespaces(source.Namespaces),
		Children:       make([]*instanceNode, 0, len(source.Children)),
		Text:           source.Text,
		Location:       source.Location,
	}
	for name, value := range source.Attributes {
		s.attributes++
		if s.attributes > s.validator.limits.MaxAttributes {
			return nil, fmt.Errorf(
				"%w: attribute count exceeds %d",
				ErrLimitExceeded,
				s.validator.limits.MaxAttributes,
			)
		}
		if name.Local == "" {
			return nil, fmt.Errorf("xsd validate: tree attribute has no local name")
		}
		node.Attributes[name] = value
	}
	for _, child := range source.Children {
		cloned, err := s.clone(ctx, child, depth+1)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, cloned)
	}
	return node, nil
}

type instanceNode struct {
	Name           xsd.QName
	Attributes     map[xsd.QName]string
	Type           xsd.QName
	Nillable       bool
	AttributeTypes map[xsd.QName]xsd.QName
	Namespaces     map[string]string
	Children       []*instanceNode
	Text           string
	Location       xsd.Location
}

func (v *Validator) parseInstance(ctx context.Context, source []byte) (*instanceNode, error) {
	if int64(len(source)) > v.limits.MaxBytes {
		return nil, fmt.Errorf("%w: instance bytes exceed %d", ErrLimitExceeded, v.limits.MaxBytes)
	}
	return v.parseInstanceReader(ctx, bytes.NewReader(source))
}

func (v *Validator) parseInstanceReader(ctx context.Context, reader io.Reader) (*instanceNode, error) {
	readLimit := v.limits.MaxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	limited := &io.LimitedReader{
		R: &contextReader{ctx: ctx, reader: reader},
		N: readLimit,
	}
	decoder := xml.NewDecoder(limited)
	decoder.Strict = true
	decoder.Entity = map[string]string{}

	var root *instanceNode
	var stack []*instanceNode
	textBytes := 0
	nodes := 0
	attributes := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			if limited.N == 0 && v.limits.MaxBytes < math.MaxInt64 {
				return nil, fmt.Errorf("%w: instance bytes exceed %d", ErrLimitExceeded, v.limits.MaxBytes)
			}
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, parseError(decoder, v.systemID, err)
		}
		if decoder.InputOffset() > v.limits.MaxBytes {
			return nil, fmt.Errorf("%w: instance bytes exceed %d", ErrLimitExceeded, v.limits.MaxBytes)
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, parseError(decoder, v.systemID, xsd.ErrDTDForbidden)
		case xml.StartElement:
			if len(stack)+1 > v.limits.MaxDepth {
				return nil, fmt.Errorf("%w: element depth exceeds %d", ErrLimitExceeded, v.limits.MaxDepth)
			}
			nodes++
			if nodes > v.limits.MaxNodes {
				return nil, fmt.Errorf("%w: node count exceeds %d", ErrLimitExceeded, v.limits.MaxNodes)
			}
			line, column := decoder.InputPos()
			var parentNamespaces map[string]string
			if len(stack) > 0 {
				parentNamespaces = stack[len(stack)-1].Namespaces
			}
			node := &instanceNode{
				Name:           xsd.QName{Namespace: value.Name.Space, Local: value.Name.Local},
				Attributes:     make(map[xsd.QName]string),
				AttributeTypes: make(map[xsd.QName]xsd.QName),
				Namespaces:     cloneNamespaces(parentNamespaces),
				Location: xsd.Location{
					SystemID: v.systemID,
					Line:     line,
					Column:   column,
					Offset:   decoder.InputOffset(),
				},
			}
			for _, attribute := range value.Attr {
				if isNamespaceDeclaration(attribute.Name) {
					prefix := attribute.Name.Local
					if attribute.Name.Space == "" {
						prefix = ""
					}
					node.Namespaces[prefix] = attribute.Value
					continue
				}
				attributes++
				if attributes > v.limits.MaxAttributes {
					return nil, fmt.Errorf(
						"%w: attribute count exceeds %d",
						ErrLimitExceeded,
						v.limits.MaxAttributes,
					)
				}
				name := xsd.QName{Namespace: attribute.Name.Space, Local: attribute.Name.Local}
				if _, duplicate := node.Attributes[name]; duplicate {
					return nil, parseError(decoder, v.systemID, fmt.Errorf(
						"duplicate expanded attribute {%s}%s",
						name.Namespace,
						name.Local,
					))
				}
				node.Attributes[name] = attribute.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, parseError(decoder, v.systemID, fmt.Errorf("multiple root elements"))
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			textBytes += len(value)
			if textBytes > v.limits.MaxTextBytes {
				return nil, fmt.Errorf("%w: text bytes exceed %d", ErrLimitExceeded, v.limits.MaxTextBytes)
			}
			stack[len(stack)-1].Text += string(value)
		}
	}
	if root == nil {
		return nil, parseError(decoder, v.systemID, io.ErrUnexpectedEOF)
	}
	return root, nil
}

func cloneNamespaces(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for prefix, namespace := range source {
		clone[prefix] = namespace
	}
	return clone
}

func validatorElementDefaultSet(element xsd.Element) bool {
	return element.DefaultSet || element.Default != ""
}

func validatorElementFixedSet(element xsd.Element) bool {
	return element.FixedSet || element.Fixed != ""
}

func validatorAttributeDefaultSet(attribute xsd.AttributeUse) bool {
	return attribute.DefaultSet || attribute.Default != ""
}

func validatorAttributeFixedSet(attribute xsd.AttributeUse) bool {
	return attribute.FixedSet || attribute.Fixed != ""
}

func isNamespaceDeclaration(name xml.Name) bool {
	return (name.Space == "" && name.Local == "xmlns") || name.Space == "xmlns"
}

type validationState struct {
	validator      *Validator
	diagnostics    []xsd.Diagnostic
	xpathSteps     int
	identityValues int
	identityTables map[*instanceNode]map[xsd.QName]identityNodeTable
	ids            map[string]struct{}
	idReferences   []idReference
}

type identityNodeTable map[string]*instanceNode

type idReference struct {
	value    string
	location xsd.Location
	path     string
}

func (s *validationState) add(
	location xsd.Location,
	path string,
	code string,
	message string,
) error {
	if len(s.diagnostics) >= s.validator.limits.MaxDiagnostics {
		return fmt.Errorf(
			"%w: diagnostics exceed %d",
			ErrLimitExceeded,
			s.validator.limits.MaxDiagnostics,
		)
	}
	s.diagnostics = append(s.diagnostics, xsd.Diagnostic{
		Severity: xsd.SeverityError,
		Code:     code,
		Message:  message,
		Path:     path,
		Location: location,
	})
	return nil
}

func (s *validationState) validateElement(
	node *instanceNode,
	element xsd.Element,
	path string,
) error {
	node.Nillable = element.Nillable
	if element.Abstract {
		return s.add(
			node.Location,
			path,
			"cvc-elt.2",
			"abstract element declaration cannot validate an instance element",
		)
	}
	if err := s.validateElementContent(node, element, path); err != nil {
		return err
	}
	return s.validateIdentityConstraints(node, element.IdentityConstraints, path)
}

func (s *validationState) validateElementContent(
	node *instanceNode,
	element xsd.Element,
	path string,
) error {
	nilledElement := false
	if nilValue, present := node.Attributes[xsd.QName{
		Namespace: schemaInstanceNamespace,
		Local:     "nil",
	}]; present {
		nilled, ok := parseSchemaBoolean(nilValue)
		if !ok {
			return s.add(
				node.Location,
				path,
				"cvc-datatype-valid.1.2.1",
				"xsi:nil is not a valid boolean",
			)
		}
		if !element.Nillable {
			return s.add(
				node.Location,
				path,
				"cvc-elt.3.1",
				"xsi:nil is present for a non-nillable element",
			)
		}
		if nilled {
			nilledElement = true
			if len(node.Children) > 0 || strings.TrimSpace(node.Text) != "" {
				return s.add(
					node.Location,
					path,
					"cvc-elt.3.2.1",
					"a nilled element must have empty content",
				)
			}
		}
	}

	effective := node
	if !nilledElement && len(node.Children) == 0 && node.Text == "" {
		value := element.Fixed
		if !validatorElementFixedSet(element) {
			value = element.Default
		}
		if validatorElementFixedSet(element) || validatorElementDefaultSet(element) {
			clone := *node
			clone.Text = value
			effective = &clone
		}
	}
	typeName := element.Type
	if lexical, present := node.Attributes[xsd.QName{
		Namespace: schemaInstanceNamespace,
		Local:     "type",
	}]; present {
		override, ok := resolveInstanceQName(lexical, node.Namespaces)
		if !ok || !s.typeExists(override) {
			return s.add(node.Location, path, "cvc-elt.4.2", "xsi:type does not name a known type")
		}
		if complexType, exists := s.validator.set.ComplexType(override); exists && complexType.Abstract {
			return s.add(node.Location, path, "cvc-type.2", "xsi:type names an abstract type")
		}
		methods, derived := s.typeDerivationMethods(override, typeName)
		if typeName.Local != "" && !derived {
			return s.add(node.Location, path, "cvc-elt.4.3", "xsi:type is not validly derived from the declared type")
		}
		if s.derivationBlocked(methods, element, typeName) {
			return s.add(node.Location, path, "cvc-elt.4.3", "xsi:type derivation is blocked")
		}
		typeName = override
	}
	if nilledElement {
		if element.InlineComplexType != nil {
			return s.validateAttributes(
				node,
				element.TargetNamespace,
				element.InlineComplexType.Attributes,
				element.InlineComplexType.AttributeWildcard,
				path,
			)
		}
		if complexType, ok := s.validator.set.ComplexType(typeName); ok {
			return s.validateAttributes(
				node,
				typeName.Namespace,
				complexType.Attributes,
				complexType.AttributeWildcard,
				path,
			)
		}
		return s.validateSimpleAttributeSet(node, path)
	}
	if element.InlineSimpleType != nil {
		node.Type = element.InlineSimpleType.Base
		if !s.inlineSimpleLexicalValid(*element.InlineSimpleType, effective.Text) {
			return s.add(
				effective.Location,
				path,
				"cvc-datatype-valid.1.2.1",
				"value is not valid for the anonymous simple type",
			)
		}
		if !s.inlineSimpleContextValid(
			*element.InlineSimpleType,
			effective.Text,
			effective.Namespaces,
		) {
			return s.add(
				effective.Location,
				path,
				"cvc-datatype-valid.1.2.1",
				"value is not valid in the instance namespace context",
			)
		}
		return s.validateElementFixedConstraint(node, effective, element, element.InlineSimpleType.Base, nilledElement, path)
	}
	if element.InlineComplexType != nil {
		if err := s.validateComplex(
			effective,
			element.TargetNamespace,
			*element.InlineComplexType,
			path,
		); err != nil {
			return err
		}
		return s.validateElementFixedConstraint(node, effective, element, typeName, nilledElement, path)
	}
	if typeName.Local == "" {
		return s.validateAnyType(effective, path)
	}
	node.Type = typeName
	if typeName == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		return s.validateAnyType(effective, path)
	}
	var err error
	if typeName.Namespace == xsd.Namespace {
		err = s.validateSimple(effective, typeName, path)
	} else if _, ok := s.validator.set.SimpleType(typeName); ok {
		err = s.validateSimple(effective, typeName, path)
	} else if complexType, ok := s.validator.set.ComplexType(typeName); ok {
		err = s.validateComplex(effective, typeName.Namespace, complexType, path)
	} else {
		return s.add(node.Location, path, "src-resolve", fmt.Sprintf(
			"type {%s}%s is not defined",
			typeName.Namespace,
			typeName.Local,
		))
	}
	if err != nil {
		return err
	}
	return s.validateElementFixedConstraint(node, effective, element, typeName, nilledElement, path)
}

func (s *validationState) validateElementFixedConstraint(
	node *instanceNode,
	effective *instanceNode,
	element xsd.Element,
	typeName xsd.QName,
	nilledElement bool,
	path string,
) error {
	if !validatorElementFixedSet(element) || nilledElement {
		return nil
	}
	if len(node.Children) > 0 {
		return s.add(
			node.Location,
			path,
			"cvc-elt.5.2.2.2.1",
			"element with a fixed constraint contains child elements",
		)
	}
	if element.InlineComplexType != nil &&
		element.InlineComplexType.SimpleContent &&
		element.InlineComplexType.InlineSimpleType != nil {
		equal, compareErr := s.inlineSimpleValuesEqualContext(
			*element.InlineComplexType.InlineSimpleType,
			effective.Text,
			element.Fixed,
			effective.Namespaces,
			element.ValueNamespaces,
		)
		if compareErr != nil || equal {
			return compareErr
		}
		return s.add(
			node.Location,
			path,
			"cvc-elt.5.2.2.2.1",
			"element value does not match its fixed constraint",
		)
	}
	if element.InlineComplexType != nil && element.InlineComplexType.Mixed {
		if effective.Text == element.Fixed {
			return nil
		}
		return s.add(node.Location, path, "cvc-elt.5.2.2.2.1", "element value does not match its fixed constraint")
	}
	if complexType, ok := s.validator.set.ComplexType(typeName); ok && complexType.Mixed {
		if effective.Text == element.Fixed {
			return nil
		}
		return s.add(node.Location, path, "cvc-elt.5.2.2.2.1", "element value does not match its fixed constraint")
	}
	if typeName.Local == "" {
		if effective.Text == element.Fixed {
			return nil
		}
		return s.add(node.Location, path, "cvc-elt.5.2.2.2.1", "element value does not match its fixed constraint")
	}
	if complexType, ok := s.validator.set.ComplexType(typeName); ok &&
		complexType.SimpleContent {
		if complexType.InlineSimpleType != nil {
			equal, compareErr := s.inlineSimpleValuesEqualContext(
				*complexType.InlineSimpleType,
				effective.Text,
				element.Fixed,
				effective.Namespaces,
				element.ValueNamespaces,
			)
			if compareErr != nil || equal {
				return compareErr
			}
			return s.add(
				node.Location,
				path,
				"cvc-elt.5.2.2.2.1",
				"element value does not match its fixed constraint",
			)
		}
		typeName = complexType.SimpleBase
	}
	equal, compareErr := s.simpleValuesEqualContext(
		typeName,
		effective.Text,
		element.Fixed,
		effective.Namespaces,
		element.ValueNamespaces,
	)
	if compareErr != nil || equal {
		return compareErr
	}
	return s.add(
		node.Location,
		path,
		"cvc-elt.5.2.2.2.1",
		"element value does not match its fixed constraint",
	)
}

func (s *validationState) validateAnyType(node *instanceNode, path string) error {
	for _, child := range node.Children {
		declaration, declared := s.validator.set.Element(child.Name)
		if !declared {
			continue
		}
		if err := s.validateElement(child, declaration, path+"/"+child.Name.Local); err != nil {
			return err
		}
	}
	return nil
}

func (s *validationState) validateSimpleAttributeSet(node *instanceNode, path string) error {
	for _, name := range sortedAttributeNames(node.Attributes) {
		if permittedSchemaInstanceAttribute(name) {
			continue
		}
		if err := s.add(
			node.Location,
			path,
			"cvc-type.3.1.1",
			fmt.Sprintf("attribute {%s}%s is not allowed on simple content", name.Namespace, name.Local),
		); err != nil {
			return err
		}
	}
	return nil
}

func resolveInstanceQName(lexical string, namespaces map[string]string) (xsd.QName, bool) {
	lexical = strings.TrimSpace(lexical)
	if lexical == "" || strings.ContainsAny(lexical, " \t\r\n") {
		return xsd.QName{}, false
	}
	prefix, local, qualified := strings.Cut(lexical, ":")
	if !qualified {
		local = prefix
		prefix = ""
	}
	if local == "" || strings.Contains(local, ":") {
		return xsd.QName{}, false
	}
	namespace, ok := namespaces[prefix]
	if prefix != "" && !ok {
		return xsd.QName{}, false
	}
	return xsd.QName{Namespace: namespace, Local: local}, true
}

func (s *validationState) typeExists(name xsd.QName) bool {
	if name.Namespace == xsd.Namespace {
		return name.Local != ""
	}
	if _, ok := s.validator.set.SimpleType(name); ok {
		return true
	}
	_, ok := s.validator.set.ComplexType(name)
	return ok
}

func (s *validationState) typeDerivationMethods(
	derived xsd.QName,
	base xsd.QName,
) ([]xsd.Derivation, bool) {
	if base.Local == "" || base == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		return nil, true
	}
	methods := make([]xsd.Derivation, 0)
	current := derived
	for current != base {
		if complexType, ok := s.validator.set.ComplexType(current); ok {
			if complexType.Base.Local == "" || complexType.Derivation == "" {
				return nil, false
			}
			methods = append(methods, complexType.Derivation)
			current = complexType.Base
			continue
		}
		if simpleType, ok := s.validator.set.SimpleType(current); ok {
			method := xsd.Derivation(simpleType.Variety)
			next := simpleType.Base
			if simpleType.Variety == xsd.SimpleList ||
				simpleType.Variety == xsd.SimpleUnion {
				next = xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
			}
			methods = append(methods, method)
			current = next
			continue
		}
		if current.Namespace == xsd.Namespace {
			parent, method, ok := datatype.BuiltInDerivation(current.Local)
			if !ok {
				return nil, false
			}
			methods = append(methods, xsd.Derivation(method))
			current = xsd.QName{Namespace: xsd.Namespace, Local: parent}
			continue
		}
		return nil, false
	}
	return methods, true
}

func (s *validationState) derivationBlocked(
	methods []xsd.Derivation,
	element xsd.Element,
	declared xsd.QName,
) bool {
	var typeBlock xsd.DerivationSet
	if complexType, ok := s.validator.set.ComplexType(declared); ok {
		typeBlock = complexType.Block
	}
	for _, method := range methods {
		if element.Block.Contains(method) || typeBlock.Contains(method) {
			return true
		}
	}
	return false
}

func (s *validationState) inlineSimpleLexicalValid(
	typeDefinition xsd.SimpleType,
	lexical string,
) bool {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		baseValid := s.simpleLexicalValid(typeDefinition.Base, lexical)
		if typeDefinition.InlineBase != nil {
			baseValid = s.inlineSimpleLexicalValid(*typeDefinition.InlineBase, lexical)
		}
		return baseValid && s.facetsValid(typeDefinition, lexical)
	case xsd.SimpleList:
		items := strings.Fields(lexical)
		if len(items) == 0 {
			return false
		}
		for _, item := range items {
			valid := s.simpleLexicalValid(typeDefinition.ItemType, item)
			if typeDefinition.InlineItem != nil {
				valid = s.inlineSimpleLexicalValid(*typeDefinition.InlineItem, item)
			}
			if !valid {
				return false
			}
		}
		return true
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleLexicalValid(member, lexical) {
				return true
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineSimpleLexicalValid(member, lexical) {
				return true
			}
		}
	}
	return false
}

func (s *validationState) validateIdentityConstraints(
	node *instanceNode,
	constraints []xsd.IdentityConstraint,
	path string,
) error {
	if s.identityTables == nil {
		s.identityTables = make(map[*instanceNode]map[xsd.QName]identityNodeTable)
	}
	tables := make(map[xsd.QName]identityNodeTable)
	conflicts := make(map[xsd.QName]map[string]struct{})
	for _, child := range node.Children {
		for name, childTable := range s.identityTables[child] {
			mergeIdentityNodeTable(tables, conflicts, name, childTable)
		}
	}
	for _, constraint := range constraints {
		if constraint.Kind == xsd.IdentityKeyRef {
			continue
		}
		if err := s.consumeIdentityWork(node, constraint); err != nil {
			return err
		}
		table := make(identityNodeTable)
		duplicateTuples := make(map[string]struct{})
		selectedNodes := selectIdentityNodes(node, constraint)
		if err := s.consumeIdentityValues(len(selectedNodes)); err != nil {
			return err
		}
		for _, selected := range selectedNodes {
			tuple, complete, multiple := s.identityTuple(selected, constraint)
			if multiple {
				if err := s.add(
					selected.Location,
					path,
					"cvc-identity-constraint",
					"identity field selects more than one value",
				); err != nil {
					return err
				}
				continue
			}
			if !complete {
				if constraint.Kind == xsd.IdentityKey {
					if err := s.add(
						selected.Location,
						path,
						"cvc-identity-constraint",
						"key field has no value",
					); err != nil {
						return err
					}
				}
				continue
			}
			if constraint.Kind == xsd.IdentityKey &&
				s.identityKeySelectsNillable(selected, constraint) {
				if err := s.add(
					selected.Location,
					path,
					"cvc-identity-constraint",
					"key field selects an element declared as nillable",
				); err != nil {
					return err
				}
				continue
			}
			_, alreadyDuplicate := duplicateTuples[tuple]
			if _, duplicate := table[tuple]; duplicate || alreadyDuplicate {
				if err := s.add(
					selected.Location,
					path,
					"cvc-identity-constraint",
					fmt.Sprintf("identity constraint %s has a duplicate value", constraint.Name),
				); err != nil {
					return err
				}
				delete(table, tuple)
				duplicateTuples[tuple] = struct{}{}
				continue
			}
			table[tuple] = selected
		}
		name := xsd.QName{
			Namespace: constraint.TargetNamespace,
			Local:     constraint.Name,
		}
		mergeIdentityNodeTable(tables, conflicts, name, table)
	}
	for _, constraint := range constraints {
		if constraint.Kind != xsd.IdentityKeyRef {
			continue
		}
		if err := s.consumeIdentityWork(node, constraint); err != nil {
			return err
		}
		table := tables[constraint.Refer]
		selectedNodes := selectIdentityNodes(node, constraint)
		if err := s.consumeIdentityValues(len(selectedNodes)); err != nil {
			return err
		}
		for _, selected := range selectedNodes {
			tuple, complete, multiple := s.identityTuple(selected, constraint)
			if multiple {
				if err := s.add(
					selected.Location,
					path,
					"cvc-identity-constraint",
					"keyref field selects more than one value",
				); err != nil {
					return err
				}
				continue
			}
			if complete {
				_, found := table[tuple]
				if found {
					continue
				}
				if err := s.add(
					selected.Location,
					path,
					"cvc-identity-constraint",
					fmt.Sprintf("keyref %s has no matching key", constraint.Name),
				); err != nil {
					return err
				}
			}
		}
	}
	s.identityTables[node] = tables
	return nil
}

func mergeIdentityNodeTable(
	tables map[xsd.QName]identityNodeTable,
	conflicts map[xsd.QName]map[string]struct{},
	name xsd.QName,
	source identityNodeTable,
) {
	if tables[name] == nil {
		tables[name] = make(identityNodeTable)
	}
	if conflicts[name] == nil {
		conflicts[name] = make(map[string]struct{})
	}
	for tuple, node := range source {
		if _, conflicted := conflicts[name][tuple]; conflicted {
			continue
		}
		if previous, duplicate := tables[name][tuple]; duplicate && previous != node {
			delete(tables[name], tuple)
			conflicts[name][tuple] = struct{}{}
			continue
		}
		tables[name][tuple] = node
	}
}

func (s *validationState) consumeIdentityWork(
	node *instanceNode,
	constraint xsd.IdentityConstraint,
) error {
	paths := 0
	for _, branch := range strings.Split(constraint.Selector, "|") {
		paths += len(strings.Split(branch, "/"))
	}
	for _, field := range constraint.Fields {
		paths += len(strings.Split(field, "/"))
	}
	work := identityNodeCount(node) * paths
	if work > s.validator.limits.MaxXPathSteps-s.xpathSteps {
		return fmt.Errorf(
			"%w: identity XPath steps exceed %d",
			ErrLimitExceeded,
			s.validator.limits.MaxXPathSteps,
		)
	}
	s.xpathSteps += work
	return nil
}

func (s *validationState) consumeIdentityValues(count int) error {
	if count > s.validator.limits.MaxIdentityValues-s.identityValues {
		return fmt.Errorf(
			"%w: identity values exceed %d",
			ErrLimitExceeded,
			s.validator.limits.MaxIdentityValues,
		)
	}
	s.identityValues += count
	return nil
}

func identityNodeCount(node *instanceNode) int {
	count := 1
	for _, child := range node.Children {
		count += identityNodeCount(child)
	}
	return count
}

func selectIdentityNodes(
	context *instanceNode,
	constraint xsd.IdentityConstraint,
) []*instanceNode {
	result := make([]*instanceNode, 0)
	seen := make(map[*instanceNode]struct{})
	for _, branch := range strings.Split(xsd.NormalizeIdentityXPath(constraint.Selector), "|") {
		branch = strings.TrimSpace(branch)
		var selected []*instanceNode
		if branch == "." {
			selected = []*instanceNode{context}
		} else if strings.HasPrefix(branch, ".//") {
			steps := strings.Split(strings.TrimPrefix(branch, ".//"), "/")
			descendants := identityDescendants(context)
			if steps[0] == "." {
				selected = followIdentityElementPath(descendants, steps[1:], constraint.Namespaces)
			} else {
				for _, descendant := range descendants {
					if identityNameMatches(descendant.Name, steps[0], constraint.Namespaces) {
						selected = append(
							selected,
							followIdentityElementPath(
								[]*instanceNode{descendant},
								steps[1:],
								constraint.Namespaces,
							)...,
						)
					}
				}
			}
		} else {
			branch = strings.TrimPrefix(branch, "./")
			selected = followIdentityElementPath(
				[]*instanceNode{context},
				strings.Split(branch, "/"),
				constraint.Namespaces,
			)
		}
		for _, candidate := range selected {
			if _, duplicate := seen[candidate]; duplicate {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

func identityDescendants(node *instanceNode) []*instanceNode {
	result := make([]*instanceNode, 0)
	var visit func(*instanceNode)
	visit = func(parent *instanceNode) {
		for _, child := range parent.Children {
			result = append(result, child)
			visit(child)
		}
	}
	visit(node)
	return result
}

func followIdentityElementPath(
	nodes []*instanceNode,
	steps []string,
	namespaces map[string]string,
) []*instanceNode {
	for _, step := range steps {
		if step == "" {
			return nil
		}
		if step == "." {
			continue
		}
		next := make([]*instanceNode, 0)
		for _, node := range nodes {
			for _, child := range node.Children {
				if identityNameMatches(child.Name, step, namespaces) {
					next = append(next, child)
				}
			}
		}
		nodes = next
	}
	return nodes
}

func (s *validationState) identityTuple(
	node *instanceNode,
	constraint xsd.IdentityConstraint,
) (string, bool, bool) {
	var tuple strings.Builder
	for _, field := range constraint.Fields {
		values := s.identityFieldValues(node, strings.TrimSpace(field), constraint.Namespaces)
		if len(values) == 0 {
			return "", false, false
		}
		if len(values) > 1 {
			return "", false, true
		}
		value := values[0]
		fmt.Fprintf(&tuple, "%d:%s;", len(value), value)
	}
	return tuple.String(), true, false
}

func (s *validationState) identityFieldValues(
	node *instanceNode,
	field string,
	namespaces map[string]string,
) []string {
	field = xsd.NormalizeIdentityXPath(field)
	if strings.Contains(field, "|") {
		values := make([]string, 0)
		seenBranches := make(map[string]struct{})
		for _, branch := range strings.Split(field, "|") {
			branch = strings.TrimSpace(branch)
			if _, duplicate := seenBranches[branch]; duplicate {
				continue
			}
			seenBranches[branch] = struct{}{}
			values = append(values, s.identityFieldValues(node, branch, namespaces)...)
		}
		return values
	}
	if field == "." {
		return []string{s.canonicalIdentityValue(node.Type, node.Text, node.Namespaces)}
	}
	descendant := strings.HasPrefix(field, ".//")
	if descendant {
		field = strings.TrimPrefix(field, ".//")
	} else {
		field = strings.TrimPrefix(field, "./")
	}
	steps := strings.Split(field, "/")
	attribute := ""
	if len(steps) > 0 && (strings.HasPrefix(steps[len(steps)-1], "@") ||
		strings.HasPrefix(steps[len(steps)-1], "attribute::")) {
		attribute = strings.TrimPrefix(steps[len(steps)-1], "@")
		attribute = strings.TrimPrefix(attribute, "attribute::")
		steps = steps[:len(steps)-1]
	}
	var nodes []*instanceNode
	if descendant {
		descendants := identityDescendants(node)
		if len(steps) == 0 {
			nodes = descendants
		} else if steps[0] == "." {
			nodes = followIdentityElementPath(descendants, steps[1:], namespaces)
		} else {
			for _, candidate := range descendants {
				if identityNameMatches(candidate.Name, steps[0], namespaces) {
					nodes = append(nodes, candidate)
				}
			}
			nodes = followIdentityElementPath(nodes, steps[1:], namespaces)
		}
	} else {
		nodes = followIdentityElementPath([]*instanceNode{node}, steps, namespaces)
	}
	if attribute == "" {
		values := make([]string, 0, len(nodes))
		for _, selected := range nodes {
			values = append(values, s.canonicalIdentityValue(
				selected.Type,
				selected.Text,
				selected.Namespaces,
			))
		}
		return values
	}
	if attribute == "*" {
		values := make([]string, 0)
		for _, selected := range nodes {
			for _, name := range sortedAttributeNames(selected.Attributes) {
				values = append(values, s.canonicalIdentityValue(
					selected.AttributeTypes[name],
					selected.Attributes[name],
					selected.Namespaces,
				))
			}
		}
		return values
	}
	name, ok := identityQName(attribute, namespaces)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(nodes))
	for _, selected := range nodes {
		if value, present := selected.Attributes[name]; present {
			values = append(values, s.canonicalIdentityValue(
				selected.AttributeTypes[name],
				value,
				selected.Namespaces,
			))
		}
	}
	return values
}

func (s *validationState) identityKeySelectsNillable(
	node *instanceNode,
	constraint xsd.IdentityConstraint,
) bool {
	for _, field := range constraint.Fields {
		if identityFieldSelectsNillable(
			node,
			strings.TrimSpace(field),
			constraint.Namespaces,
		) {
			return true
		}
	}
	return false
}

func identityFieldSelectsNillable(
	node *instanceNode,
	field string,
	namespaces map[string]string,
) bool {
	field = xsd.NormalizeIdentityXPath(field)
	if strings.Contains(field, "|") {
		for _, branch := range strings.Split(field, "|") {
			if identityFieldSelectsNillable(node, strings.TrimSpace(branch), namespaces) {
				return true
			}
		}
		return false
	}
	if field == "." {
		return node.Nillable
	}
	descendant := strings.HasPrefix(field, ".//")
	if descendant {
		field = strings.TrimPrefix(field, ".//")
	} else {
		field = strings.TrimPrefix(field, "./")
	}
	steps := strings.Split(field, "/")
	if len(steps) > 0 && (strings.HasPrefix(steps[len(steps)-1], "@") ||
		strings.HasPrefix(steps[len(steps)-1], "attribute::")) {
		return false
	}
	var nodes []*instanceNode
	if descendant {
		descendants := identityDescendants(node)
		if steps[0] == "." {
			nodes = followIdentityElementPath(descendants, steps[1:], namespaces)
		} else {
			for _, candidate := range descendants {
				if identityNameMatches(candidate.Name, steps[0], namespaces) {
					nodes = append(nodes, candidate)
				}
			}
			nodes = followIdentityElementPath(nodes, steps[1:], namespaces)
		}
	} else {
		nodes = followIdentityElementPath([]*instanceNode{node}, steps, namespaces)
	}
	return len(nodes) == 1 && nodes[0].Nillable
}

func (s *validationState) canonicalIdentityValue(
	typeName xsd.QName,
	lexical string,
	namespaces map[string]string,
) string {
	if typeName.Namespace != xsd.Namespace {
		if typeDefinition, ok := s.validator.set.SimpleType(typeName); ok {
			return s.canonicalIdentityDefinition(typeDefinition, lexical, namespaces)
		}
		if typeDefinition, ok := s.validator.set.ComplexType(typeName); ok && typeDefinition.SimpleContent {
			if typeDefinition.InlineSimpleType != nil {
				return s.canonicalIdentityDefinition(
					*typeDefinition.InlineSimpleType,
					lexical,
					namespaces,
				)
			}
			return s.canonicalIdentityValue(typeDefinition.SimpleBase, lexical, namespaces)
		}
	}
	primitive := s.primitiveType(typeName)
	normalized := s.normalizeLexical(typeName, lexical)
	switch primitive.Local {
	case "boolean":
		if value, ok := parseSchemaBoolean(normalized); ok {
			return "boolean:" + strconv.FormatBool(value)
		}
	case "decimal":
		if value, err := datatype.ParseDecimal(normalized); err == nil {
			return "decimal:" + value.String()
		}
	case "float", "double":
		bitSize := 64
		if primitive.Local == "float" {
			bitSize = 32
		}
		if value, ok := parseXMLFloat(normalized, bitSize); ok {
			if math.IsNaN(value) {
				return primitive.Local + ":NaN"
			}
			if value == 0 {
				return primitive.Local + ":0"
			}
			return primitive.Local + ":" + strconv.FormatFloat(value, 'g', -1, bitSize)
		}
	case "hexBinary":
		if value, err := hex.DecodeString(normalized); err == nil {
			return "hexBinary:" + hex.EncodeToString(value)
		}
	case "base64Binary":
		compact := strings.Join(strings.Fields(normalized), "")
		if value, err := base64.StdEncoding.DecodeString(compact); err == nil {
			return "base64Binary:" + hex.EncodeToString(value)
		}
	case "duration":
		if value, ok := datatype.CanonicalOrderedValue(primitive.Local, normalized); ok {
			return "duration:" + value
		}
	case "dateTime", "time", "date", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
		if value, ok := datatype.CanonicalOrderedValue(primitive.Local, normalized); ok {
			return primitive.Local + ":" + value
		}
	case "QName", "NOTATION":
		if value, ok := resolveInstanceQName(normalized, namespaces); ok {
			return fmt.Sprintf("%s:{%s}%s", primitive.Local, value.Namespace, value.Local)
		}
	}
	return "lexical:" + normalized
}

func (s *validationState) canonicalIdentityDefinition(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) string {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		normalized := s.normalizeRestrictionLexical(typeDefinition, lexical)
		if typeDefinition.InlineBase != nil {
			return s.canonicalIdentityDefinition(*typeDefinition.InlineBase, normalized, namespaces)
		}
		return s.canonicalIdentityValue(typeDefinition.Base, normalized, namespaces)
	case xsd.SimpleList:
		var canonical strings.Builder
		canonical.WriteString("list:")
		for _, item := range strings.Fields(lexical) {
			value := ""
			if typeDefinition.InlineItem != nil {
				value = s.canonicalIdentityDefinition(*typeDefinition.InlineItem, item, namespaces)
			} else {
				value = s.canonicalIdentityValue(typeDefinition.ItemType, item, namespaces)
			}
			fmt.Fprintf(&canonical, "%d:%s;", len(value), value)
		}
		return canonical.String()
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleLexicalValid(member, lexical) && s.simpleContextValid(member, lexical, namespaces) {
				return s.canonicalIdentityValue(member, lexical, namespaces)
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineSimpleLexicalValid(member, lexical) &&
				s.inlineSimpleContextValid(member, lexical, namespaces) {
				return s.canonicalIdentityDefinition(member, lexical, namespaces)
			}
		}
	}
	return ":lexical:" + lexical
}

func identityNameMatches(
	name xsd.QName,
	expression string,
	namespaces map[string]string,
) bool {
	expression = strings.TrimPrefix(expression, "child::")
	if expression == "*" {
		return true
	}
	if prefix, local, qualified := strings.Cut(expression, ":"); qualified && local == "*" {
		namespace, ok := namespaces[prefix]
		return ok && name.Namespace == namespace
	}
	expected, ok := identityQName(expression, namespaces)
	return ok && name == expected
}

func identityQName(expression string, namespaces map[string]string) (xsd.QName, bool) {
	parts := strings.Split(expression, ":")
	switch len(parts) {
	case 1:
		return xsd.QName{Local: parts[0]}, parts[0] != ""
	case 2:
		namespace, ok := namespaces[parts[0]]
		return xsd.QName{Namespace: namespace, Local: parts[1]}, ok && parts[1] != ""
	default:
		return xsd.QName{}, false
	}
}

func parseSchemaBoolean(value string) (bool, bool) {
	switch value {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}

func (s *validationState) simpleValuesEqual(
	typeName xsd.QName,
	left string,
	right string,
) (bool, error) {
	if typeName.Namespace != xsd.Namespace {
		if simpleType, ok := s.validator.set.SimpleType(typeName); ok {
			switch simpleType.Variety {
			case xsd.SimpleRestriction:
				if simpleType.InlineBase != nil {
					return s.inlineSimpleValuesEqual(*simpleType.InlineBase, left, right)
				}
				return s.simpleValuesEqual(simpleType.Base, left, right)
			case xsd.SimpleList:
				leftItems := strings.Fields(left)
				rightItems := strings.Fields(right)
				if len(leftItems) != len(rightItems) {
					return false, nil
				}
				for index := range leftItems {
					equal, err := s.simpleValuesEqual(
						simpleType.ItemType,
						leftItems[index],
						rightItems[index],
					)
					if err != nil || !equal {
						return false, err
					}
				}
				return true, nil
			case xsd.SimpleUnion:
				for _, member := range simpleType.MemberTypes {
					if !s.simpleLexicalValid(member, left) || !s.simpleLexicalValid(member, right) {
						continue
					}
					if equal, err := s.simpleValuesEqual(member, left, right); err != nil || equal {
						return equal, err
					}
				}
				return false, nil
			}
		}
		return false, nil
	}
	switch typeName.Local {
	case "string", "anySimpleType", "normalizedString", "token":
		return s.normalizeLexical(typeName, left) == s.normalizeLexical(typeName, right), nil
	case "boolean":
		leftValue, leftOK := parseSchemaBoolean(s.normalizeLexical(typeName, left))
		rightValue, rightOK := parseSchemaBoolean(s.normalizeLexical(typeName, right))
		return leftOK && rightOK && leftValue == rightValue, nil
	case "decimal":
		leftValue, err := datatype.ParseDecimal(left)
		if err != nil {
			return false, nil
		}
		rightValue, err := datatype.ParseDecimal(right)
		if err != nil {
			return false, nil
		}
		return leftValue.Compare(rightValue) == 0, nil
	case "float", "double":
		bitSize := 64
		if typeName.Local == "float" {
			bitSize = 32
		}
		leftValue, leftOK := parseXMLFloat(s.normalizeLexical(typeName, left), bitSize)
		rightValue, rightOK := parseXMLFloat(s.normalizeLexical(typeName, right), bitSize)
		if !leftOK || !rightOK {
			return false, nil
		}
		if math.IsNaN(leftValue) && math.IsNaN(rightValue) {
			return true, nil
		}
		return leftValue == rightValue, nil
	case "integer", "nonPositiveInteger", "negativeInteger", "long", "int",
		"short", "byte", "nonNegativeInteger", "unsignedLong", "unsignedInt",
		"unsignedShort", "unsignedByte", "positiveInteger":
		leftValue, err := datatype.ParseInteger(left)
		if err != nil {
			return false, nil
		}
		rightValue, err := datatype.ParseInteger(right)
		if err != nil {
			return false, nil
		}
		return leftValue.Compare(rightValue) == 0, nil
	case "hexBinary":
		leftValue, leftErr := hex.DecodeString(s.normalizeLexical(typeName, left))
		rightValue, rightErr := hex.DecodeString(s.normalizeLexical(typeName, right))
		return leftErr == nil && rightErr == nil && bytes.Equal(leftValue, rightValue), nil
	case "base64Binary":
		leftValue, leftErr := base64.StdEncoding.Strict().DecodeString(
			strings.Join(strings.Fields(left), ""),
		)
		rightValue, rightErr := base64.StdEncoding.Strict().DecodeString(
			strings.Join(strings.Fields(right), ""),
		)
		return leftErr == nil && rightErr == nil && bytes.Equal(leftValue, rightValue), nil
	case "duration":
		return durationValuesEqual(
			s.normalizeLexical(typeName, left),
			s.normalizeLexical(typeName, right),
		), nil
	case "dateTime", "time", "date", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
		comparison, comparable := compareCalendarValues(
			typeName.Local,
			s.normalizeLexical(typeName, left),
			s.normalizeLexical(typeName, right),
		)
		return comparable && comparison == 0, nil
	default:
		return s.normalizeLexical(typeName, left) == s.normalizeLexical(typeName, right), nil
	}
}

func (s *validationState) simpleValuesEqualContext(
	typeName xsd.QName,
	left string,
	right string,
	leftNamespaces map[string]string,
	rightNamespaces map[string]string,
) (bool, error) {
	if typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.validator.set.SimpleType(typeName)
		if ok {
			switch typeDefinition.Variety {
			case xsd.SimpleRestriction:
				if typeDefinition.InlineBase != nil {
					return s.inlineSimpleValuesEqualContext(
						*typeDefinition.InlineBase,
						left,
						right,
						leftNamespaces,
						rightNamespaces,
					)
				}
				return s.simpleValuesEqualContext(
					typeDefinition.Base,
					left,
					right,
					leftNamespaces,
					rightNamespaces,
				)
			case xsd.SimpleList:
				leftItems := strings.Fields(left)
				rightItems := strings.Fields(right)
				if len(leftItems) != len(rightItems) {
					return false, nil
				}
				for index := range leftItems {
					var equal bool
					var err error
					if typeDefinition.InlineItem != nil {
						equal, err = s.inlineSimpleValuesEqualContext(
							*typeDefinition.InlineItem,
							leftItems[index],
							rightItems[index],
							leftNamespaces,
							rightNamespaces,
						)
					} else {
						equal, err = s.simpleValuesEqualContext(
							typeDefinition.ItemType,
							leftItems[index],
							rightItems[index],
							leftNamespaces,
							rightNamespaces,
						)
					}
					if err != nil || !equal {
						return false, err
					}
				}
				return true, nil
			case xsd.SimpleUnion:
				for _, member := range typeDefinition.MemberTypes {
					leftValid := s.simpleLexicalValid(member, left) &&
						s.simpleContextValid(member, left, leftNamespaces)
					rightValid := s.simpleLexicalValid(member, right) &&
						s.simpleContextValid(member, right, rightNamespaces)
					if !leftValid && !rightValid {
						continue
					}
					if !leftValid || !rightValid {
						return false, nil
					}
					return s.simpleValuesEqualContext(
						member,
						left,
						right,
						leftNamespaces,
						rightNamespaces,
					)
				}
				for _, member := range typeDefinition.InlineMembers {
					leftValid := s.inlineSimpleLexicalValid(member, left) &&
						s.inlineSimpleContextValid(member, left, leftNamespaces)
					rightValid := s.inlineSimpleLexicalValid(member, right) &&
						s.inlineSimpleContextValid(member, right, rightNamespaces)
					if !leftValid && !rightValid {
						continue
					}
					if !leftValid || !rightValid {
						return false, nil
					}
					return s.inlineSimpleValuesEqualContext(
						member,
						left,
						right,
						leftNamespaces,
						rightNamespaces,
					)
				}
				return false, nil
			}
		}
	}
	if typeName.Namespace == xsd.Namespace &&
		(typeName.Local == "QName" || typeName.Local == "NOTATION") {
		leftName, leftOK := resolveInstanceQName(strings.TrimSpace(left), leftNamespaces)
		rightName, rightOK := resolveInstanceQName(strings.TrimSpace(right), rightNamespaces)
		return leftOK && rightOK && leftName == rightName, nil
	}
	return s.simpleValuesEqual(typeName, left, right)
}

func (s *validationState) inlineSimpleValuesEqual(
	typeDefinition xsd.SimpleType,
	left string,
	right string,
) (bool, error) {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.inlineSimpleValuesEqual(*typeDefinition.InlineBase, left, right)
		}
		return s.simpleValuesEqual(typeDefinition.Base, left, right)
	case xsd.SimpleList:
		leftItems := strings.Fields(left)
		rightItems := strings.Fields(right)
		if len(leftItems) != len(rightItems) {
			return false, nil
		}
		for index := range leftItems {
			var equal bool
			var err error
			if typeDefinition.InlineItem != nil {
				equal, err = s.inlineSimpleValuesEqual(
					*typeDefinition.InlineItem,
					leftItems[index],
					rightItems[index],
				)
			} else {
				equal, err = s.simpleValuesEqual(
					typeDefinition.ItemType,
					leftItems[index],
					rightItems[index],
				)
			}
			if err != nil || !equal {
				return false, err
			}
		}
		return true, nil
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			leftValid := s.simpleLexicalValid(member, left)
			rightValid := s.simpleLexicalValid(member, right)
			if !leftValid && !rightValid {
				continue
			}
			if !leftValid || !rightValid {
				return false, nil
			}
			return s.simpleValuesEqual(member, left, right)
		}
		for _, member := range typeDefinition.InlineMembers {
			leftValid := s.inlineSimpleLexicalValid(member, left)
			rightValid := s.inlineSimpleLexicalValid(member, right)
			if !leftValid && !rightValid {
				continue
			}
			if !leftValid || !rightValid {
				return false, nil
			}
			return s.inlineSimpleValuesEqual(member, left, right)
		}
		return false, nil
	default:
		return left == right, nil
	}
}

func parseXMLFloat(lexical string, bitSize int) (float64, bool) {
	switch lexical {
	case "INF":
		return math.Inf(1), true
	case "-INF":
		return math.Inf(-1), true
	case "NaN":
		return math.NaN(), true
	default:
		value, err := strconv.ParseFloat(lexical, bitSize)
		return value, err == nil
	}
}

func (s *validationState) validateSimple(
	node *instanceNode,
	typeName xsd.QName,
	path string,
) error {
	for _, name := range sortedAttributeNames(node.Attributes) {
		if permittedSchemaInstanceAttribute(name) {
			continue
		}
		message := fmt.Sprintf(
			"attribute {%s}%s is not allowed on simple content",
			name.Namespace,
			name.Local,
		)
		if err := s.add(node.Location, path, "cvc-type.3.1.1", message); err != nil {
			return err
		}
	}
	if len(node.Children) > 0 {
		return s.add(
			node.Location,
			path,
			"cvc-type.3.1.2",
			"simple content contains a child element",
		)
	}
	if !s.simpleLexicalValid(typeName, node.Text) {
		return s.add(node.Location, path, "cvc-datatype-valid.1.2.1", fmt.Sprintf(
			"value is not valid for %s",
			typeName.Local,
		))
	}
	if !s.simpleContextValid(typeName, node.Text, node.Namespaces) {
		return s.add(node.Location, path, "cvc-datatype-valid.1.2.1", fmt.Sprintf(
			"value is not valid for %s in the instance namespace context",
			typeName.Local,
		))
	}
	return s.recordIDValues(typeName, node.Text, node.Location, path)
}

func qNameLexicalBound(lexical string, namespaces map[string]string) bool {
	_, ok := resolveInstanceQName(strings.TrimSpace(lexical), namespaces)
	return ok
}

func (s *validationState) simpleContextValid(
	typeName xsd.QName,
	lexical string,
	namespaces map[string]string,
) bool {
	if typeName.Namespace == xsd.Namespace {
		if typeName.Local == "QName" || typeName.Local == "NOTATION" {
			return qNameLexicalBound(lexical, namespaces)
		}
		return true
	}
	typeDefinition, ok := s.validator.set.SimpleType(typeName)
	if !ok {
		return false
	}
	return s.inlineSimpleContextValid(typeDefinition, lexical, namespaces)
}

func (s *validationState) inlineSimpleContextValid(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) bool {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		valid := false
		if typeDefinition.InlineBase != nil {
			valid = s.inlineSimpleContextValid(*typeDefinition.InlineBase, lexical, namespaces)
		} else {
			valid = s.simpleContextValid(typeDefinition.Base, lexical, namespaces)
		}
		return valid && s.contextualEnumerationValid(
			typeDefinition,
			s.normalizeRestrictionLexical(typeDefinition, lexical),
			namespaces,
		)
	case xsd.SimpleList:
		for _, item := range strings.Fields(lexical) {
			if typeDefinition.InlineItem != nil {
				if !s.inlineSimpleContextValid(*typeDefinition.InlineItem, item, namespaces) {
					return false
				}
			} else if !s.simpleContextValid(typeDefinition.ItemType, item, namespaces) {
				return false
			}
		}
		return true
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleLexicalValid(member, lexical) && s.simpleContextValid(member, lexical, namespaces) {
				return true
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineSimpleLexicalValid(member, lexical) &&
				s.inlineSimpleContextValid(member, lexical, namespaces) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func (s *validationState) recordIDValues(
	typeName xsd.QName,
	lexical string,
	location xsd.Location,
	path string,
) error {
	switch s.idTypeKind(typeName) {
	case "ID":
		value := strings.TrimSpace(lexical)
		if s.ids == nil {
			s.ids = make(map[string]struct{})
		}
		if _, duplicate := s.ids[value]; duplicate {
			return s.add(location, path, "cvc-id.2", "ID value is not unique")
		}
		s.ids[value] = struct{}{}
	case "IDREF", "IDREFS":
		for _, value := range strings.Fields(lexical) {
			s.idReferences = append(s.idReferences, idReference{
				value: value, location: location, path: path,
			})
		}
	}
	return nil
}

func (s *validationState) idTypeKind(typeName xsd.QName) string {
	for typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.validator.set.SimpleType(typeName)
		if !ok || typeDefinition.Variety != xsd.SimpleRestriction {
			return ""
		}
		typeName = typeDefinition.Base
	}
	switch typeName.Local {
	case "ID", "IDREF", "IDREFS":
		return typeName.Local
	default:
		return ""
	}
}

func (s *validationState) validateIDReferences() error {
	for _, reference := range s.idReferences {
		if _, ok := s.ids[reference.value]; ok {
			continue
		}
		if err := s.add(
			reference.location,
			reference.path,
			"cvc-idref",
			"IDREF value does not match an ID",
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *validationState) simpleLexicalValid(typeName xsd.QName, lexical string) bool {
	if typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.validator.set.SimpleType(typeName)
		if !ok {
			return false
		}
		switch typeDefinition.Variety {
		case xsd.SimpleRestriction:
			baseValid := s.simpleLexicalValid(typeDefinition.Base, lexical)
			if typeDefinition.InlineBase != nil {
				baseValid = s.inlineSimpleLexicalValid(*typeDefinition.InlineBase, lexical)
			}
			if !baseValid {
				return false
			}
			return s.facetsValid(typeDefinition, lexical)
		case xsd.SimpleList:
			items := strings.Fields(lexical)
			if len(items) == 0 {
				return false
			}
			for _, item := range items {
				valid := s.simpleLexicalValid(typeDefinition.ItemType, item)
				if typeDefinition.InlineItem != nil {
					valid = s.inlineSimpleLexicalValid(*typeDefinition.InlineItem, item)
				}
				if !valid {
					return false
				}
			}
			return true
		default: // Compiled sets permit only restriction, list, or union.
			for _, memberType := range typeDefinition.MemberTypes {
				if s.simpleLexicalValid(memberType, lexical) {
					return true
				}
			}
			for _, member := range typeDefinition.InlineMembers {
				if s.inlineSimpleLexicalValid(member, lexical) {
					return true
				}
			}
			return false
		}
	}
	normalized := s.normalizeLexical(typeName, lexical)
	switch typeName.Local {
	case "anySimpleType":
		return true
	case "boolean":
		_, ok := parseSchemaBoolean(normalized)
		return ok
	case "decimal":
		_, err := datatype.ParseDecimal(normalized)
		return err == nil
	case "integer", "nonPositiveInteger", "negativeInteger", "long", "int",
		"short", "byte", "nonNegativeInteger", "unsignedLong", "unsignedInt",
		"unsignedShort", "unsignedByte", "positiveInteger":
		value, err := datatype.ParseInteger(normalized)
		return err == nil && datatype.ValidateBuiltInInteger(typeName.Local, value) == nil
	default:
		return datatype.ValidateBuiltInLexical(typeName.Local, normalized) == nil
	}
}

func (s *validationState) facetsValid(typeDefinition xsd.SimpleType, lexical string) bool {
	normalized := s.normalizeRestrictionLexical(typeDefinition, lexical)
	length := uint64(utf8.RuneCountInString(normalized))
	if typeDefinition.InlineBase != nil && typeDefinition.InlineBase.Variety == xsd.SimpleList {
		normalized = strings.Join(strings.Fields(lexical), " ")
		length = uint64(len(strings.Fields(normalized)))
	}
	if base, ok := s.validator.set.SimpleType(typeDefinition.Base); ok &&
		base.Variety == xsd.SimpleList {
		length = uint64(len(strings.Fields(normalized)))
	}
	switch s.primitiveType(typeDefinition.Base).Local {
	case "NMTOKENS", "IDREFS", "ENTITIES":
		length = uint64(len(strings.Fields(normalized)))
	case "hexBinary":
		if decoded, err := hex.DecodeString(normalized); err == nil {
			length = uint64(len(decoded))
		}
	case "base64Binary":
		compact := strings.Join(strings.Fields(normalized), "")
		if decoded, err := base64.StdEncoding.DecodeString(compact); err == nil {
			length = uint64(len(decoded))
		}
	}
	enumerations := make([]xsd.Facet, 0)
	hasPattern := false
	patternMatched := false
	for _, facet := range typeDefinition.Facets {
		switch facet.Kind {
		case xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength:
			bound, err := strconv.ParseUint(facet.Value, 10, 64)
			if err != nil {
				return false
			}
			switch facet.Kind {
			case xsd.FacetLength:
				if length != bound {
					return false
				}
			case xsd.FacetMinLength:
				if length < bound {
					return false
				}
			case xsd.FacetMaxLength:
				if length > bound {
					return false
				}
			}
		case xsd.FacetEnumeration:
			enumerations = append(enumerations, facet)
		case xsd.FacetMinInclusive, xsd.FacetMinExclusive,
			xsd.FacetMaxInclusive, xsd.FacetMaxExclusive,
			xsd.FacetTotalDigits, xsd.FacetFractionDigits:
			if !s.numericFacetValid(typeDefinition.Base, normalized, facet) {
				return false
			}
		case xsd.FacetWhiteSpace:
			if facet.Value != "preserve" && facet.Value != "replace" &&
				facet.Value != "collapse" {
				return false
			}
		case xsd.FacetPattern:
			hasPattern = true
			pattern, err := datatype.CompilePattern(facet.Value)
			if err != nil {
				return false
			}
			patternMatched = patternMatched || pattern.MatchString(normalized)
		}
	}
	if hasPattern && !patternMatched {
		return false
	}
	if len(enumerations) > 0 {
		if s.simpleTypeUsesNamespaceContext(typeDefinition.Base) {
			return true
		}
		for _, enumeration := range enumerations {
			equal, err := s.simpleValuesEqual(typeDefinition.Base, normalized, enumeration.Value)
			if err == nil && equal {
				return true
			}
		}
		return false
	}
	return true
}

func (s *validationState) contextualEnumerationValid(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) bool {
	hasEnumeration := false
	for _, facet := range typeDefinition.Facets {
		if facet.Kind != xsd.FacetEnumeration {
			continue
		}
		hasEnumeration = true
		equal, err := s.simpleValuesEqualContext(
			typeDefinition.Base,
			lexical,
			facet.Value,
			namespaces,
			facet.Namespaces,
		)
		if err == nil && equal {
			return true
		}
	}
	return !hasEnumeration
}

func (s *validationState) simpleTypeUsesNamespaceContext(typeName xsd.QName) bool {
	if typeName.Namespace == xsd.Namespace {
		return typeName.Local == "QName" || typeName.Local == "NOTATION"
	}
	typeDefinition, ok := s.validator.set.SimpleType(typeName)
	if !ok {
		return false
	}
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.inlineTypeUsesNamespaceContext(*typeDefinition.InlineBase)
		}
		return s.simpleTypeUsesNamespaceContext(typeDefinition.Base)
	case xsd.SimpleList:
		if typeDefinition.InlineItem != nil {
			return s.inlineTypeUsesNamespaceContext(*typeDefinition.InlineItem)
		}
		return s.simpleTypeUsesNamespaceContext(typeDefinition.ItemType)
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleTypeUsesNamespaceContext(member) {
				return true
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineTypeUsesNamespaceContext(member) {
				return true
			}
		}
	}
	return false
}

func (s *validationState) inlineTypeUsesNamespaceContext(typeDefinition xsd.SimpleType) bool {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.inlineTypeUsesNamespaceContext(*typeDefinition.InlineBase)
		}
		return s.simpleTypeUsesNamespaceContext(typeDefinition.Base)
	case xsd.SimpleList:
		if typeDefinition.InlineItem != nil {
			return s.inlineTypeUsesNamespaceContext(*typeDefinition.InlineItem)
		}
		return s.simpleTypeUsesNamespaceContext(typeDefinition.ItemType)
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleTypeUsesNamespaceContext(member) {
				return true
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineTypeUsesNamespaceContext(member) {
				return true
			}
		}
	}
	return false
}

func (s *validationState) numericFacetValid(
	base xsd.QName,
	lexical string,
	facet xsd.Facet,
) bool {
	primitive := s.primitiveType(base)
	if primitive.Local == "decimal" {
		value, err := datatype.ParseDecimal(lexical)
		if err != nil {
			return false
		}
		switch facet.Kind {
		case xsd.FacetTotalDigits:
			bound, parseErr := strconv.Atoi(facet.Value)
			return parseErr == nil && bound > 0 && value.TotalDigits() <= bound
		case xsd.FacetFractionDigits:
			bound, parseErr := strconv.Atoi(facet.Value)
			return parseErr == nil && bound >= 0 && value.FractionDigits() <= bound
		}
		boundary, parseErr := datatype.ParseDecimal(facet.Value)
		if parseErr != nil {
			return false
		}
		comparison := value.Compare(boundary)
		switch facet.Kind {
		case xsd.FacetMinInclusive:
			return comparison >= 0
		case xsd.FacetMinExclusive:
			return comparison > 0
		case xsd.FacetMaxInclusive:
			return comparison <= 0
		case xsd.FacetMaxExclusive:
			return comparison < 0
		}
	}
	if primitive.Local == "float" || primitive.Local == "double" {
		bitSize := 64
		if primitive.Local == "float" {
			bitSize = 32
		}
		value, valueOK := parseXMLFloat(lexical, bitSize)
		boundary, boundaryOK := parseXMLFloat(facet.Value, bitSize)
		if !valueOK || !boundaryOK || math.IsNaN(value) || math.IsNaN(boundary) {
			return false
		}
		comparison := 0
		if value < boundary {
			comparison = -1
		} else if value > boundary {
			comparison = 1
		}
		return comparisonSatisfiesFacet(comparison, facet.Kind)
	}
	if primitive.Local == "duration" {
		comparison, comparable := compareDurations(lexical, facet.Value)
		return comparable && comparisonSatisfiesFacet(comparison, facet.Kind)
	}
	switch primitive.Local {
	case "dateTime", "time", "date", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
		comparison, comparable := compareCalendarValues(primitive.Local, lexical, facet.Value)
		return comparable && comparisonSatisfiesFacet(comparison, facet.Kind)
	}
	return false
}

func comparisonSatisfiesFacet(comparison int, kind xsd.FacetKind) bool {
	switch kind {
	case xsd.FacetMinInclusive:
		return comparison >= 0
	case xsd.FacetMinExclusive:
		return comparison > 0
	case xsd.FacetMaxInclusive:
		return comparison <= 0
	case xsd.FacetMaxExclusive:
		return comparison < 0
	default:
		return false
	}
}

var durationValuePattern = regexp.MustCompile(
	`^(-)?P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`,
)

type durationValue struct {
	sign          int
	years, months big.Int
	days, hours   big.Int
	minutes       big.Int
	seconds       big.Rat
}

func parseDurationValue(lexical string) (durationValue, bool) {
	match := durationValuePattern.FindStringSubmatch(lexical)
	if match == nil {
		return durationValue{}, false
	}
	value := durationValue{sign: 1}
	if match[1] != "" {
		value.sign = -1
	}
	values := []*big.Int{&value.years, &value.months, &value.days, &value.hours, &value.minutes}
	for index, target := range values {
		if match[index+2] == "" {
			continue
		}
		target.SetString(match[index+2], 10)
	}
	if match[7] != "" {
		value.seconds.SetString(match[7])
	}
	return value, true
}

func compareDurations(left, right string) (int, bool) {
	return datatype.CompareOrdered("duration", left, right)
}

func durationValuesEqual(left, right string) bool {
	leftValue, leftOK := parseDurationValue(left)
	rightValue, rightOK := parseDurationValue(right)
	if !leftOK || !rightOK {
		return false
	}
	leftMonths, leftSeconds := durationComponents(leftValue)
	rightMonths, rightSeconds := durationComponents(rightValue)
	return leftMonths.Cmp(rightMonths) == 0 && leftSeconds.Cmp(rightSeconds) == 0
}

func durationComponents(value durationValue) (*big.Int, *big.Rat) {
	months := new(big.Int).Mul(&value.years, big.NewInt(12))
	months.Add(months, &value.months)
	days := new(big.Int).Mul(&value.days, big.NewInt(86400))
	days.Add(days, new(big.Int).Mul(&value.hours, big.NewInt(3600)))
	days.Add(days, new(big.Int).Mul(&value.minutes, big.NewInt(60)))
	seconds := new(big.Rat).SetInt(days)
	seconds.Add(seconds, &value.seconds)
	if value.sign < 0 {
		months.Neg(months)
		seconds.Neg(seconds)
	}
	return months, seconds
}

func compareCalendarValues(kind, left, right string) (int, bool) {
	return datatype.CompareOrdered(kind, left, right)
}

func (s *validationState) primitiveType(typeName xsd.QName) xsd.QName {
	for typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.validator.set.SimpleType(typeName)
		if !ok || typeDefinition.Variety != xsd.SimpleRestriction {
			return xsd.QName{}
		}
		typeName = typeDefinition.Base
	}
	if typeName.Local == "integer" || isIntegerDerived(typeName.Local) {
		return xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}
	}
	return typeName
}

func isIntegerDerived(local string) bool {
	switch local {
	case "nonPositiveInteger", "negativeInteger", "long", "int", "short", "byte",
		"nonNegativeInteger", "unsignedLong", "unsignedInt", "unsignedShort",
		"unsignedByte", "positiveInteger":
		return true
	default:
		return false
	}
}

func (s *validationState) normalizeLexical(typeName xsd.QName, lexical string) string {
	if typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.validator.set.SimpleType(typeName)
		if !ok {
			return lexical
		}
		switch typeDefinition.Variety {
		case xsd.SimpleRestriction:
			return s.normalizeRestrictionLexical(typeDefinition, lexical)
		case xsd.SimpleList:
			return strings.Join(strings.Fields(lexical), " ")
		default:
			return lexical
		}
	}
	switch typeName.Local {
	case "string":
		return lexical
	case "normalizedString":
		return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(lexical)
	default:
		return strings.Join(strings.Fields(lexical), " ")
	}
}

func (s *validationState) normalizeRestrictionLexical(
	typeDefinition xsd.SimpleType,
	lexical string,
) string {
	normalized := s.normalizeLexical(typeDefinition.Base, lexical)
	if typeDefinition.InlineBase != nil {
		switch typeDefinition.InlineBase.Variety {
		case xsd.SimpleRestriction:
			normalized = s.normalizeRestrictionLexical(*typeDefinition.InlineBase, lexical)
		case xsd.SimpleList:
			normalized = strings.Join(strings.Fields(lexical), " ")
		default:
			normalized = lexical
		}
	}
	for _, facet := range typeDefinition.Facets {
		if facet.Kind != xsd.FacetWhiteSpace {
			continue
		}
		switch facet.Value {
		case "replace":
			normalized = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(normalized)
		case "collapse":
			normalized = strings.Join(strings.Fields(normalized), " ")
		}
	}
	return normalized
}

func (s *validationState) validateComplex(
	node *instanceNode,
	typeNamespace string,
	typeDefinition xsd.ComplexType,
	path string,
) error {
	if !typeDefinition.SimpleContent && !typeDefinition.Mixed && strings.TrimSpace(node.Text) != "" {
		if err := s.add(
			node.Location,
			path,
			"cvc-complex-type.2.3",
			"element-only content contains character data",
		); err != nil {
			return err
		}
	}
	if err := s.validateAttributes(
		node,
		typeNamespace,
		typeDefinition.Attributes,
		typeDefinition.AttributeWildcard,
		path,
	); err != nil {
		return err
	}
	if typeDefinition.SimpleContent {
		if len(node.Children) > 0 {
			return s.add(
				node.Children[0].Location,
				path,
				"cvc-complex-type.2.2",
				"simple content contains a child element",
			)
		}
		contentNode := *node
		contentNode.Attributes = nil
		if typeDefinition.InlineSimpleType != nil {
			if !s.inlineSimpleLexicalValid(*typeDefinition.InlineSimpleType, contentNode.Text) ||
				!s.inlineSimpleContextValid(
					*typeDefinition.InlineSimpleType,
					contentNode.Text,
					contentNode.Namespaces,
				) {
				return s.add(
					contentNode.Location,
					path,
					"cvc-datatype-valid.1.2.1",
					"value is not valid for the simple content type",
				)
			}
			return nil
		}
		base := typeDefinition.SimpleBase
		if base.Local == "" {
			base = typeDefinition.Base
		}
		return s.validateSimple(&contentNode, base, path)
	}
	if typeDefinition.Content == nil {
		if len(node.Children) > 0 {
			return s.add(
				node.Children[0].Location,
				path,
				"cvc-complex-type.2.1",
				"empty content type contains a child element",
			)
		}
		return nil
	}
	next, matched, err := s.matchGroup(
		typeDefinition.Content,
		node.Children,
		0,
		typeNamespace,
		path,
	)
	if err != nil {
		return err
	}
	if !matched || next != len(node.Children) {
		location := node.Location
		if next < len(node.Children) {
			location = node.Children[next].Location
		}
		return s.add(
			location,
			path,
			"cvc-complex-type.2.4.a",
			"child element sequence does not match the content model",
		)
	}
	return nil
}

func (s *validationState) validateAttributes(
	node *instanceNode,
	typeNamespace string,
	uses []xsd.AttributeUse,
	wildcard *xsd.Wildcard,
	path string,
) error {
	known := make(map[xsd.QName]struct{}, len(uses))
	for _, use := range uses {
		effective := use
		name := use.Ref
		if name.Local == "" {
			name = xsd.QName{Namespace: use.Namespace, Local: use.Name}
		} else {
			declaration, ok := s.validator.set.Attribute(name)
			if !ok {
				if err := s.add(node.Location, path, "src-resolve", fmt.Sprintf(
					"attribute {%s}%s is not defined",
					name.Namespace,
					name.Local,
				)); err != nil {
					return err
				}
				continue
			}
			effective.Name = declaration.Name
			effective.Type = declaration.Type
			effective.InlineSimpleType = declaration.InlineSimpleType
			if !validatorAttributeDefaultSet(effective) && !validatorAttributeFixedSet(effective) {
				effective.Default = declaration.Default
				effective.Fixed = declaration.Fixed
				effective.DefaultSet = declaration.DefaultSet
				effective.FixedSet = declaration.FixedSet
			}
		}
		known[name] = struct{}{}
		value, present := node.Attributes[name]
		if effective.Use == xsd.AttributeRequired && !present {
			if err := s.add(node.Location, path, "cvc-complex-type.4", fmt.Sprintf(
				"required attribute {%s}%s is missing",
				name.Namespace,
				name.Local,
			)); err != nil {
				return err
			}
		}
		if effective.Use == xsd.AttributeProhibited && present {
			if err := s.add(node.Location, path, "cvc-complex-type.3.2.2", fmt.Sprintf(
				"attribute {%s}%s is prohibited",
				name.Namespace,
				name.Local,
			)); err != nil {
				return err
			}
		}
		if present && effective.InlineSimpleType != nil {
			node.AttributeTypes[name] = effective.InlineSimpleType.Base
			if !s.inlineSimpleLexicalValid(*effective.InlineSimpleType, value) {
				if err := s.add(
					node.Location,
					path+"/@"+name.Local,
					"cvc-datatype-valid.1.2.1",
					"attribute is not valid for its anonymous simple type",
				); err != nil {
					return err
				}
			}
			if !s.inlineSimpleContextValid(*effective.InlineSimpleType, value, node.Namespaces) {
				if err := s.add(
					node.Location,
					path+"/@"+name.Local,
					"cvc-datatype-valid.1.2.1",
					"attribute is not valid in the instance namespace context",
				); err != nil {
					return err
				}
			}
		} else if present && effective.Type.Local != "" {
			node.AttributeTypes[name] = effective.Type
			attributeNode := &instanceNode{
				Name: name, Text: value, Location: node.Location, Namespaces: node.Namespaces,
			}
			if err := s.validateSimple(attributeNode, effective.Type, path+"/@"+name.Local); err != nil {
				return err
			}
		}
		if present && validatorAttributeFixedSet(effective) {
			equal, _ := s.attributeValuesEqual(
				effective,
				value,
				effective.Fixed,
				node.Namespaces,
			)
			if !equal {
				if err := s.add(
					node.Location,
					path+"/@"+name.Local,
					"cvc-complex-type.3.1",
					"attribute value does not match its fixed constraint",
				); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range sortedAttributeNames(node.Attributes) {
		if permittedSchemaInstanceAttribute(name) {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		if wildcardMatches(wildcard, name.Namespace, typeNamespace) {
			if wildcard.ProcessContents == xsd.ProcessSkip {
				continue
			}
			declaration, declared := s.validator.set.Attribute(name)
			if !declared && wildcard.ProcessContents == xsd.ProcessLax {
				continue
			}
			if !declared {
				if err := s.add(node.Location, path+"/@"+name.Local, "cvc-wildcard", fmt.Sprintf(
					"attribute {%s}%s has no declaration",
					name.Namespace,
					name.Local,
				)); err != nil {
					return err
				}
				continue
			}
			if declaration.InlineSimpleType != nil {
				if !s.inlineSimpleLexicalValid(
					*declaration.InlineSimpleType,
					node.Attributes[name],
				) {
					if err := s.add(
						node.Location,
						path+"/@"+name.Local,
						"cvc-datatype-valid.1.2.1",
						"wildcard attribute is not valid for its anonymous type",
					); err != nil {
						return err
					}
				}
			} else {
				attributeNode := &instanceNode{
					Name:       name,
					Text:       node.Attributes[name],
					Location:   node.Location,
					Namespaces: node.Namespaces,
				}
				if err := s.validateSimple(
					attributeNode,
					declaration.Type,
					path+"/@"+name.Local,
				); err != nil {
					return err
				}
			}
			continue
		}
		if err := s.add(node.Location, path, "cvc-complex-type.3.2.2", fmt.Sprintf(
			"attribute {%s}%s is not allowed",
			name.Namespace,
			name.Local,
		)); err != nil {
			return err
		}
	}
	return nil
}

func (s *validationState) attributeValuesEqual(
	attribute xsd.AttributeUse,
	left string,
	right string,
	leftNamespaces map[string]string,
) (bool, error) {
	if attribute.InlineSimpleType != nil {
		return s.inlineSimpleValuesEqualContext(
			*attribute.InlineSimpleType,
			left,
			right,
			leftNamespaces,
			attribute.ValueNamespaces,
		)
	}
	if attribute.Type.Local == "" {
		return left == right, nil
	}
	return s.simpleValuesEqualContext(
		attribute.Type,
		left,
		right,
		leftNamespaces,
		attribute.ValueNamespaces,
	)
}

func (s *validationState) inlineSimpleValuesEqualContext(
	typeDefinition xsd.SimpleType,
	left string,
	right string,
	leftNamespaces map[string]string,
	rightNamespaces map[string]string,
) (bool, error) {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.inlineSimpleValuesEqualContext(
				*typeDefinition.InlineBase,
				left,
				right,
				leftNamespaces,
				rightNamespaces,
			)
		}
		return s.simpleValuesEqualContext(
			typeDefinition.Base,
			left,
			right,
			leftNamespaces,
			rightNamespaces,
		)
	case xsd.SimpleList:
		leftItems := strings.Fields(left)
		rightItems := strings.Fields(right)
		if len(leftItems) != len(rightItems) {
			return false, nil
		}
		for index := range leftItems {
			var equal bool
			var err error
			if typeDefinition.InlineItem != nil {
				equal, err = s.inlineSimpleValuesEqualContext(
					*typeDefinition.InlineItem,
					leftItems[index],
					rightItems[index],
					leftNamespaces,
					rightNamespaces,
				)
			} else {
				equal, err = s.simpleValuesEqualContext(
					typeDefinition.ItemType,
					leftItems[index],
					rightItems[index],
					leftNamespaces,
					rightNamespaces,
				)
			}
			if err != nil || !equal {
				return false, err
			}
		}
		return true, nil
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			leftValid := s.simpleLexicalValid(member, left) &&
				s.simpleContextValid(member, left, leftNamespaces)
			rightValid := s.simpleLexicalValid(member, right) &&
				s.simpleContextValid(member, right, rightNamespaces)
			if !leftValid && !rightValid {
				continue
			}
			if !leftValid || !rightValid {
				return false, nil
			}
			return s.simpleValuesEqualContext(
				member,
				left,
				right,
				leftNamespaces,
				rightNamespaces,
			)
		}
		for _, member := range typeDefinition.InlineMembers {
			leftValid := s.inlineSimpleLexicalValid(member, left) &&
				s.inlineSimpleContextValid(member, left, leftNamespaces)
			rightValid := s.inlineSimpleLexicalValid(member, right) &&
				s.inlineSimpleContextValid(member, right, rightNamespaces)
			if !leftValid && !rightValid {
				continue
			}
			if !leftValid || !rightValid {
				return false, nil
			}
			return s.inlineSimpleValuesEqualContext(
				member,
				left,
				right,
				leftNamespaces,
				rightNamespaces,
			)
		}
		return false, nil
	}
	return s.inlineSimpleValuesEqual(typeDefinition, left, right)
}

func permittedSchemaInstanceAttribute(name xsd.QName) bool {
	if name.Namespace != schemaInstanceNamespace {
		return false
	}
	switch name.Local {
	case "nil", "type", "schemaLocation", "noNamespaceSchemaLocation":
		return true
	default:
		return false
	}
}

func sortedAttributeNames(attributes map[xsd.QName]string) []xsd.QName {
	names := make([]xsd.QName, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool {
		if names[left].Namespace == names[right].Namespace {
			return names[left].Local < names[right].Local
		}
		return names[left].Namespace < names[right].Namespace
	})
	return names
}

func (s *validationState) matchGroup(
	group *xsd.ModelGroup,
	children []*instanceNode,
	index int,
	typeNamespace string,
	path string,
) (int, bool, error) {
	if !group.OccursSet {
		return s.matchGroupOnce(group, children, index, typeNamespace, path)
	}
	current := index
	count := uint64(0)
	for group.Unbounded || count < group.MaxOccurs {
		next, matched, err := s.matchGroupOnce(
			group,
			children,
			current,
			typeNamespace,
			path,
		)
		if err != nil {
			return index, false, err
		}
		if !matched {
			break
		}
		count++
		if next == current {
			break
		}
		current = next
	}
	return current, count >= group.MinOccurs, nil
}

func (s *validationState) matchGroupOnce(
	group *xsd.ModelGroup,
	children []*instanceNode,
	index int,
	typeNamespace string,
	path string,
) (int, bool, error) {
	switch group.Compositor {
	case xsd.Sequence:
		current := index
		for _, particle := range group.Particles {
			next, matched, err := s.matchParticle(
				particle,
				children,
				current,
				typeNamespace,
				path,
			)
			if err != nil || !matched {
				return index, false, err
			}
			current = next
		}
		return current, true, nil
	case xsd.Choice:
		nullable := false
		for _, particle := range group.Particles {
			next, matched, err := s.matchParticle(
				particle,
				children,
				index,
				typeNamespace,
				path,
			)
			if err != nil {
				return index, false, err
			}
			if matched && next != index {
				return next, true, nil
			}
			nullable = nullable || matched
		}
		return index, nullable, nil
	case xsd.All:
		current := index
		counts := make([]uint64, len(group.Particles))
		for current < len(children) {
			consumed := false
			for particleIndex, particle := range group.Particles {
				if !particle.Unbounded && counts[particleIndex] >= particle.MaxOccurs {
					continue
				}
				next, matched, err := s.matchParticleOnce(
					particle,
					children,
					current,
					typeNamespace,
					path,
				)
				if err != nil {
					return index, false, err
				}
				if !matched {
					continue
				}
				counts[particleIndex]++
				current = next
				consumed = true
				break
			}
			if !consumed {
				break
			}
		}
		for particleIndex, particle := range group.Particles {
			if counts[particleIndex] < particle.MinOccurs {
				return index, false, nil
			}
		}
		return current, true, nil
	default:
		return index, false, nil
	}
}

func (s *validationState) matchParticle(
	particle xsd.Particle,
	children []*instanceNode,
	index int,
	typeNamespace string,
	path string,
) (int, bool, error) {
	current := index
	count := uint64(0)
	for particle.Unbounded || count < particle.MaxOccurs {
		next, matched, err := s.matchParticleOnce(
			particle,
			children,
			current,
			typeNamespace,
			path,
		)
		if err != nil {
			return index, false, err
		}
		if !matched {
			break
		}
		count++
		if next == current {
			break
		}
		current = next
	}
	if count < particle.MinOccurs {
		return index, false, nil
	}
	return current, true, nil
}

func (s *validationState) matchParticleOnce(
	particle xsd.Particle,
	children []*instanceNode,
	index int,
	typeNamespace string,
	path string,
) (int, bool, error) {
	if particle.Element != nil {
		if index >= len(children) {
			return index, false, nil
		}
		declaration := *particle.Element
		expected := declaration.Ref
		if expected.Local == "" {
			expected = xsd.QName{
				Namespace: declaration.Namespace,
				Local:     declaration.Name,
			}
		}
		if children[index].Name != expected {
			if declaration.Ref.Local == "" {
				return index, false, nil
			}
			member, ok := s.validator.set.SubstitutionMember(
				declaration.Ref,
				children[index].Name,
			)
			if !ok {
				return index, false, nil
			}
			declaration = member
			expected = children[index].Name
		} else if declaration.Ref.Local != "" {
			global, ok := s.validator.set.Element(declaration.Ref)
			if !ok {
				return index, true, s.add(
					children[index].Location,
					path+"/"+expected.Local,
					"src-resolve",
					fmt.Sprintf(
						"element {%s}%s is not defined",
						declaration.Ref.Namespace,
						declaration.Ref.Local,
					),
				)
			}
			declaration = global
		}
		childPath := path + "/" + expected.Local
		if err := s.validateElement(children[index], declaration, childPath); err != nil {
			return index, true, err
		}
		return index + 1, true, nil
	}
	if particle.Group != nil {
		return s.matchGroup(particle.Group, children, index, typeNamespace, path)
	}
	if particle.Wildcard != nil {
		if index >= len(children) ||
			!wildcardMatches(particle.Wildcard, children[index].Name.Namespace, typeNamespace) {
			return index, false, nil
		}
		child := children[index]
		if particle.Wildcard.ProcessContents == xsd.ProcessSkip {
			return index + 1, true, nil
		}
		declaration, declared := s.validator.set.Element(child.Name)
		if !declared && particle.Wildcard.ProcessContents == xsd.ProcessLax {
			return index + 1, true, nil
		}
		childPath := path + "/" + child.Name.Local
		if !declared {
			return index + 1, true, s.add(
				child.Location,
				childPath,
				"cvc-wildcard",
				fmt.Sprintf(
					"element {%s}%s has no declaration",
					child.Name.Namespace,
					child.Name.Local,
				),
			)
		}
		if err := s.validateElement(child, declaration, childPath); err != nil {
			return index + 1, true, err
		}
		return index + 1, true, nil
	}
	return index, false, nil
}

func wildcardMatches(wildcard *xsd.Wildcard, namespace string, targetNamespace string) bool {
	if wildcard == nil {
		return false
	}
	for _, constraint := range wildcard.Namespaces {
		switch constraint {
		case "##any":
			return true
		case "##other":
			return namespace != "" && namespace != targetNamespace
		case "##local":
			if namespace == "" {
				return true
			}
		case "##targetNamespace":
			if namespace == targetNamespace {
				return true
			}
		default:
			if namespace == constraint {
				return true
			}
		}
	}
	return false
}

func parseError(decoder *xml.Decoder, systemID string, err error) error {
	line, column := decoder.InputPos()
	return &xsd.ParseError{
		Location: xsd.Location{
			SystemID: systemID,
			Line:     line,
			Column:   column,
			Offset:   decoder.InputOffset(),
		},
		Err: err,
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
