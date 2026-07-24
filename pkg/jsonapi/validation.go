package jsonapi

import (
	"fmt"
	"mime"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

var registeredLinkRelation = regexp.MustCompile(`^[a-z][a-z0-9.-]*$`)

// Violation describes one JSON:API conformance failure.
type Violation struct {
	Path    string
	Code    string
	Message string
}

// ValidationError reports every conformance violation found in a document.
type ValidationError struct {
	Violations []Violation
	causes     []error
}

// ValidationContext identifies the protocol boundary at which a document is
// being validated.
type ValidationContext uint8

const (
	// GenericDocument applies context-independent JSON:API document rules.
	GenericDocument ValidationContext = iota
	// Response applies server response identity rules.
	Response
	// CreateRequest applies resource creation request rules.
	CreateRequest
	// UpdateRequest applies resource update request rules.
	UpdateRequest
	// ToOneRelationshipRequest applies to-one relationship mutation rules.
	ToOneRelationshipRequest
	// ToManyRelationshipRequest applies to-many relationship mutation rules.
	ToManyRelationshipRequest
)

// ValidationOptions configures context and optional endpoint identity checks.
type ValidationOptions struct {
	Context      ValidationContext
	ExpectedType string
	ExpectedID   string
	// ExpectedIDPresent enables endpoint identity matching when ExpectedID is
	// empty. A non-empty ExpectedID enables matching without this flag.
	ExpectedIDPresent bool
	// SparseFieldsetsOmittedLinkage applies the sole specification exception
	// to full linkage. Set it only when requested sparse fieldsets omitted the
	// relationship fields that would otherwise link included resources.
	SparseFieldsetsOmittedLinkage bool
}

// Error implements error.
func (err *ValidationError) Error() string {
	if len(err.Violations) == 0 {
		return "JSON:API validation failed"
	}

	first := err.Violations[0]
	if len(err.Violations) == 1 {
		return fmt.Sprintf("JSON:API validation failed at %q: %s", first.Path, first.Message)
	}

	return fmt.Sprintf(
		"JSON:API validation failed at %q: %s (and %d more violations)",
		first.Path,
		first.Message,
		len(err.Violations)-1,
	)
}

// Unwrap exposes application callback failures without including their text
// in the public validation message.
func (err *ValidationError) Unwrap() []error {
	return err.causes
}

// Validate checks the context-independent structural requirements of a
// JSON:API document and returns all violations in document order.
func (document Document) Validate() error {
	return document.ValidateWith(ValidationOptions{})
}

// ValidateWith checks a document using rules for a specific request or
// response boundary.
func (document Document) ValidateWith(options ValidationOptions) error {
	validator := documentValidator{options: options}
	validator.validateDocument(document)
	if len(validator.violations) == 0 {
		return nil
	}

	return &ValidationError{Violations: validator.violations}
}

type documentValidator struct {
	violations []Violation
	causes     []error
	options    ValidationOptions
}

type identityRequirement uint8

const (
	identityEither identityRequirement = iota
	identityOptional
	identityID
)

func (validator *documentValidator) validateDocument(document Document) {
	if validator.options.Context > ToManyRelationshipRequest {
		validator.add("", "validation-context", "validation context is invalid")
	}
	if document.Data == nil && document.Errors == nil && document.Meta == nil &&
		!hasNonAtMember(document.AdditionalMembers) {
		validator.add("", "required", "document must contain data, errors, or meta")
	}
	if document.Data != nil && document.Errors != nil {
		validator.add("/errors", "conflict", "data and errors must not coexist")
	}
	if document.Data == nil && document.Included != nil {
		validator.add("/included", "requires-data", "included requires top-level data")
	}
	if validator.requestContext() {
		if document.Data == nil {
			validator.add("/data", "required", "request document must contain data")
		}
		if document.Errors != nil {
			validator.add("/errors", "forbidden", "request document must not contain errors")
		}
	}

	if document.JSONAPI != nil {
		validator.validateJSONAPI(*document.JSONAPI)
	}
	validator.validateLinks(document.Links, "/links")
	validator.validateLinkScope(
		document.Links,
		"/links",
		"self", "related", "describedby", "first", "last", "prev", "next",
	)
	validator.validatePaginationLinks(
		document.Links,
		"/links",
		document.Data != nil && document.Data.kind == primaryDataMany,
	)
	switch validator.options.Context {
	case CreateRequest:
		validator.validateResourceMutation(document.Data, "/data", identityOptional)
	case UpdateRequest:
		validator.validateResourceMutation(document.Data, "/data", identityID)
	case ToOneRelationshipRequest:
		validator.validateRelationshipPrimaryData(document.Data, "/data", false)
	case ToManyRelationshipRequest:
		validator.validateRelationshipPrimaryData(document.Data, "/data", true)
	case Response:
		validator.validatePrimaryData(document.Data, "/data", identityID, false, identityID)
	default:
		validator.validatePrimaryData(document.Data, "/data", identityEither, false, identityEither)
	}
	validator.validateIncluded(document, validator.includedIdentity())
	validator.validateDocumentIdentity(document)
	for index, apiError := range document.Errors {
		validator.validateError(apiError, "/errors/"+strconv.Itoa(index))
	}
}

func (validator *documentValidator) requestContext() bool {
	return validator.options.Context == CreateRequest ||
		validator.options.Context == UpdateRequest ||
		validator.options.Context == ToOneRelationshipRequest ||
		validator.options.Context == ToManyRelationshipRequest
}

func (validator *documentValidator) includedIdentity() identityRequirement {
	if validator.options.Context == Response {
		return identityID
	}

	return identityEither
}

func (validator *documentValidator) validateJSONAPI(object JSONAPI) {
	validator.validateURIList(object.Ext, "/jsonapi/ext")
	validator.validateURIList(object.Profile, "/jsonapi/profile")
}

func (validator *documentValidator) validateURIList(values []string, path string) {
	seen := make(map[string]int, len(values))
	for index, value := range values {
		itemPath := path + "/" + strconv.Itoa(index)
		absolute, valid := parseURIReference(value)
		if value == "" || !valid || !absolute {
			validator.add(itemPath, "uri", "value must be an absolute URI")
		}
		if previous, exists := seen[value]; exists {
			validator.add(
				itemPath,
				"duplicate-uri",
				fmt.Sprintf("URI duplicates item at index %d", previous),
			)
		} else {
			seen[value] = index
		}
	}
}

func (validator *documentValidator) validateAppliedURIs(
	declared []string,
	applied []string,
	path string,
	kind string,
	allowUnknown bool,
) {
	if declared == nil {
		return
	}
	expected := make(map[string]struct{}, len(applied))
	for _, value := range applied {
		expected[value] = struct{}{}
	}
	seen := make(map[string]struct{}, len(declared))
	for index, value := range declared {
		seen[value] = struct{}{}
		if _, exists := expected[value]; !exists && !allowUnknown {
			validator.add(
				path+"/"+strconv.Itoa(index),
				"unsupported-"+kind,
				kind+" declaration is not configured as applied",
			)
		}
	}
	for _, value := range applied {
		if _, exists := seen[value]; !exists {
			validator.add(
				path,
				"missing-"+kind,
				kind+" declaration omits an applied URI",
			)
		}
	}
}

func (validator *documentValidator) validatePrimaryData(
	data *PrimaryData,
	path string,
	identity identityRequirement,
	requireRelationshipData bool,
	identifierIdentity identityRequirement,
) {
	if data == nil || data.kind == primaryDataNull {
		return
	}
	if data.kind == primaryDataOne {
		if data.one == nil {
			validator.add(path, "required", "single primary data must contain a resource")
			return
		}
		validator.validateResource(*data.one, path, identity, requireRelationshipData, identifierIdentity)
		return
	}
	if data.kind != primaryDataMany {
		validator.add(path, "shape", "primary data must be null, a resource, or an array")
		return
	}
	for index, resource := range data.many {
		validator.validateResource(
			resource,
			path+"/"+strconv.Itoa(index),
			identity,
			requireRelationshipData,
			identifierIdentity,
		)
	}
}

func (validator *documentValidator) validateResource(
	resource ResourceObject,
	path string,
	identity identityRequirement,
	requireRelationshipData bool,
	identifierIdentity identityRequirement,
) {
	if resource.Type == "" {
		validator.add(path+"/type", "required", "resource type is required")
	} else if !validImplementationMemberName(resource.Type) {
		validator.add(path+"/type", "member-name", "resource type must be a valid member name")
	}
	if identity == identityID && !resource.hasID() {
		validator.add(path+"/id", "required", "resource id is required")
	} else if identity == identityEither && !resource.hasID() && !resource.hasLID() {
		validator.add(path+"/id", "required", "resource id or lid is required")
	}

	for name := range resource.Attributes {
		fieldPath := path + "/attributes/" + escapePointerToken(name)
		if strings.HasPrefix(name, "@") {
			if !validMemberName(name) {
				validator.add(fieldPath, "member-name", "@-Member name is invalid")
			}
			continue
		}
		if name == "id" || name == "type" {
			validator.add(fieldPath, "reserved-field", "attribute name conflicts with resource identity")
		} else if !validMemberName(name) {
			validator.add(fieldPath, "member-name", "attribute name is invalid")
		}
	}
	for name, relationship := range resource.Relationships {
		fieldPath := path + "/relationships/" + escapePointerToken(name)
		if strings.HasPrefix(name, "@") {
			if !validMemberName(name) {
				validator.add(fieldPath, "member-name", "@-Member name is invalid")
			}
			continue
		}
		if name == "id" || name == "type" {
			validator.add(fieldPath, "reserved-field", "relationship name conflicts with resource identity")
		} else if !validMemberName(name) {
			validator.add(fieldPath, "member-name", "relationship name is invalid")
		}
		if _, exists := resource.Attributes[name]; exists && !strings.HasPrefix(name, "@") {
			validator.add(fieldPath, "duplicate-field", "attribute and relationship names must be unique")
		}
		validator.validateRelationship(
			relationship,
			fieldPath,
			requireRelationshipData,
			identifierIdentity,
		)
	}
	validator.validateLinks(resource.Links, path+"/links")
	validator.validateLinkScope(resource.Links, path+"/links", "self")
}

func (validator *documentValidator) validateRelationship(
	relationship Relationship,
	path string,
	requireData bool,
	identifierIdentity identityRequirement,
) {
	if !hasRelationshipLink(relationship.Links) && relationship.Data == nil && relationship.Meta == nil &&
		!hasNonAtMember(relationship.AdditionalMembers) {
		validator.add(
			path,
			"required",
			"relationship must contain self or related links, data, meta, or an extension member",
		)
	}
	if requireData && relationship.Data == nil {
		validator.add(path+"/data", "required", "resource mutation relationships require data")
	}
	validator.validateLinks(relationship.Links, path+"/links")
	validator.validateLinkScope(
		relationship.Links,
		path+"/links",
		"self", "related", "first", "last", "prev", "next",
	)
	validator.validatePaginationLinks(
		relationship.Links,
		path+"/links",
		relationship.Data == nil || relationship.Data.kind == relationshipDataMany,
	)
	validator.validateRelationshipData(relationship.Data, path+"/data", identifierIdentity)
}

func hasNonAtMember(members Members) bool {
	for name := range members {
		if !strings.HasPrefix(name, "@") {
			return true
		}
	}
	return false
}

func hasRelationshipLink(links Links) bool {
	for name, link := range links {
		if name == "self" || name == "related" || link.extension {
			return true
		}
	}
	return false
}

func validExtensionMemberName(name string) bool {
	namespace, member, found := strings.Cut(name, ":")
	return found && validExtensionNamespace(namespace) && validImplementationMemberName(member)
}

func validImplementationMemberName(name string) bool {
	return !strings.HasPrefix(name, "@") && validMemberName(name)
}

func (validator *documentValidator) validateRelationshipData(
	data *RelationshipData,
	path string,
	identity identityRequirement,
) {
	if data == nil || data.kind == relationshipDataNull {
		return
	}
	if data.kind == relationshipDataOne {
		if data.one == nil {
			validator.add(path, "required", "to-one linkage must contain an identifier")
			return
		}
		validator.validateIdentifier(*data.one, path, identity)
		return
	}
	if data.kind != relationshipDataMany {
		validator.add(path, "shape", "linkage must be null, an identifier, or an array")
		return
	}
	for index, identifier := range data.many {
		validator.validateIdentifier(identifier, path+"/"+strconv.Itoa(index), identity)
	}
}

func (validator *documentValidator) validateIdentifier(
	identifier Identifier,
	path string,
	identity identityRequirement,
) {
	if identifier.Type == "" {
		validator.add(path+"/type", "required", "resource identifier type is required")
	} else if !validImplementationMemberName(identifier.Type) {
		validator.add(path+"/type", "member-name", "resource identifier type must be a valid member name")
	}
	if identity == identityID && !identifier.hasID() {
		validator.add(path+"/id", "required", "resource identifier id is required")
	} else if identity == identityEither && !identifier.hasID() && !identifier.hasLID() {
		validator.add(path+"/id", "required", "resource identifier requires id or lid")
	}
}

func (validator *documentValidator) validateResourceMutation(
	data *PrimaryData,
	path string,
	identity identityRequirement,
) {
	if data == nil {
		return
	}
	if data.kind != primaryDataOne || data.one == nil {
		validator.add(path, "shape", "resource mutation data must be one resource object")
		return
	}
	resource := *data.one
	validator.validateResource(resource, path, identity, true, identityEither)
	validator.validateExpectedIdentity(resource, path)
}

func (validator *documentValidator) validateExpectedIdentity(resource ResourceObject, path string) {
	if validator.options.ExpectedType != "" && resource.Type != "" &&
		resource.Type != validator.options.ExpectedType {
		validator.add(path+"/type", "endpoint-mismatch", "resource type does not match endpoint")
	}
	if (validator.options.ExpectedIDPresent || validator.options.ExpectedID != "") &&
		resource.hasID() &&
		resource.ID != validator.options.ExpectedID {
		validator.add(path+"/id", "endpoint-mismatch", "resource id does not match endpoint")
	}
}

func (validator *documentValidator) validateRelationshipPrimaryData(
	data *PrimaryData,
	path string,
	many bool,
) {
	if data == nil {
		return
	}
	if !many && data.kind == primaryDataNull {
		return
	}
	if !many && data.kind == primaryDataOne && data.one != nil {
		validator.validatePrimaryIdentifier(*data.one, path)
		return
	}
	if many && data.kind == primaryDataMany {
		for index, resource := range data.many {
			validator.validatePrimaryIdentifier(resource, path+"/"+strconv.Itoa(index))
		}
		return
	}
	validator.add(path, "shape", "relationship data has the wrong to-one or to-many shape")
}

func (validator *documentValidator) validatePrimaryIdentifier(resource ResourceObject, path string) {
	validator.validateIdentifier(Identifier{
		Type:       resource.Type,
		ID:         resource.ID,
		LID:        resource.LID,
		Meta:       resource.Meta,
		idPresent:  resource.idPresent,
		lidPresent: resource.lidPresent,
	}, path, identityID)
	if resource.Attributes != nil {
		validator.add(path+"/attributes", "forbidden", "resource identifier must not contain attributes")
	}
	if resource.Relationships != nil {
		validator.add(path+"/relationships", "forbidden", "resource identifier must not contain relationships")
	}
	if resource.Links != nil {
		validator.add(path+"/links", "forbidden", "resource identifier must not contain links")
	}
}

func (validator *documentValidator) validateLinks(links Links, path string) {
	for name, link := range links {
		linkPath := path + "/" + escapePointerToken(name)
		if strings.HasPrefix(name, "@") {
			if !validMemberName(name) {
				validator.add(linkPath, "member-name", "@-Member name is invalid")
			}
			continue
		}
		if !validMemberName(name) && (!link.extension || !validExtensionMemberName(name)) {
			validator.add(linkPath, "member-name", "link name is invalid")
		}
		validator.validateLink(link, linkPath)
	}
}

func (validator *documentValidator) validateLinkScope(
	links Links,
	path string,
	allowed ...string,
) {
	for name, link := range links {
		if strings.HasPrefix(name, "@") || link.extension || slices.Contains(allowed, name) {
			continue
		}
		validator.add(
			path+"/"+escapePointerToken(name),
			"link-scope",
			"link member is not allowed in this links object",
		)
	}
}

func (validator *documentValidator) validatePaginationLinks(
	links Links,
	path string,
	collection bool,
) {
	if collection {
		return
	}
	for _, name := range []string{"first", "last", "prev", "next"} {
		if _, exists := links[name]; exists {
			validator.add(
				path+"/"+name,
				"pagination-shape",
				"pagination links require collection or to-many data",
			)
		}
	}
}

func (validator *documentValidator) validateLink(link Link, path string) {
	validator.validateLinkAt(link, path, 0, make(map[*Link]struct{}))
}

func (validator *documentValidator) validateLinkAt(
	link Link,
	path string,
	depth int,
	ancestors map[*Link]struct{},
) {
	if link.extension {
		return
	}
	if link.null {
		return
	}
	if link.object && !link.hrefPresent {
		validator.add(path+"/href", "required", "link object href is required")
	} else {
		validator.validateURL(link.href, path)
	}
	if (link.rel != "" || link.relPresent) && !validLinkRelation(link.rel) {
		validator.add(path+"/rel", "link-relation", "rel must be a registered relation or URI")
	}
	if link.describedBy != nil {
		describedByPath := path + "/describedby"
		if depth >= DefaultMaxNestingDepth {
			validator.add(
				describedByPath,
				"limit",
				"constructed link exceeds the nesting depth limit",
			)
		} else if _, cyclic := ancestors[link.describedBy]; cyclic {
			validator.add(describedByPath, "cycle", "constructed link contains a cycle")
		} else {
			ancestors[link.describedBy] = struct{}{}
			validator.validateLinkAt(*link.describedBy, describedByPath, depth+1, ancestors)
			delete(ancestors, link.describedBy)
		}
	}
	if link.targetType != "" || link.targetTypePresent {
		if mediaType, _, err := mime.ParseMediaType(link.targetType); err != nil || mediaType == "" {
			validator.add(path+"/type", "media-type", "type must be a valid media type")
		}
	}
	if link.hreflang != nil {
		for _, tag := range link.hreflang.values {
			if !validLanguageTag(tag) {
				validator.add(path+"/hreflang", "language-tag", "hreflang must contain valid language tags")
				break
			}
		}
	}
}

func validLanguageTag(tag string) bool {
	if tag == "" || strings.HasPrefix(tag, "-") || strings.HasSuffix(tag, "-") || strings.Contains(tag, "--") {
		return false
	}
	for _, character := range tag {
		if character != '-' &&
			(character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	_, err := language.Parse(tag)

	return err == nil
}

func validLinkRelation(relation string) bool {
	if registeredLinkRelation.MatchString(relation) {
		return true
	}
	absolute, valid := parseURIReference(relation)

	return valid && absolute
}

func (validator *documentValidator) validateURL(value, path string) {
	if _, valid := parseURIReference(value); !valid {
		validator.add(path, "uri-reference", "link must contain a valid URI-reference")
	}
}

func parseURIReference(value string) (bool, bool) {
	remainder := value
	if index := strings.IndexByte(remainder, '#'); index >= 0 {
		if !validURIComponent(remainder[index+1:], "/?:@") {
			return false, false
		}
		remainder = remainder[:index]
	}
	if index := strings.IndexByte(remainder, '?'); index >= 0 {
		if !validURIComponent(remainder[index+1:], "/?:@") {
			return false, false
		}
		remainder = remainder[:index]
	}
	absolute := false
	colon := strings.IndexByte(remainder, ':')
	slash := strings.IndexByte(remainder, '/')
	if colon >= 0 && (slash < 0 || colon < slash) {
		if !validURIScheme(remainder[:colon]) {
			return false, false
		}
		absolute = true
		remainder = remainder[colon+1:]
	}
	if strings.HasPrefix(remainder, "//") {
		remainder = remainder[2:]
		index := strings.IndexByte(remainder, '/')
		authority := remainder
		if index >= 0 {
			authority = remainder[:index]
			remainder = remainder[index:]
		} else {
			remainder = ""
		}
		if !validURIAuthority(authority) {
			return false, false
		}
	}
	if !validURIComponent(remainder, "/:@") {
		return false, false
	}

	return absolute, true
}

func validURIScheme(scheme string) bool {
	if scheme == "" || !isASCIIAlpha(scheme[0]) {
		return false
	}
	for index := 1; index < len(scheme); index++ {
		character := scheme[index]
		if !isASCIIAlpha(character) && (character < '0' || character > '9') &&
			character != '+' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func validURIAuthority(authority string) bool {
	hostPort := authority
	if index := strings.LastIndexByte(authority, '@'); index >= 0 {
		if !validURIComponent(authority[:index], ":") {
			return false
		}
		hostPort = authority[index+1:]
	}
	if strings.HasPrefix(hostPort, "[") {
		closing := strings.IndexByte(hostPort, ']')
		if closing < 0 || !validIPLiteral(hostPort[1:closing]) {
			return false
		}
		return validURIPort(hostPort[closing+1:])
	}
	if strings.Contains(hostPort, "[") || strings.Contains(hostPort, "]") {
		return false
	}
	if index := strings.LastIndexByte(hostPort, ':'); index >= 0 {
		if !validURIComponent(hostPort[:index], "") {
			return false
		}
		return validURIPort(hostPort[index:])
	}
	return validURIComponent(hostPort, "")
}

func validIPLiteral(value string) bool {
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Is6()
	}
	if len(value) < 4 || value[0] != 'v' && value[0] != 'V' {
		return false
	}
	dot := strings.IndexByte(value, '.')
	if dot < 2 {
		return false
	}
	for index := 1; index < dot; index++ {
		if !isHex(value[index]) {
			return false
		}
	}
	return validURIComponent(value[dot+1:], ":") && dot+1 < len(value)
}

func validURIPort(value string) bool {
	if value == "" {
		return true
	}
	if value[0] != ':' {
		return false
	}
	for _, character := range value[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validURIComponent(value, extra string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '%' {
			if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
				return false
			}
			index += 2
			continue
		}
		if isURIUnreserved(character) || strings.ContainsRune("!$&'()*+,;="+extra, rune(character)) {
			continue
		}
		return false
	}
	return true
}

func isURIUnreserved(character byte) bool {
	return isASCIIAlpha(character) ||
		character >= '0' && character <= '9' || strings.ContainsRune("-._~", rune(character))
}

func isASCIIAlpha(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isHex(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f' ||
		character >= 'A' && character <= 'F'
}

func (validator *documentValidator) validateError(apiError ErrorObject, path string) {
	if apiError.ID == "" && apiError.present == 0 && apiError.Links == nil && apiError.Status == "" &&
		apiError.Code == "" && apiError.Title == "" && apiError.Detail == "" &&
		apiError.Source == nil && apiError.Meta == nil &&
		!hasNonAtMember(apiError.AdditionalMembers) {
		validator.add(path, "required", "error object must contain at least one member")
	}
	if (apiError.Status != "" || apiError.present&errorStatusPresent != 0) &&
		!validHTTPStatus(apiError.Status) {
		validator.add(
			path+"/status",
			"http-status",
			"error status must be an HTTP status code from 100 through 599",
		)
	}
	validator.validateLinks(apiError.Links, path+"/links")
	validator.validateLinkScope(apiError.Links, path+"/links", "about", "type")
	if apiError.Source != nil && !validJSONPointer(apiError.Source.Pointer) {
		validator.add(path+"/source/pointer", "json-pointer", "source pointer must be a JSON Pointer")
	}
}

func validHTTPStatus(status string) bool {
	return len(status) == 3 && status[0] >= '1' && status[0] <= '5' &&
		status[1] >= '0' && status[1] <= '9' && status[2] >= '0' && status[2] <= '9'
}

func (validator *documentValidator) validateIncluded(
	document Document,
	identity identityRequirement,
) {
	if document.Included == nil {
		return
	}

	included := make(map[string][]int, len(document.Included)*2)
	for index, resource := range document.Included {
		path := "/included/" + strconv.Itoa(index)
		validator.validateResource(resource, path, identity, false, identity)
		for _, key := range resourceObjectKeys(resource) {
			included[key] = append(included[key], index)
		}
	}

	reachable := make(map[int]bool, len(document.Included))
	queue := validator.primaryLinkage(document.Data)
	for len(queue) > 0 {
		identifier := queue[0]
		queue = queue[1:]
		key := identifierKey(identifier)
		for _, index := range included[key] {
			if reachable[index] {
				continue
			}
			reachable[index] = true
			queue = append(queue, relationshipIdentifiers(document.Included[index].Relationships)...)
		}
	}

	for index := range document.Included {
		if !reachable[index] && !validator.options.SparseFieldsetsOmittedLinkage {
			validator.add(
				"/included/"+strconv.Itoa(index),
				"full-linkage",
				"included resource is not linked from primary data",
			)
		}
	}
}

type resourceObservation struct {
	resource ResourceObject
	path     string
}

type identityObservation struct {
	resourceType string
	id           string
	lid          string
	idPresent    bool
	lidPresent   bool
	path         string
}

func (validator *documentValidator) validateDocumentIdentity(document Document) {
	resources := documentResources(document)
	canonical := make(map[string]string, len(resources))
	var identities []identityObservation
	for _, observation := range resources {
		resource := observation.resource
		keys := resourceObjectKeys(resource)
		if len(keys) > 0 {
			previous := ""
			for _, key := range keys {
				if existing, exists := canonical[key]; exists {
					previous = existing
					break
				}
			}
			if previous != "" {
				validator.add(
					observation.path,
					"duplicate-resource",
					"resource duplicates canonical object at "+previous,
				)
			}
			for _, key := range keys {
				if _, exists := canonical[key]; !exists {
					canonical[key] = observation.path
				}
			}
		}
		identities = append(identities, identityObservation{
			resourceType: resource.Type,
			id:           resource.ID,
			lid:          resource.LID,
			idPresent:    resource.hasID(),
			lidPresent:   resource.hasLID(),
			path:         observation.path,
		})
		identities = append(identities, relationshipIdentityObservations(
			resource.Relationships,
			observation.path+"/relationships",
		)...)
	}
	validator.validateLocalIdentities(identities)
}

func documentResources(document Document) []resourceObservation {
	var resources []resourceObservation
	if document.Data != nil {
		switch document.Data.kind {
		case primaryDataNull:
		case primaryDataOne:
			if document.Data.one != nil {
				resources = append(resources, resourceObservation{*document.Data.one, "/data"})
			}
		case primaryDataMany:
			for index, resource := range document.Data.many {
				resources = append(resources, resourceObservation{
					resource: resource,
					path:     "/data/" + strconv.Itoa(index),
				})
			}
		}
	}
	for index, resource := range document.Included {
		resources = append(resources, resourceObservation{
			resource: resource,
			path:     "/included/" + strconv.Itoa(index),
		})
	}

	return resources
}

func relationshipIdentityObservations(
	relationships Relationships,
	path string,
) []identityObservation {
	names := make([]string, 0, len(relationships))
	for name := range relationships {
		names = append(names, name)
	}
	sort.Strings(names)
	var observations []identityObservation
	for _, name := range names {
		if strings.HasPrefix(name, "@") {
			continue
		}
		data := relationships[name].Data
		if data == nil || data.kind == relationshipDataNull {
			continue
		}
		dataPath := path + "/" + escapePointerToken(name) + "/data"
		if data.kind == relationshipDataOne && data.one != nil {
			observations = append(observations, identityFromIdentifier(*data.one, dataPath))
		}
		if data.kind == relationshipDataMany {
			for index, identifier := range data.many {
				observations = append(
					observations,
					identityFromIdentifier(identifier, dataPath+"/"+strconv.Itoa(index)),
				)
			}
		}
	}

	return observations
}

func identityFromIdentifier(identifier Identifier, path string) identityObservation {
	return identityObservation{
		resourceType: identifier.Type,
		id:           identifier.ID,
		lid:          identifier.LID,
		idPresent:    identifier.hasID(),
		lidPresent:   identifier.hasLID(),
		path:         path,
	}
}

func (validator *documentValidator) validateLocalIdentities(observations []identityObservation) {
	byID := make(map[string]identityObservation)
	byLID := make(map[string]identityObservation)
	for _, observation := range observations {
		if observation.resourceType == "" || !observation.lidPresent {
			continue
		}
		if observation.idPresent {
			idKey := observation.resourceType + "\x00" + observation.id
			if previous, exists := byID[idKey]; exists && previous.lid != observation.lid {
				validator.add(
					observation.path+"/lid",
					"local-identity",
					"lid differs from another representation of this resource",
				)
			} else {
				byID[idKey] = observation
			}
			lidKey := observation.resourceType + "\x00" + observation.lid
			if previous, exists := byLID[lidKey]; exists && previous.id != observation.id {
				validator.add(
					observation.path+"/lid",
					"local-identity",
					"lid identifies a different resource id elsewhere in the document",
				)
			} else {
				byLID[lidKey] = observation
			}
		}
	}
}

func (validator *documentValidator) primaryLinkage(data *PrimaryData) []Identifier {
	if data == nil || data.kind == primaryDataNull {
		return nil
	}
	if data.kind == primaryDataOne && data.one != nil {
		return relationshipIdentifiers(data.one.Relationships)
	}

	var identifiers []Identifier
	for _, resource := range data.many {
		identifiers = append(identifiers, relationshipIdentifiers(resource.Relationships)...)
	}

	return identifiers
}

func relationshipIdentifiers(relationships Relationships) []Identifier {
	var identifiers []Identifier
	for name, relationship := range relationships {
		if strings.HasPrefix(name, "@") {
			continue
		}
		data := relationship.Data
		if data == nil || data.kind == relationshipDataNull {
			continue
		}
		if data.kind == relationshipDataOne && data.one != nil {
			identifiers = append(identifiers, *data.one)
		}
		if data.kind == relationshipDataMany {
			identifiers = append(identifiers, data.many...)
		}
	}

	return identifiers
}

func resourceKey(resourceType, id, lid string, idPresent bool) string {
	if idPresent {
		return resourceType + "\x00id\x00" + id
	}

	return resourceType + "\x00lid\x00" + lid
}

func resourceObjectKeys(resource ResourceObject) []string {
	if resource.Type == "" {
		return nil
	}
	keys := make([]string, 0, 2)
	if resource.hasID() {
		keys = append(keys, resourceKey(resource.Type, resource.ID, "", true))
	}
	if resource.hasLID() {
		keys = append(keys, resourceKey(resource.Type, "", resource.LID, false))
	}
	return keys
}

func identifierKey(identifier Identifier) string {
	return resourceKey(identifier.Type, identifier.ID, identifier.LID, identifier.hasID())
}

func validMemberName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	if strings.HasPrefix(name, "@") {
		name = strings.TrimPrefix(name, "@")
		if name == "" {
			return false
		}
	}

	runes := []rune(name)
	if !globallyAllowed(runes[0]) || !globallyAllowed(runes[len(runes)-1]) {
		return false
	}
	if len(runes) > 2 {
		for _, character := range runes[1 : len(runes)-1] {
			if !globallyAllowed(character) && character != '-' && character != '_' && character != ' ' {
				return false
			}
		}
	}

	return true
}

func globallyAllowed(character rune) bool {
	return character > unicode.MaxASCII ||
		character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}

func validJSONPointer(pointer string) bool {
	if pointer == "" {
		return true
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	for index := 0; index < len(pointer); index++ {
		if pointer[index] != '~' {
			continue
		}
		if index+1 >= len(pointer) || pointer[index+1] != '0' && pointer[index+1] != '1' {
			return false
		}
		index++
	}

	return true
}

func escapePointerToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")

	return strings.ReplaceAll(token, "/", "~1")
}

func (validator *documentValidator) add(path, code, message string) {
	validator.violations = append(validator.violations, Violation{
		Path:    path,
		Code:    code,
		Message: message,
	})
}
