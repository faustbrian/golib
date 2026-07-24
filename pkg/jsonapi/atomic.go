package jsonapi

import "strconv"

// AtomicExtensionURI identifies the official Atomic Operations extension.
const AtomicExtensionURI = "https://jsonapi.org/ext/atomic"

// AtomicOperationCode identifies an Atomic Operations mutation.
type AtomicOperationCode string

const (
	// AtomicAdd creates a resource or adds relationship members.
	AtomicAdd AtomicOperationCode = "add"
	// AtomicUpdate updates a resource or replaces relationship linkage.
	AtomicUpdate AtomicOperationCode = "update"
	// AtomicRemove removes a resource or relationship members.
	AtomicRemove AtomicOperationCode = "remove"
)

// AtomicValidationContext identifies an Atomic Operations protocol boundary.
type AtomicValidationContext uint8

const (
	// AtomicGenericContext applies context-independent Atomic document rules.
	AtomicGenericContext AtomicValidationContext = iota
	// AtomicRequestContext requires an operations request document.
	AtomicRequestContext
	// AtomicResponseContext requires a valid results or errors response.
	AtomicResponseContext
)

// AtomicValidationOptions configures request or response validation.
type AtomicValidationOptions struct {
	Context             AtomicValidationContext
	ExpectedResultCount int
	ExpectedOperations  []AtomicOperation
}

// AtomicDocument is a document using the official Atomic Operations
// extension. Operations, results, errors, and meta retain nil-versus-empty
// presence semantics.
type AtomicDocument struct {
	JSONAPI    *JSONAPI
	Links      Links
	Operations []AtomicOperation
	Results    []AtomicResult
	Errors     []ErrorObject
	Meta       Meta
}

// AtomicOperation describes one ordered mutation in an atomic request.
type AtomicOperation struct {
	Op          AtomicOperationCode
	Ref         *AtomicReference
	Href        string
	Data        *PrimaryData
	Meta        Meta
	hrefPresent bool
}

// WithHref returns a copy whose href member is present, including when value
// is the empty URI-reference.
func (operation AtomicOperation) WithHref(value string) AtomicOperation {
	operation.Href = value
	operation.hrefPresent = true
	return operation
}

func (operation AtomicOperation) hasHref() bool {
	return operation.Href != "" || operation.hrefPresent
}

// AtomicReference identifies a resource or one of its relationships.
type AtomicReference struct {
	Type         string `json:"type"`
	ID           string `json:"id,omitempty"`
	LID          string `json:"lid,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	idPresent    bool
	lidPresent   bool
	relPresent   bool
}

// WithID returns a copy whose id member is present, including when value is
// the empty string.
func (reference AtomicReference) WithID(value string) AtomicReference {
	reference.ID = value
	reference.idPresent = true
	return reference
}

// WithLID returns a copy whose lid member is present, including when value is
// the empty string.
func (reference AtomicReference) WithLID(value string) AtomicReference {
	reference.LID = value
	reference.lidPresent = true
	return reference
}

// WithRelationship returns a copy whose relationship member is present.
func (reference AtomicReference) WithRelationship(value string) AtomicReference {
	reference.Relationship = value
	reference.relPresent = true
	return reference
}

func (reference AtomicReference) hasID() bool {
	return reference.ID != "" || reference.idPresent
}

func (reference AtomicReference) hasLID() bool {
	return reference.LID != "" || reference.lidPresent
}

func (reference AtomicReference) hasRelationship() bool {
	return reference.Relationship != "" || reference.relPresent
}

// AtomicResult describes the result at the same position as its operation.
type AtomicResult struct {
	Data *PrimaryData
	Meta Meta
}

// Validate checks Atomic Operations document and operation invariants.
func (document AtomicDocument) Validate() error {
	return document.ValidateWith(AtomicValidationOptions{})
}

// ValidateWith checks an Atomic document in a request or response context.
func (document AtomicDocument) ValidateWith(options AtomicValidationOptions) error {
	validator := documentValidator{}

	if options.ExpectedResultCount < 0 {
		validator.add(
			"/atomic:results",
			"result-count",
			"expected result count must not be negative",
		)
	}
	if options.ExpectedOperations != nil && options.ExpectedResultCount > 0 &&
		options.ExpectedResultCount != len(options.ExpectedOperations) {
		validator.add(
			"/atomic:results",
			"result-count",
			"expected result count must match expected operations",
		)
	}
	if document.Operations == nil && document.Results == nil &&
		document.Errors == nil && document.Meta == nil {
		validator.add("", "required", "atomic document must contain operations, results, errors, or meta")
	}
	if document.Operations != nil && len(document.Operations) == 0 {
		validator.add("/atomic:operations", "min-items", "operations must contain at least one item")
	}
	if document.Results != nil && len(document.Results) == 0 {
		validator.add("/atomic:results", "min-items", "results must contain at least one item")
	}
	if document.Operations != nil && document.Results != nil {
		validator.add("/atomic:results", "conflict", "operations and results must not coexist")
	}
	if (document.Operations != nil || document.Results != nil) && document.Errors != nil {
		validator.add("/errors", "conflict", "atomic members and errors must not coexist")
	}
	switch options.Context {
	case AtomicGenericContext:
	case AtomicRequestContext:
		if document.Operations == nil {
			validator.add("/atomic:operations", "required", "atomic request requires operations")
		}
		if document.Results != nil {
			validator.add("/atomic:results", "forbidden", "atomic request must not contain results")
		}
		if document.Errors != nil {
			validator.add("/errors", "forbidden", "atomic request must not contain errors")
		}
	case AtomicResponseContext:
		if document.Operations != nil {
			validator.add("/atomic:operations", "forbidden", "atomic response must not contain operations")
		}
		expectedResults := options.ExpectedResultCount
		if options.ExpectedOperations != nil {
			expectedResults = len(options.ExpectedOperations)
		}
		if document.Results == nil && document.Errors == nil && expectedResults > 0 {
			validator.add("/atomic:results", "required", "successful atomic response requires results")
		}
		if document.Results != nil &&
			(options.ExpectedOperations != nil || options.ExpectedResultCount > 0) &&
			len(document.Results) != expectedResults {
			validator.add(
				"/atomic:results",
				"result-count",
				"result count must match the request operation count",
			)
		}
	default:
		validator.add("", "validation-context", "atomic validation context is invalid")
	}
	if document.JSONAPI != nil {
		validator.validateJSONAPI(*document.JSONAPI)
		validator.validateAppliedURIs(
			document.JSONAPI.Ext,
			[]string{AtomicExtensionURI},
			"/jsonapi/ext",
			"extension",
			false,
		)
	}
	validator.validateLinks(document.Links, "/links")
	validator.validateLinkScope(
		document.Links,
		"/links",
		"self", "related", "describedby", "first", "last", "prev", "next",
	)
	validator.validatePaginationLinks(document.Links, "/links", false)
	validator.validateAtomicOperations(document.Operations)
	for index, result := range document.Results {
		path := "/atomic:results/" + strconv.Itoa(index) + "/data"
		if index < len(options.ExpectedOperations) {
			validator.validateAtomicResult(result, options.ExpectedOperations[index], path)
		} else if result.Data != nil {
			validator.validateAtomicResourceData(result.Data, path, identityID)
		}
	}
	for index, apiError := range document.Errors {
		validator.validateError(apiError, "/errors/"+strconv.Itoa(index))
	}

	if len(validator.violations) == 0 {
		return nil
	}

	return &ValidationError{Violations: validator.violations}
}

func (validator *documentValidator) validateAtomicResult(
	result AtomicResult,
	operation AtomicOperation,
	path string,
) {
	if result.Data == nil {
		if operation.Op == AtomicAdd && !atomicRelationshipOperation(operation) &&
			operation.Data != nil && operation.Data.kind == primaryDataOne &&
			operation.Data.one != nil && !operation.Data.one.hasID() {
			validator.add(path, "required", "server-generated resource create result requires data")
		}
		return
	}
	if operation.Op == AtomicRemove || atomicRelationshipOperation(operation) {
		validator.add(path, "forbidden", "operation result must not contain data")
		return
	}
	validator.validateAtomicResourceData(result.Data, path, identityID)
}

func atomicRelationshipOperation(operation AtomicOperation) bool {
	if operation.Ref != nil {
		return operation.Ref.hasRelationship()
	}
	return operation.Data != nil &&
		(operation.Op == AtomicAdd && operation.Data.kind == primaryDataMany ||
			operation.Op == AtomicUpdate && operation.Data.kind != primaryDataOne)
}

func (validator *documentValidator) validateAtomicOperations(operations []AtomicOperation) {
	assigned := make(map[string]struct{})
	for index, operation := range operations {
		path := "/atomic:operations/" + strconv.Itoa(index)
		validator.validateAtomicOperation(operation, path)
		if operation.Ref != nil && operation.Ref.hasLID() {
			key := resourceKey(operation.Ref.Type, "", operation.Ref.LID, false)
			if _, exists := assigned[key]; !exists {
				validator.add(
					path+"/ref/lid",
					"unresolved-lid",
					"reference lid must be assigned by a prior operation",
				)
			}
		}
		if operation.Op == AtomicAdd && operation.Data != nil &&
			operation.Data.kind == primaryDataOne && operation.Data.one != nil {
			resource := operation.Data.one
			if resource.Type != "" && resource.hasLID() {
				assigned[resourceKey(resource.Type, "", resource.LID, false)] = struct{}{}
			}
		}
		validator.validateAtomicDataLIDs(operation, path, assigned)
	}
}

func (validator *documentValidator) validateAtomicDataLIDs(
	operation AtomicOperation,
	path string,
	assigned map[string]struct{},
) {
	if operation.Data == nil {
		return
	}
	if operation.Data.kind == primaryDataMany {
		for index, resource := range operation.Data.many {
			validator.validateAtomicAssignedLID(
				resource.Type,
				resource.hasID(),
				resource.hasLID(),
				resource.LID,
				path+"/data/"+strconv.Itoa(index)+"/lid",
				assigned,
			)
		}
		return
	}
	if operation.Data.kind != primaryDataOne || operation.Data.one == nil {
		return
	}
	resource := *operation.Data.one
	if operation.Op != AtomicAdd {
		validator.validateAtomicAssignedLID(
			resource.Type,
			resource.hasID(),
			resource.hasLID(),
			resource.LID,
			path+"/data/lid",
			assigned,
		)
	}
	for _, observation := range relationshipIdentityObservations(
		resource.Relationships,
		path+"/data/relationships",
	) {
		validator.validateAtomicAssignedLID(
			observation.resourceType,
			observation.idPresent,
			observation.lidPresent,
			observation.lid,
			observation.path+"/lid",
			assigned,
		)
	}
}

func (validator *documentValidator) validateAtomicAssignedLID(
	resourceType string,
	idPresent bool,
	lidPresent bool,
	lid string,
	path string,
	assigned map[string]struct{},
) {
	if idPresent || !lidPresent {
		return
	}
	if _, exists := assigned[resourceKey(resourceType, "", lid, false)]; !exists {
		validator.add(path, "unresolved-lid", "lid must be assigned by a prior add operation")
	}
}

func (validator *documentValidator) validateAtomicOperation(
	operation AtomicOperation,
	path string,
) {
	if operation.Op == "" {
		validator.add(path+"/op", "required", "operation code is required")
	} else if operation.Op != AtomicAdd && operation.Op != AtomicUpdate &&
		operation.Op != AtomicRemove {
		validator.add(path+"/op", "value", "operation code must be add, update, or remove")
	}
	if operation.Ref != nil && operation.hasHref() {
		validator.add(path+"/href", "conflict", "ref and href must not coexist")
	}
	if operation.Ref != nil {
		validator.validateAtomicReference(*operation.Ref, path+"/ref")
	}
	if operation.hasHref() {
		validator.validateURL(operation.Href, path+"/href")
	}

	relationshipTarget := operation.Ref != nil && operation.Ref.hasRelationship()
	switch operation.Op {
	case AtomicAdd:
		if operation.Data == nil {
			validator.add(path+"/data", "required", "add operation requires data")
		} else if relationshipTarget {
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", true)
		} else if operation.Ref == nil && operation.Data.kind == primaryDataMany {
			validator.requireAtomicTarget(operation, path)
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", true)
		} else {
			if operation.Ref != nil {
				validator.add(path+"/ref", "forbidden", "resource creation must not use ref")
			}
			validator.validateAtomicResourceData(operation.Data, path+"/data", identityOptional)
		}
	case AtomicUpdate:
		if operation.Data == nil {
			validator.add(path+"/data", "required", "update operation requires data")
		} else if relationshipTarget {
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", false)
		} else if operation.Ref == nil && operation.Data.kind != primaryDataOne {
			validator.requireAtomicTarget(operation, path)
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", false)
		} else {
			validator.validateAtomicResourceData(operation.Data, path+"/data", identityEither)
			if operation.Ref != nil && operation.Data.kind == primaryDataOne &&
				operation.Data.one != nil {
				validator.validateAtomicResourceTarget(
					*operation.Ref,
					*operation.Data.one,
					path+"/data",
				)
			}
		}
	case AtomicRemove:
		validator.requireAtomicTarget(operation, path)
		if relationshipTarget && operation.Data == nil {
			validator.add(path+"/data", "required", "relationship removal requires data")
		} else if relationshipTarget {
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", true)
		} else if operation.Data != nil && operation.Ref != nil {
			validator.add(path+"/data", "forbidden", "resource removal must not contain data")
		} else if operation.Data != nil {
			validator.validateAtomicRelationshipData(operation.Data, path+"/data", true)
		}
	}
}

func (validator *documentValidator) validateAtomicResourceTarget(
	reference AtomicReference,
	resource ResourceObject,
	path string,
) {
	if reference.Type != "" && resource.Type != "" && reference.Type != resource.Type {
		validator.add(path+"/type", "target-mismatch", "resource type does not match ref target")
	}
	if reference.hasID() && resource.hasID() && reference.ID != resource.ID {
		validator.add(path+"/id", "target-mismatch", "resource id does not match ref target")
	}
	if reference.hasLID() && resource.hasLID() && reference.LID != resource.LID {
		validator.add(path+"/lid", "target-mismatch", "resource lid does not match ref target")
	}
}

func (validator *documentValidator) requireAtomicTarget(
	operation AtomicOperation,
	path string,
) {
	if operation.Ref == nil && !operation.hasHref() {
		validator.add(path+"/ref", "required", "operation requires ref or href")
	}
}

func (validator *documentValidator) validateAtomicReference(
	reference AtomicReference,
	path string,
) {
	if reference.Type == "" {
		validator.add(path+"/type", "required", "reference type is required")
	} else if !validImplementationMemberName(reference.Type) {
		validator.add(path+"/type", "member-name", "reference type must be a valid member name")
	}
	if !reference.hasID() && !reference.hasLID() {
		validator.add(path+"/id", "required", "reference requires id or lid")
	}
	if reference.hasID() && reference.hasLID() {
		validator.add(path+"/lid", "conflict", "reference id and lid must not coexist")
	}
	if reference.hasRelationship() && !validImplementationMemberName(reference.Relationship) {
		validator.add(
			path+"/relationship",
			"member-name",
			"relationship must be a valid member name",
		)
	}
}

func (validator *documentValidator) validateAtomicResourceData(
	data *PrimaryData,
	path string,
	identity identityRequirement,
) {
	if data.kind != primaryDataOne || data.one == nil {
		validator.add(path, "shape", "resource operation data must be one resource object")
		return
	}
	validator.validateResource(*data.one, path, identity, true, identityEither)
}

func (validator *documentValidator) validateAtomicRelationshipData(
	data *PrimaryData,
	path string,
	requireMany bool,
) {
	if requireMany && data.kind != primaryDataMany {
		validator.add(path, "shape", "add and remove relationship data must be an array")
		return
	}
	if data.kind == primaryDataNull {
		return
	}
	if data.kind == primaryDataOne && data.one != nil {
		validator.validateAtomicIdentifier(*data.one, path)
		return
	}
	if data.kind == primaryDataMany {
		for index, resource := range data.many {
			validator.validateAtomicIdentifier(resource, path+"/"+strconv.Itoa(index))
		}
		return
	}
	validator.add(path, "shape", "relationship data must be null, an identifier, or an array")
}

func (validator *documentValidator) validateAtomicIdentifier(
	resource ResourceObject,
	path string,
) {
	validator.validateIdentifier(Identifier{
		Type:       resource.Type,
		ID:         resource.ID,
		LID:        resource.LID,
		Meta:       resource.Meta,
		idPresent:  resource.idPresent,
		lidPresent: resource.lidPresent,
	}, path, identityEither)
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
