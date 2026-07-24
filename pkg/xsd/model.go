package xsd

import (
	"sort"
	"strings"
)

const Namespace = "http://www.w3.org/2001/XMLSchema"

// Form controls whether locally declared element or attribute names are
// namespace-qualified in instance documents.
type Form string

const (
	FormUnqualified Form = "unqualified"
	FormQualified   Form = "qualified"
)

// Derivation is an XML Schema derivation or substitution control token.
type Derivation string

const (
	DerivationExtension    Derivation = "extension"
	DerivationRestriction  Derivation = "restriction"
	DerivationSubstitution Derivation = "substitution"
	DerivationList         Derivation = "list"
	DerivationUnion        Derivation = "union"
)

// DerivationSet represents blockDefault and finalDefault values.
// The zero value is an empty set.
type DerivationSet struct {
	all    bool
	values map[Derivation]struct{}
}

// All reports whether the lexical value was #all.
func (s DerivationSet) All() bool { return s.all }

// Contains reports whether a derivation is in the set. #all contains every
// derivation.
func (s DerivationSet) Contains(value Derivation) bool {
	if s.all {
		return true
	}
	_, ok := s.values[value]
	return ok
}

// String returns the deterministic XML Schema lexical representation.
func (s DerivationSet) String() string {
	if s.all {
		return "#all"
	}
	values := make([]string, 0, len(s.values))
	for value := range s.values {
		values = append(values, string(value))
	}
	sort.Strings(values)
	return strings.Join(values, " ")
}

// Annotation represents xs:annotation content retained by the parser.
type Annotation struct {
	ID             string
	Documentation  []Documentation
	AppInformation []AppInfo
}

// AppInfo preserves machine-readable xs:appinfo content.
type AppInfo struct {
	ID      string
	Source  string
	Content string
}

// Documentation represents human-readable xs:documentation content.
type Documentation struct {
	ID       string
	Source   string
	Language string
	Content  string
	Markup   string
}

// Notation is a global xs:notation declaration.
type Notation struct {
	ID         string
	Name       string
	Public     string
	System     string
	Annotation *Annotation
}

// ReferenceKind identifies a schema composition directive.
type ReferenceKind string

const (
	ReferenceInclude  ReferenceKind = "include"
	ReferenceImport   ReferenceKind = "import"
	ReferenceRedefine ReferenceKind = "redefine"
)

// SchemaReference preserves an include, import, or redefine in source order.
// Location is the lexical schemaLocation; URI is resolved against xml:base and
// the document SystemID without loading it.
type SchemaReference struct {
	ID         string
	Kind       ReferenceKind
	Namespace  string
	Location   string
	URI        string
	Annotation *Annotation
}

// QName is an expanded XML qualified name.
type QName struct {
	Namespace string
	Local     string
}

// IdentityKind identifies an xs:unique, xs:key, or xs:keyref constraint.
type IdentityKind string

const (
	IdentityUnique IdentityKind = "unique"
	IdentityKey    IdentityKind = "key"
	IdentityKeyRef IdentityKind = "keyref"
)

// IdentityConstraint preserves the XML Schema identity XPath subset.
type IdentityConstraint struct {
	ID                 string
	TargetNamespace    string
	Name               string
	Kind               IdentityKind
	Refer              QName
	Selector           string
	SelectorID         string
	Fields             []string
	FieldIDs           []string
	Namespaces         map[string]string
	Annotation         *Annotation
	SelectorAnnotation *Annotation
	FieldAnnotations   []*Annotation
}

// Element is a global element declaration.
type Element struct {
	ID                  string
	Name                string
	Namespace           string
	Type                QName
	Ref                 QName
	Form                Form
	Abstract            bool
	Nillable            bool
	Default             string
	Fixed               string
	DefaultSet          bool
	FixedSet            bool
	ValueNamespaces     map[string]string
	Block               DerivationSet
	Final               DerivationSet
	IdentityConstraints []IdentityConstraint
	InlineSimpleType    *SimpleType
	InlineComplexType   *ComplexType
	TargetNamespace     string
	SubstitutionGroup   QName
	Annotation          *Annotation
}

// Attribute is a global attribute declaration.
type Attribute struct {
	ID               string
	Name             string
	Type             QName
	Default          string
	Fixed            string
	DefaultSet       bool
	FixedSet         bool
	ValueNamespaces  map[string]string
	InlineSimpleType *SimpleType
	Annotation       *Annotation
}

// AttributeUseKind controls whether an attribute use is optional, required,
// or prohibited.
type AttributeUseKind string

const (
	AttributeOptional   AttributeUseKind = "optional"
	AttributeRequired   AttributeUseKind = "required"
	AttributeProhibited AttributeUseKind = "prohibited"
)

// AttributeUse is an attribute use within a complex type.
type AttributeUse struct {
	ID               string
	Name             string
	Namespace        string
	Ref              QName
	Type             QName
	Form             Form
	Use              AttributeUseKind
	Default          string
	Fixed            string
	DefaultSet       bool
	FixedSet         bool
	ValueNamespaces  map[string]string
	InlineSimpleType *SimpleType
	Annotation       *Annotation
}

// Compositor identifies an XML Schema model group compositor.
type Compositor string

const (
	Sequence Compositor = "sequence"
	Choice   Compositor = "choice"
	All      Compositor = "all"
)

// ModelGroup is a sequence, choice, or all group.
type ModelGroup struct {
	ID         string
	Compositor Compositor
	Particles  []Particle
	MinOccurs  uint64
	MaxOccurs  uint64
	Unbounded  bool
	OccursSet  bool
	Annotation *Annotation
}

// Particle applies occurrence constraints to an element or nested group.
type Particle struct {
	ID         string
	MinOccurs  uint64
	MaxOccurs  uint64
	Unbounded  bool
	Element    *Element
	Group      *ModelGroup
	GroupRef   QName
	Wildcard   *Wildcard
	Annotation *Annotation
}

// ProcessContents controls validation for declarations matched by a wildcard.
type ProcessContents string

const (
	ProcessStrict ProcessContents = "strict"
	ProcessLax    ProcessContents = "lax"
	ProcessSkip   ProcessContents = "skip"
)

// Wildcard constrains namespaces and validation for xs:any or xs:anyAttribute.
type Wildcard struct {
	ID              string
	Namespaces      []string
	ProcessContents ProcessContents
	Annotation      *Annotation
}

// ModelGroupDefinition is a named global model group.
type ModelGroupDefinition struct {
	ID         string
	Name       string
	Content    *ModelGroup
	Annotation *Annotation
}

// AttributeGroup is a named global collection of attribute uses.
type AttributeGroup struct {
	ID                       string
	Name                     string
	Attributes               []AttributeUse
	References               []QName
	Wildcard                 *Wildcard
	Annotation               *Annotation
	AttributeGroupReferences []AttributeGroupReference
}

// AttributeGroupReference preserves an attribute-group reference annotation.
type AttributeGroupReference struct {
	ID         string
	Ref        QName
	Annotation *Annotation
}

// SimpleVariety identifies restriction, list, or union construction.
type SimpleVariety string

const (
	SimpleRestriction SimpleVariety = "restriction"
	SimpleList        SimpleVariety = "list"
	SimpleUnion       SimpleVariety = "union"
)

// FacetKind identifies an XML Schema constraining facet.
type FacetKind string

const (
	FacetLength         FacetKind = "length"
	FacetMinLength      FacetKind = "minLength"
	FacetMaxLength      FacetKind = "maxLength"
	FacetPattern        FacetKind = "pattern"
	FacetEnumeration    FacetKind = "enumeration"
	FacetWhiteSpace     FacetKind = "whiteSpace"
	FacetMaxInclusive   FacetKind = "maxInclusive"
	FacetMaxExclusive   FacetKind = "maxExclusive"
	FacetMinInclusive   FacetKind = "minInclusive"
	FacetMinExclusive   FacetKind = "minExclusive"
	FacetTotalDigits    FacetKind = "totalDigits"
	FacetFractionDigits FacetKind = "fractionDigits"
)

// Facet preserves one restriction facet in source order.
type Facet struct {
	ID         string
	Kind       FacetKind
	Value      string
	Fixed      bool
	Namespaces map[string]string
	Annotation *Annotation
}

// SimpleType is a global simple type definition.
type SimpleType struct {
	ID                string
	Name              string
	Variety           SimpleVariety
	Final             DerivationSet
	Base              QName
	InlineBase        *SimpleType
	Facets            []Facet
	ItemType          QName
	InlineItem        *SimpleType
	MemberTypes       []QName
	InlineMembers     []SimpleType
	Annotation        *Annotation
	VarietyAnnotation *Annotation
	VarietyID         string
}

// ComplexType is a global complex type definition.
type ComplexType struct {
	ID                       string
	Name                     string
	Base                     QName
	SimpleBase               QName
	InlineSimpleType         *SimpleType
	SimpleFacets             []Facet
	Derivation               Derivation
	SimpleContent            bool
	Abstract                 bool
	Mixed                    bool
	MixedSet                 bool
	Block                    DerivationSet
	Final                    DerivationSet
	Content                  *ModelGroup
	Attributes               []AttributeUse
	AttributeGroupRefs       []QName
	AttributeGroupReferences []AttributeGroupReference
	AttributeWildcard        *Wildcard
	Annotation               *Annotation
	ContentAnnotation        *Annotation
	DerivationAnnotation     *Annotation
	ContentID                string
	DerivationID             string
}

// Document is an XML Schema schema document. Component declarations are
// populated in source order so deterministic serialization is possible.
type Document struct {
	ID                   string
	SystemID             string
	BaseURI              string
	TargetNamespace      string
	Namespaces           map[string]string
	ElementFormDefault   Form
	AttributeFormDefault Form
	BlockDefault         DerivationSet
	FinalDefault         DerivationSet
	Version              string
	Language             string
	Annotations          []Annotation
	References           []SchemaReference
	Redefinitions        []Redefinition
	Elements             []Element
	Attributes           []Attribute
	SimpleTypes          []SimpleType
	ComplexTypes         []ComplexType
	ModelGroups          []ModelGroupDefinition
	AttributeGroups      []AttributeGroup
	Notations            []Notation
}

// Redefinition contains the declarations nested in xs:redefine.
type Redefinition struct {
	Reference       SchemaReference
	SimpleTypes     []SimpleType
	ComplexTypes    []ComplexType
	ModelGroups     []ModelGroupDefinition
	AttributeGroups []AttributeGroup
}
