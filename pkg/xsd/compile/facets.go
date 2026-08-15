package compile

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func (s *compileState) normalizeConstraintLexical(
	typeDefinition xsd.SimpleType,
	lexical string,
) string {
	whitespace, _ := s.definitionWhitespace(typeDefinition, 0)
	return normalizeConstraintWhitespace(lexical, whitespace)
}

func normalizeConstraintWhitespace(lexical string, whitespace string) string {
	switch whitespace {
	case "replace":
		return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(lexical)
	case "collapse":
		return strings.Join(strings.Fields(lexical), " ")
	default:
		return lexical
	}
}

func (s *compileState) restrictionConstraintFacetsValid(
	typeDefinition xsd.SimpleType,
	lexical string,
) bool {
	return s.restrictionConstraintFacetsValidContext(typeDefinition, lexical, nil, 0)
}

func (s *compileState) restrictionConstraintFacetsValidContext(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
	depth int,
) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	shape := s.restrictionBaseShape(typeDefinition)
	length := uint64(utf8.RuneCountInString(lexical))
	switch shape.variety {
	case listShape:
		length = uint64(len(strings.Fields(lexical)))
	case atomicShape:
		switch shape.primitive {
		case "hexBinary":
			if decoded, err := hex.DecodeString(lexical); err == nil {
				length = uint64(len(decoded))
			}
		case "base64Binary":
			compact := strings.Join(strings.Fields(lexical), "")
			if decoded, err := base64.StdEncoding.DecodeString(compact); err == nil {
				length = uint64(len(decoded))
			}
		}
	}
	enumerations := make([]xsd.Facet, 0)
	for _, facet := range typeDefinition.Facets {
		switch facet.Kind {
		case xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength:
			bound, err := strconv.ParseUint(facet.Value, 10, 64)
			if err != nil || facet.Kind == xsd.FacetLength && length != bound ||
				facet.Kind == xsd.FacetMinLength && length < bound ||
				facet.Kind == xsd.FacetMaxLength && length > bound {
				return false
			}
		case xsd.FacetEnumeration:
			enumerations = append(enumerations, facet)
		case xsd.FacetMinInclusive, xsd.FacetMinExclusive,
			xsd.FacetMaxInclusive, xsd.FacetMaxExclusive,
			xsd.FacetTotalDigits, xsd.FacetFractionDigits:
			if !constraintNumericFacetValid(shape.primitive, lexical, facet) {
				return false
			}
		}
	}
	if len(enumerations) == 0 {
		return true
	}
	for _, enumeration := range enumerations {
		childDepth := compileChildDepth(depth)
		enumerationLexical := s.normalizeConstraintLexical(
			typeDefinition,
			enumeration.Value,
		)
		equal := false
		if typeDefinition.InlineBase != nil {
			equal = s.inlineConstraintValuesEqualContext(
				*typeDefinition.InlineBase,
				lexical,
				enumerationLexical,
				namespaces,
				enumeration.Namespaces,
				childDepth,
			)
		} else {
			equal = s.simpleConstraintValuesEqualContext(
				typeDefinition.Base,
				lexical,
				enumerationLexical,
				namespaces,
				enumeration.Namespaces,
				childDepth,
			)
		}
		if equal {
			return true
		}
	}
	return false
}

func constraintNumericFacetValid(primitive string, lexical string, facet xsd.Facet) bool {
	if primitive == "decimal" {
		value, err := datatype.ParseDecimal(lexical)
		if err != nil {
			return false
		}
		switch facet.Kind {
		case xsd.FacetTotalDigits:
			bound, parseErr := strconv.Atoi(facet.Value)
			if parseErr != nil {
				return false
			}
			return value.TotalDigits() <= bound
		case xsd.FacetFractionDigits:
			bound, parseErr := strconv.Atoi(facet.Value)
			if parseErr != nil {
				return false
			}
			if bound < 0 {
				return false
			}
			return value.FractionDigits() <= bound
		}
		boundary, parseErr := datatype.ParseDecimal(facet.Value)
		if parseErr != nil {
			return false
		}
		return constraintComparisonValid(value.Compare(boundary), facet.Kind)
	}
	switch primitive {
	case "float", "double":
		bitSize := 64
		if primitive == "float" {
			bitSize = 32
		}
		value, valueOK := constraintFloat(lexical, bitSize)
		boundary, boundaryOK := constraintFloat(facet.Value, bitSize)
		if !valueOK {
			return false
		}
		if !boundaryOK {
			return false
		}
		if math.IsNaN(value) {
			return false
		}
		if math.IsNaN(boundary) {
			return false
		}
		comparison := 0
		if value < boundary {
			comparison = -1
		} else if value > boundary {
			comparison = 1
		}
		return constraintComparisonValid(comparison, facet.Kind)
	}
	comparison, comparable := datatype.CompareOrdered(primitive, lexical, facet.Value)
	return comparable && constraintComparisonValid(comparison, facet.Kind)
}

func constraintComparisonValid(comparison int, kind xsd.FacetKind) bool {
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

func constraintAtomicValuesEqual(primitive string, left string, right string) bool {
	switch primitive {
	case "boolean":
		return (left == "true" || left == "1") == (right == "true" || right == "1")
	case "decimal":
		leftValue, leftErr := datatype.ParseDecimal(left)
		rightValue, rightErr := datatype.ParseDecimal(right)
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return false
		}
		return leftValue.Compare(rightValue) == 0
	case "float", "double":
		bitSize := 64
		if primitive == "float" {
			bitSize = 32
		}
		leftValue, leftOK := constraintFloat(left, bitSize)
		rightValue, rightOK := constraintFloat(right, bitSize)
		if !leftOK {
			return false
		}
		if !rightOK {
			return false
		}
		if math.IsNaN(leftValue) {
			return math.IsNaN(rightValue)
		}
		return leftValue == rightValue
	case "hexBinary":
		leftValue, leftErr := hex.DecodeString(left)
		rightValue, rightErr := hex.DecodeString(right)
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return false
		}
		return string(leftValue) == string(rightValue)
	case "base64Binary":
		leftValue, leftErr := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(left), ""))
		rightValue, rightErr := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(right), ""))
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return false
		}
		return string(leftValue) == string(rightValue)
	default:
		if comparison, comparable := datatype.CompareOrdered(primitive, left, right); comparable {
			return comparison == 0
		}
		return left == right
	}
}

func resolveConstraintQName(
	lexical string,
	namespaces map[string]string,
) (xsd.QName, bool) {
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

func (s *compileState) simpleConstraintValuesEqualContext(
	typeName xsd.QName,
	left string,
	right string,
	leftNamespaces map[string]string,
	rightNamespaces map[string]string,
	depth int,
) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	if typeName.Namespace != xsd.Namespace {
		typeDefinition, ok := s.simpleTypes[typeName]
		if !ok {
			return false
		}
		return s.inlineConstraintValuesEqualContext(
			typeDefinition,
			left,
			right,
			leftNamespaces,
			rightNamespaces,
			compileChildDepth(depth),
		)
	}
	whitespace, _ := s.namedWhitespace(typeName, depth)
	left = normalizeConstraintWhitespace(left, whitespace)
	right = normalizeConstraintWhitespace(right, whitespace)
	shape := s.namedShape(typeName, depth)
	if shape.variety == listShape {
		leftItems := strings.Fields(left)
		rightItems := strings.Fields(right)
		if len(leftItems) != len(rightItems) {
			return false
		}
		for index := range leftItems {
			if leftItems[index] != rightItems[index] {
				return false
			}
		}
		return true
	}
	if shape.primitive == "QName" || shape.primitive == "NOTATION" {
		leftName, leftOK := resolveConstraintQName(left, leftNamespaces)
		rightName, rightOK := resolveConstraintQName(right, rightNamespaces)
		if !leftOK {
			return false
		}
		if !rightOK {
			return false
		}
		return leftName == rightName
	}
	return constraintAtomicValuesEqual(shape.primitive, left, right)
}

func (s *compileState) inlineConstraintValuesEqualContext(
	typeDefinition xsd.SimpleType,
	left string,
	right string,
	leftNamespaces map[string]string,
	rightNamespaces map[string]string,
	depth int,
) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	childDepth := compileChildDepth(depth)
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.inlineConstraintValuesEqualContext(
				*typeDefinition.InlineBase,
				left,
				right,
				leftNamespaces,
				rightNamespaces,
				childDepth,
			)
		}
		return s.simpleConstraintValuesEqualContext(
			typeDefinition.Base,
			left,
			right,
			leftNamespaces,
			rightNamespaces,
			compileChildDepth(depth),
		)
	case xsd.SimpleList:
		leftItems := strings.Fields(left)
		rightItems := strings.Fields(right)
		if len(leftItems) != len(rightItems) {
			return false
		}
		for index := range leftItems {
			equal := s.simpleConstraintValuesEqualContext(
				typeDefinition.ItemType,
				leftItems[index],
				rightItems[index],
				leftNamespaces,
				rightNamespaces,
				childDepth,
			)
			if typeDefinition.InlineItem != nil {
				equal = s.inlineConstraintValuesEqualContext(
					*typeDefinition.InlineItem,
					leftItems[index],
					rightItems[index],
					leftNamespaces,
					rightNamespaces,
					childDepth,
				)
			}
			if !equal {
				return false
			}
		}
		return true
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			leftValid := s.simpleConstraintValidDepthContext(
				member,
				left,
				leftNamespaces,
				childDepth,
			)
			rightValid := s.simpleConstraintValidDepthContext(
				member,
				right,
				rightNamespaces,
				childDepth,
			)
			switch constraintValidityPair(leftValid, rightValid) {
			case bothConstraintsValid:
				return s.simpleConstraintValuesEqualContext(
					member,
					left,
					right,
					leftNamespaces,
					rightNamespaces,
					childDepth,
				)
			case oneConstraintValid:
				return false
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			leftValid := s.inlineConstraintValidDepthContext(
				member,
				left,
				leftNamespaces,
				childDepth,
			)
			rightValid := s.inlineConstraintValidDepthContext(
				member,
				right,
				rightNamespaces,
				childDepth,
			)
			switch constraintValidityPair(leftValid, rightValid) {
			case bothConstraintsValid:
				return s.inlineConstraintValuesEqualContext(
					member,
					left,
					right,
					leftNamespaces,
					rightNamespaces,
					childDepth,
				)
			case oneConstraintValid:
				return false
			}
		}
	}
	return false
}

type constraintValidity uint8

const (
	noConstraintsValid constraintValidity = iota
	oneConstraintValid
	bothConstraintsValid
)

func constraintValidityPair(left, right bool) constraintValidity {
	if left {
		if right {
			return bothConstraintsValid
		}
		return oneConstraintValid
	}
	if right {
		return oneConstraintValid
	}
	return noConstraintsValid
}

func constraintFloat(lexical string, bitSize int) (float64, bool) {
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

type simpleShape struct {
	variety   string
	primitive string
}

const (
	atomicShape = "atomic"
	listShape   = "list"
	unionShape  = "union"
)

func (s *compileState) validateRestrictionFacets(typeDefinition xsd.SimpleType) error {
	shape := s.restrictionBaseShape(typeDefinition)
	seen := make(map[xsd.FacetKind]struct{}, len(typeDefinition.Facets))
	integers := make(map[xsd.FacetKind]datatype.Integer)
	for _, facet := range typeDefinition.Facets {
		if facet.Kind != xsd.FacetPattern && facet.Kind != xsd.FacetEnumeration {
			if _, duplicate := seen[facet.Kind]; duplicate {
				return fmt.Errorf("%w: facet %s occurs more than once", ErrInvalidComponent, facet.Kind)
			}
			seen[facet.Kind] = struct{}{}
		}
		if !facetApplicable(shape, facet.Kind) {
			return fmt.Errorf(
				"%w: facet %s is not applicable to a %s type",
				ErrInvalidComponent,
				facet.Kind,
				shape.variety,
			)
		}
		if facet.Fixed {
			switch facet.Kind {
			case xsd.FacetPattern, xsd.FacetEnumeration:
				return fmt.Errorf("%w: facet %s cannot be fixed", ErrInvalidComponent, facet.Kind)
			}
		}
		switch facet.Kind {
		case xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength,
			xsd.FacetTotalDigits, xsd.FacetFractionDigits:
			value, err := datatype.ParseInteger(facet.Value)
			minimum := datatype.Integer{}
			if facet.Kind == xsd.FacetTotalDigits {
				minimum, _ = datatype.ParseInteger("1")
			}
			if err != nil {
				return fmt.Errorf("%w: facet %s has invalid value %q", ErrInvalidComponent, facet.Kind, facet.Value)
			}
			if value.Compare(minimum) < 0 {
				return fmt.Errorf("%w: facet %s has invalid value %q", ErrInvalidComponent, facet.Kind, facet.Value)
			}
			integers[facet.Kind] = value
		case xsd.FacetWhiteSpace:
			if !validWhitespaceValue(facet.Value) {
				return fmt.Errorf("%w: whiteSpace has invalid value %q", ErrInvalidComponent, facet.Value)
			}
			baseWhitespace, fixed := s.restrictionBaseWhitespace(typeDefinition)
			if whitespaceRank(facet.Value) < whitespaceRank(baseWhitespace) {
				return fmt.Errorf("%w: whiteSpace weakens or changes its fixed base facet", ErrInvalidComponent)
			}
			if fixed && facet.Value != baseWhitespace {
				return fmt.Errorf("%w: whiteSpace weakens or changes its fixed base facet", ErrInvalidComponent)
			}
		case xsd.FacetPattern:
			if _, err := datatype.CompilePattern(facet.Value); err != nil {
				return fmt.Errorf("%w: invalid pattern facet: %v", ErrInvalidComponent, err)
			}
		case xsd.FacetEnumeration, xsd.FacetMinInclusive, xsd.FacetMinExclusive,
			xsd.FacetMaxInclusive, xsd.FacetMaxExclusive:
			if !s.restrictionBaseValueValidContext(
				typeDefinition,
				facet.Value,
				facet.Namespaces,
			) {
				return fmt.Errorf("%w: facet %s value %q is not valid for its base", ErrInvalidComponent, facet.Kind, facet.Value)
			}
		}
	}
	if _, inclusive := seen[xsd.FacetMinInclusive]; inclusive {
		if _, exclusive := seen[xsd.FacetMinExclusive]; exclusive {
			return fmt.Errorf("%w: minInclusive and minExclusive are mutually exclusive", ErrInvalidComponent)
		}
	}
	if _, inclusive := seen[xsd.FacetMaxInclusive]; inclusive {
		if _, exclusive := seen[xsd.FacetMaxExclusive]; exclusive {
			return fmt.Errorf("%w: maxInclusive and maxExclusive are mutually exclusive", ErrInvalidComponent)
		}
	}
	if minimum, ok := integers[xsd.FacetMinLength]; ok {
		if maximum, exists := integers[xsd.FacetMaxLength]; exists && minimum.Compare(maximum) > 0 {
			return fmt.Errorf("%w: minLength exceeds maxLength", ErrInvalidComponent)
		}
	}
	if length, ok := integers[xsd.FacetLength]; ok {
		if minimum, exists := integers[xsd.FacetMinLength]; exists && minimum.Compare(length) > 0 {
			return fmt.Errorf("%w: minLength exceeds length", ErrInvalidComponent)
		}
		if maximum, exists := integers[xsd.FacetMaxLength]; exists && length.Compare(maximum) > 0 {
			return fmt.Errorf("%w: length exceeds maxLength", ErrInvalidComponent)
		}
	}
	if fraction, ok := integers[xsd.FacetFractionDigits]; ok {
		if total, exists := integers[xsd.FacetTotalDigits]; exists && fraction.Compare(total) > 0 {
			return fmt.Errorf("%w: fractionDigits exceeds totalDigits", ErrInvalidComponent)
		}
	}
	if fraction, ok := integers[xsd.FacetFractionDigits]; ok {
		if s.restrictionBaseDerivesFromInteger(typeDefinition, 0) &&
			fraction.Compare(datatype.Integer{}) != 0 {
			return fmt.Errorf("%w: fractionDigits for an integer type must be zero", ErrInvalidComponent)
		}
	}
	if err := s.validateFacetRestriction(typeDefinition, integers); err != nil {
		return err
	}
	if err := s.validateOrderedFacetRestriction(typeDefinition, shape.primitive); err != nil {
		return err
	}
	if shape.variety == atomicShape && shape.primitive == "NOTATION" {
		if err := s.validateNotationRestriction(typeDefinition); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) validateOrderedFacetRestriction(
	typeDefinition xsd.SimpleType,
	primitive string,
) error {
	var minimum *xsd.Facet
	var maximum *xsd.Facet
	for index := range typeDefinition.Facets {
		facet := &typeDefinition.Facets[index]
		ordered := true
		switch facet.Kind {
		case xsd.FacetMinInclusive, xsd.FacetMinExclusive:
			minimum = facet
		case xsd.FacetMaxInclusive, xsd.FacetMaxExclusive:
			maximum = facet
		default:
			ordered = false
		}
		if !ordered {
			continue
		}
		base, ok := s.restrictionAncestorFacet(typeDefinition, facet.Kind, 0)
		if !ok {
			continue
		}
		if !base.Fixed {
			continue
		}
		comparison, comparable := constraintOrderedCompare(primitive, facet.Value, base.Value)
		if !comparable || comparison != 0 {
			return fmt.Errorf("%w: fixed facet %s was changed", ErrInvalidComponent, facet.Kind)
		}
	}
	if minimum != nil && maximum != nil {
		if !orderedIntervalValid(primitive, *minimum, *maximum) {
			return fmt.Errorf("%w: minimum and maximum facets are inconsistent", ErrInvalidComponent)
		}
	}
	if minimum != nil {
		if base, ok := s.restrictionAncestorBound(typeDefinition, true, 0); ok &&
			!orderedLowerRestricts(primitive, *minimum, base) {
			return fmt.Errorf("%w: minimum facet weakens its base", ErrInvalidComponent)
		}
		if base, ok := s.restrictionAncestorBound(typeDefinition, false, 0); ok &&
			!orderedIntervalValid(primitive, *minimum, base) {
			return fmt.Errorf("%w: minimum facet conflicts with its base maximum", ErrInvalidComponent)
		}
	}
	if maximum != nil {
		if base, ok := s.restrictionAncestorBound(typeDefinition, false, 0); ok &&
			!orderedUpperRestricts(primitive, *maximum, base) {
			return fmt.Errorf("%w: maximum facet weakens its base", ErrInvalidComponent)
		}
		if base, ok := s.restrictionAncestorBound(typeDefinition, true, 0); ok &&
			!orderedIntervalValid(primitive, base, *maximum) {
			return fmt.Errorf("%w: maximum facet conflicts with its base minimum", ErrInvalidComponent)
		}
	}
	return nil
}

func orderedIntervalValid(primitive string, minimum xsd.Facet, maximum xsd.Facet) bool {
	comparison, comparable := constraintOrderedCompare(primitive, minimum.Value, maximum.Value)
	if !comparable || comparison > 0 {
		return false
	}
	if comparison < 0 {
		return true
	}
	minimumExclusive := minimum.Kind == xsd.FacetMinExclusive
	maximumExclusive := maximum.Kind == xsd.FacetMaxExclusive
	return minimumExclusive == maximumExclusive
}

func orderedLowerRestricts(primitive string, derived xsd.Facet, base xsd.Facet) bool {
	comparison, comparable := constraintOrderedCompare(primitive, derived.Value, base.Value)
	if !comparable || comparison < 0 {
		return false
	}
	if comparison > 0 {
		return true
	}
	return base.Kind != xsd.FacetMinExclusive || derived.Kind == xsd.FacetMinExclusive
}

func orderedUpperRestricts(primitive string, derived xsd.Facet, base xsd.Facet) bool {
	comparison, comparable := constraintOrderedCompare(primitive, derived.Value, base.Value)
	if !comparable || comparison > 0 {
		return false
	}
	if comparison < 0 {
		return true
	}
	return base.Kind != xsd.FacetMaxExclusive || derived.Kind == xsd.FacetMaxExclusive
}

func constraintOrderedCompare(primitive string, left string, right string) (int, bool) {
	if primitive == "decimal" {
		leftValue, leftErr := datatype.ParseDecimal(left)
		rightValue, rightErr := datatype.ParseDecimal(right)
		if leftErr != nil {
			return 0, false
		}
		if rightErr != nil {
			return 0, false
		}
		return leftValue.Compare(rightValue), true
	}
	switch primitive {
	case "float", "double":
		bitSize := 64
		if primitive != "double" {
			bitSize = 32
		}
		leftValue, leftOK := constraintFloat(left, bitSize)
		rightValue, rightOK := constraintFloat(right, bitSize)
		if !leftOK {
			return 0, false
		}
		if !rightOK {
			return 0, false
		}
		if math.IsNaN(leftValue) {
			return 0, false
		}
		if math.IsNaN(rightValue) {
			return 0, false
		}
		if leftValue < rightValue {
			return -1, true
		}
		if leftValue > rightValue {
			return 1, true
		}
		return 0, true
	}
	return datatype.CompareOrdered(primitive, left, right)
}

func (s *compileState) restrictionAncestorBound(
	typeDefinition xsd.SimpleType,
	lower bool,
	depth int,
) (xsd.Facet, bool) {
	if compileDepthExceeded(depth) {
		return xsd.Facet{}, false
	}
	if typeDefinition.InlineBase != nil {
		return s.definitionBound(*typeDefinition.InlineBase, lower, compileChildDepth(depth))
	}
	if typeDefinition.Base.Namespace == xsd.Namespace {
		return xsd.Facet{}, false
	}
	base, ok := s.simpleTypes[typeDefinition.Base]
	if !ok {
		return xsd.Facet{}, false
	}
	return s.definitionBound(base, lower, compileChildDepth(depth))
}

func (s *compileState) definitionBound(
	typeDefinition xsd.SimpleType,
	lower bool,
	depth int,
) (xsd.Facet, bool) {
	if compileDepthExceeded(depth) || typeDefinition.Variety != xsd.SimpleRestriction {
		return xsd.Facet{}, false
	}
	for _, facet := range typeDefinition.Facets {
		if lower {
			switch facet.Kind {
			case xsd.FacetMinInclusive, xsd.FacetMinExclusive:
				return facet, true
			}
		} else {
			switch facet.Kind {
			case xsd.FacetMaxInclusive, xsd.FacetMaxExclusive:
				return facet, true
			}
		}
	}
	return s.restrictionAncestorBound(typeDefinition, lower, compileChildDepth(depth))
}

func (s *compileState) validateFacetRestriction(
	typeDefinition xsd.SimpleType,
	integers map[xsd.FacetKind]datatype.Integer,
) error {
	for kind, value := range integers {
		baseFacet, ok := s.restrictionAncestorFacet(typeDefinition, kind, 0)
		if ok {
			baseValue, err := datatype.ParseInteger(baseFacet.Value)
			if err == nil {
				comparison := value.Compare(baseValue)
				invalid := false
				if baseFacet.Fixed && comparison != 0 {
					invalid = true
				}
				switch kind {
				case xsd.FacetLength:
					if comparison != 0 {
						invalid = true
					}
				case xsd.FacetMinLength:
					if comparison < 0 {
						invalid = true
					}
				case xsd.FacetMaxLength, xsd.FacetTotalDigits, xsd.FacetFractionDigits:
					if comparison > 0 {
						invalid = true
					}
				}
				if invalid {
					return fmt.Errorf("%w: facet %s does not restrict its base facet", ErrInvalidComponent, kind)
				}
			}
		}
	}
	return nil
}

func (s *compileState) restrictionAncestorFacet(
	typeDefinition xsd.SimpleType,
	kind xsd.FacetKind,
	depth int,
) (xsd.Facet, bool) {
	if compileDepthExceeded(depth) {
		return xsd.Facet{}, false
	}
	if typeDefinition.InlineBase != nil {
		return s.definitionFacet(*typeDefinition.InlineBase, kind, compileChildDepth(depth))
	}
	if typeDefinition.Base.Namespace == xsd.Namespace {
		return xsd.Facet{}, false
	}
	base, ok := s.simpleTypes[typeDefinition.Base]
	if !ok {
		return xsd.Facet{}, false
	}
	return s.definitionFacet(base, kind, compileChildDepth(depth))
}

func (s *compileState) definitionFacet(
	typeDefinition xsd.SimpleType,
	kind xsd.FacetKind,
	depth int,
) (xsd.Facet, bool) {
	if compileDepthExceeded(depth) || typeDefinition.Variety != xsd.SimpleRestriction {
		return xsd.Facet{}, false
	}
	for _, facet := range typeDefinition.Facets {
		if facet.Kind == kind {
			return facet, true
		}
	}
	return s.restrictionAncestorFacet(typeDefinition, kind, compileChildDepth(depth))
}

func (s *compileState) restrictionBaseDerivesFromInteger(
	typeDefinition xsd.SimpleType,
	depth int,
) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	if typeDefinition.InlineBase != nil {
		return s.definitionDerivesFromInteger(*typeDefinition.InlineBase, compileChildDepth(depth))
	}
	return s.namedDerivesFromInteger(typeDefinition.Base, compileChildDepth(depth))
}

func (s *compileState) definitionDerivesFromInteger(
	typeDefinition xsd.SimpleType,
	depth int,
) bool {
	if typeDefinition.Variety != xsd.SimpleRestriction {
		return false
	}
	return s.restrictionBaseDerivesFromInteger(typeDefinition, compileChildDepth(depth))
}

func (s *compileState) namedDerivesFromInteger(name xsd.QName, depth int) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	if name.Namespace != xsd.Namespace {
		definition, ok := s.simpleTypes[name]
		if !ok {
			return false
		}
		return s.definitionDerivesFromInteger(definition, compileChildDepth(depth))
	}
	for name.Local != "anySimpleType" {
		if name.Local == "integer" {
			return true
		}
		base, ok := datatype.BuiltInBase(name.Local)
		if !ok {
			return false
		}
		name.Local = base
	}
	return false
}

func (s *compileState) validateNotationRestriction(typeDefinition xsd.SimpleType) error {
	if !s.hasNotationEnumeration(typeDefinition, 0) {
		return fmt.Errorf("%w: NOTATION restriction requires an enumeration", ErrInvalidComponent)
	}
	for _, facet := range typeDefinition.Facets {
		if facet.Kind == xsd.FacetEnumeration {
			name, ok := notationFacetName(facet)
			if !ok {
				return fmt.Errorf("%w: NOTATION enumeration %q has an unbound prefix", ErrInvalidComponent, facet.Value)
			}
			if _, declared := s.notations[name]; !declared {
				return unresolvedComponent("notation", name)
			}
		}
	}
	return nil
}

func (s *compileState) hasNotationEnumeration(typeDefinition xsd.SimpleType, depth int) bool {
	if compileDepthExceeded(depth) {
		return false
	}
	for _, facet := range typeDefinition.Facets {
		if facet.Kind == xsd.FacetEnumeration {
			return true
		}
	}
	if typeDefinition.InlineBase != nil {
		return s.hasNotationEnumeration(*typeDefinition.InlineBase, compileChildDepth(depth))
	}
	if typeDefinition.Base.Namespace == xsd.Namespace {
		return false
	}
	base, ok := s.simpleTypes[typeDefinition.Base]
	if !ok {
		return false
	}
	return s.hasNotationEnumeration(base, compileChildDepth(depth))
}

func notationFacetName(facet xsd.Facet) (xsd.QName, bool) {
	lexical := strings.TrimSpace(facet.Value)
	prefix, local, qualified := strings.Cut(lexical, ":")
	if !qualified {
		return xsd.QName{Namespace: facet.Namespaces[""], Local: lexical}, true
	}
	namespace, ok := facet.Namespaces[prefix]
	return xsd.QName{Namespace: namespace, Local: local}, ok
}

func facetApplicable(shape simpleShape, kind xsd.FacetKind) bool {
	switch shape.variety {
	case listShape:
		switch kind {
		case xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength,
			xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace:
			return true
		}
	case unionShape:
		switch kind {
		case xsd.FacetPattern, xsd.FacetEnumeration:
			return true
		}
	case atomicShape:
		switch kind {
		case xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace:
			return true
		case xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength:
			switch shape.primitive {
			case "string", "anyURI", "hexBinary", "base64Binary", "QName", "NOTATION":
				return true
			}
		case xsd.FacetTotalDigits, xsd.FacetFractionDigits:
			return shape.primitive == "decimal"
		case xsd.FacetMinInclusive, xsd.FacetMinExclusive,
			xsd.FacetMaxInclusive, xsd.FacetMaxExclusive:
			switch shape.primitive {
			case "decimal", "float", "double", "duration", "dateTime", "time", "date",
				"gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
				return true
			}
		}
	}
	return false
}

func (s *compileState) restrictionBaseShape(typeDefinition xsd.SimpleType) simpleShape {
	if typeDefinition.InlineBase != nil {
		return s.definitionShape(*typeDefinition.InlineBase, 0)
	}
	return s.namedShape(typeDefinition.Base, 0)
}

func (s *compileState) definitionShape(typeDefinition xsd.SimpleType, depth int) simpleShape {
	if compileDepthExceeded(depth) {
		return simpleShape{}
	}
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.InlineBase != nil {
			return s.definitionShape(*typeDefinition.InlineBase, compileChildDepth(depth))
		}
		return s.namedShape(typeDefinition.Base, compileChildDepth(depth))
	case xsd.SimpleList:
		return simpleShape{variety: listShape}
	case xsd.SimpleUnion:
		return simpleShape{variety: unionShape}
	default:
		return simpleShape{}
	}
}

func (s *compileState) namedShape(name xsd.QName, depth int) simpleShape {
	if compileDepthExceeded(depth) {
		return simpleShape{}
	}
	if name.Namespace != xsd.Namespace {
		definition, ok := s.simpleTypes[name]
		if !ok {
			return simpleShape{}
		}
		return s.definitionShape(definition, compileChildDepth(depth))
	}
	local := name.Local
	for {
		base, method, ok := datatype.BuiltInDerivation(local)
		if !ok {
			return simpleShape{variety: atomicShape, primitive: local}
		}
		if method == listShape {
			return simpleShape{variety: listShape}
		}
		if base == "anySimpleType" {
			return simpleShape{variety: atomicShape, primitive: local}
		}
		local = base
	}
}

func validWhitespaceValue(value string) bool {
	return value == "preserve" || value == "replace" || value == "collapse"
}

func whitespaceRank(value string) int {
	switch value {
	case "collapse":
		return 2
	case "replace":
		return 1
	default:
		return 0
	}
}

func (s *compileState) restrictionBaseWhitespace(typeDefinition xsd.SimpleType) (string, bool) {
	if typeDefinition.InlineBase != nil {
		return s.definitionWhitespace(*typeDefinition.InlineBase, 0)
	}
	return s.namedWhitespace(typeDefinition.Base, 0)
}

func (s *compileState) definitionWhitespace(typeDefinition xsd.SimpleType, depth int) (string, bool) {
	if compileDepthExceeded(depth) {
		return "", false
	}
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		var base string
		var fixed bool
		if typeDefinition.InlineBase != nil {
			base, fixed = s.definitionWhitespace(*typeDefinition.InlineBase, compileChildDepth(depth))
		} else {
			base, fixed = s.namedWhitespace(typeDefinition.Base, compileChildDepth(depth))
		}
		for _, facet := range typeDefinition.Facets {
			if facet.Kind == xsd.FacetWhiteSpace {
				return facet.Value, fixed || facet.Fixed
			}
		}
		return base, fixed
	case xsd.SimpleList:
		return "collapse", true
	default:
		return "", false
	}
}

func (s *compileState) namedWhitespace(name xsd.QName, depth int) (string, bool) {
	if compileDepthExceeded(depth) {
		return "", false
	}
	if name.Namespace != xsd.Namespace {
		definition, ok := s.simpleTypes[name]
		if !ok {
			return "", false
		}
		return s.definitionWhitespace(definition, compileChildDepth(depth))
	}
	shape := s.namedShape(name, depth)
	if shape.variety == listShape {
		return "collapse", true
	}
	switch name.Local {
	case "string":
		return "preserve", false
	case "normalizedString":
		return "replace", false
	default:
		if shape.primitive == "string" {
			return "collapse", name.Local != "token"
		}
		return "collapse", true
	}
}

func (s *compileState) restrictionBaseValueValid(typeDefinition xsd.SimpleType, lexical string) bool {
	return s.restrictionBaseValueValidContext(typeDefinition, lexical, nil)
}

func (s *compileState) restrictionBaseValueValidContext(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) bool {
	if typeDefinition.InlineBase != nil {
		return s.inlineConstraintValidContext(*typeDefinition.InlineBase, lexical, namespaces)
	}
	return s.simpleConstraintValidContext(typeDefinition.Base, lexical, namespaces)
}
