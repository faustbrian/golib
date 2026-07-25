package compose

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var (
	// ErrConflict reports two non-equivalent values at one merge destination.
	ErrConflict = errors.New("OpenAPI composition conflict")
	// ErrVersionMismatch reports documents with different exact revisions.
	ErrVersionMismatch = errors.New("OpenAPI composition version mismatch")
	// ErrInvalidComponentName reports a rename outside the OpenAPI name grammar.
	ErrInvalidComponentName = errors.New("invalid OpenAPI component name")
	// ErrRenameConflict reports a renamed component target that is already used.
	ErrRenameConflict = errors.New("OpenAPI component rename conflict")
)

// ConflictDecision selects the value retained for a merge collision.
type ConflictDecision uint8

const (
	// RejectConflict stops merging and returns a ConflictError.
	RejectConflict ConflictDecision = iota
	// KeepExisting retains the value already present in the result.
	KeepExisting
	// UseIncoming replaces the existing value with the incoming value.
	UseIncoming
	// RenameIncoming gives a colliding OpenAPI component a new local name and
	// rewrites internal references in its incoming document.
	RenameIncoming
)

// Conflict describes one immutable merge collision and its provenance.
type Conflict struct {
	pointer          string
	existingDocument int
	incomingDocument int
	existing         jsonvalue.Value
	incoming         jsonvalue.Value
}

// Pointer returns the escaped JSON Pointer in the merged document.
func (conflict Conflict) Pointer() string {
	return conflict.pointer
}

// ExistingDocumentIndex returns the source index of the retained value.
func (conflict Conflict) ExistingDocumentIndex() int {
	return conflict.existingDocument
}

// IncomingDocumentIndex returns the source index of the incoming value.
func (conflict Conflict) IncomingDocumentIndex() int {
	return conflict.incomingDocument
}

// Existing returns the immutable value already present at Pointer.
func (conflict Conflict) Existing() jsonvalue.Value {
	return conflict.existing
}

// Incoming returns the immutable incoming value at Pointer.
func (conflict Conflict) Incoming() jsonvalue.Value {
	return conflict.incoming
}

// ConflictError reports a rejected merge collision.
type ConflictError struct {
	conflict Conflict
}

// Error implements error without rendering document values.
func (failure *ConflictError) Error() string {
	return fmt.Sprintf(
		"%v at %s between documents %d and %d",
		ErrConflict,
		failure.conflict.pointer,
		failure.conflict.existingDocument,
		failure.conflict.incomingDocument,
	)
}

// Unwrap makes errors.Is recognize ErrConflict.
func (failure *ConflictError) Unwrap() error {
	return ErrConflict
}

// Pointer returns the escaped JSON Pointer of the collision.
func (failure *ConflictError) Pointer() string {
	return failure.conflict.Pointer()
}

// ExistingDocumentIndex returns the existing value's source index.
func (failure *ConflictError) ExistingDocumentIndex() int {
	return failure.conflict.ExistingDocumentIndex()
}

// IncomingDocumentIndex returns the incoming value's source index.
func (failure *ConflictError) IncomingDocumentIndex() int {
	return failure.conflict.IncomingDocumentIndex()
}

// Existing returns the immutable existing value.
func (failure *ConflictError) Existing() jsonvalue.Value {
	return failure.conflict.Existing()
}

// Incoming returns the immutable incoming value.
func (failure *ConflictError) Incoming() jsonvalue.Value {
	return failure.conflict.Incoming()
}

// ConflictResolver explicitly decides a non-equivalent merge collision.
type ConflictResolver func(Conflict) (ConflictDecision, error)

// ComponentRename describes a requested component rename.
type ComponentRename struct {
	conflict  Conflict
	registry  string
	original  string
	suggested string
}

// Conflict returns the component collision that requested renaming.
func (rename ComponentRename) Conflict() Conflict {
	return rename.conflict
}

// Registry returns the Components Object registry field.
func (rename ComponentRename) Registry() string {
	return rename.registry
}

// OriginalName returns the incoming component's source name.
func (rename ComponentRename) OriginalName() string {
	return rename.original
}

// SuggestedName returns the first deterministic available package name.
func (rename ComponentRename) SuggestedName() string {
	return rename.suggested
}

// ComponentRenamer selects a valid unused target component name.
type ComponentRenamer func(ComponentRename) (string, error)

// MergeOptions bounds merge work and controls collisions.
type MergeOptions struct {
	MaxDocuments          int
	MaxEntries            int
	MaxDepth              int
	MaxValueNodes         int
	MaxComponentNameBytes int
	ResolveConflict       ConflictResolver
	RenameComponent       ComponentRenamer
}

// DefaultMergeOptions returns conservative untrusted-document bounds.
// Collisions are rejected unless ResolveConflict is explicitly supplied.
func DefaultMergeOptions() MergeOptions {
	return MergeOptions{
		MaxDocuments:          1_000,
		MaxEntries:            100_000,
		MaxDepth:              256,
		MaxValueNodes:         1_000_000,
		MaxComponentNameBytes: 1_024,
	}
}

// Contribution records one incoming value considered by merge policy.
type Contribution struct {
	documentIndex int
	sourcePointer string
	targetPointer string
	replaced      bool
}

// DocumentIndex returns the incoming document's source index.
func (contribution Contribution) DocumentIndex() int {
	return contribution.documentIndex
}

// SourcePointer returns the escaped pointer in the incoming document.
func (contribution Contribution) SourcePointer() string {
	return contribution.sourcePointer
}

// TargetPointer returns the escaped pointer in the merged document.
func (contribution Contribution) TargetPointer() string {
	return contribution.targetPointer
}

// Replaced reports whether the incoming value replaced an existing value.
func (contribution Contribution) Replaced() bool {
	return contribution.replaced
}

// MergeResult owns the merged document and source-ordered provenance.
type MergeResult struct {
	document      openapi.Document
	contributions []Contribution
}

// Document returns the immutable merged document.
func (result MergeResult) Document() openapi.Document {
	return result.document
}

// Contributions returns caller-owned merge provenance.
func (result MergeResult) Contributions() []Contribution {
	return append([]Contribution(nil), result.contributions...)
}

// Merge combines exact-version documents without mutating its inputs.
// Named path and component registries are unioned in source order. All other
// non-equivalent collisions require an explicit resolver decision.
func Merge(
	ctx context.Context,
	documents []openapi.Document,
	options MergeOptions,
) (MergeResult, error) {
	if ctx == nil || len(documents) == 0 {
		return MergeResult{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	if options.MaxDocuments < 0 || options.MaxEntries < 0 ||
		options.MaxDepth < 0 || options.MaxValueNodes < 0 ||
		options.MaxComponentNameBytes < 0 {
		return MergeResult{}, ErrInvalidOptions
	}
	defaults := DefaultMergeOptions()
	if options.MaxDocuments == 0 {
		options.MaxDocuments = defaults.MaxDocuments
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = defaults.MaxEntries
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaults.MaxDepth
	}
	if options.MaxValueNodes == 0 {
		options.MaxValueNodes = defaults.MaxValueNodes
	}
	if options.MaxComponentNameBytes == 0 {
		options.MaxComponentNameBytes = defaults.MaxComponentNameBytes
	}
	if len(documents) > options.MaxDocuments {
		return MergeResult{}, ErrLimitExceeded
	}
	for _, document := range documents {
		if document == nil {
			return MergeResult{}, ErrInvalidInput
		}
	}

	version := documents[0].SpecificationVersion()
	for index := 1; index < len(documents); index++ {
		if documents[index].SpecificationVersion() != version {
			return MergeResult{}, fmt.Errorf(
				"%w: document 0 is %s and document %d is %s",
				ErrVersionMismatch,
				version,
				index,
				documents[index].SpecificationVersion(),
			)
		}
	}

	merger := documentMerger{
		ctx:            ctx,
		dialect:        version.Dialect(),
		options:        options,
		owners:         make(map[string]int),
		decisions:      make(map[mergeDecisionKey]ConflictDecision),
		sourcePointers: make(map[mergeDecisionKey]string),
	}
	root := documents[0].Raw()
	merger.owners[""] = 0
	for index := 1; index < len(documents); index++ {
		incoming, err := merger.prepareIncoming(root, documents[index].Raw(), index)
		if err != nil {
			return MergeResult{}, err
		}
		root, err = merger.mergeRoot(root, incoming, index)
		if err != nil {
			return MergeResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	// Exact version equality and value selection from already decoded inputs
	// preserve the root marker invariant required by Decode.
	merged, _ := openapi.Decode(root)
	return MergeResult{
		document:      merged,
		contributions: merger.contributions,
	}, nil
}

type documentMerger struct {
	ctx             context.Context
	dialect         specversion.Dialect
	options         MergeOptions
	entries         int
	preparedEntries int
	valueNodes      int
	owners          map[string]int
	decisions       map[mergeDecisionKey]ConflictDecision
	sourcePointers  map[mergeDecisionKey]string
	contributions   []Contribution
}

type mergeDecisionKey struct {
	documentIndex int
	pointer       string
}

func (merger *documentMerger) mergeRoot(
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	documentIndex int,
) (jsonvalue.Value, error) {
	return merger.mergeObject(existing, incoming, "", documentIndex, merger.rootMember)
}

func (merger *documentMerger) rootMember(
	name string,
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
) (jsonvalue.Value, error) {
	if name == "components" && merger.dialect != specversion.DialectSwagger20 {
		return merger.mergeObject(
			existing, incoming, pointer, documentIndex, merger.componentMember,
		)
	}
	if merger.rootRegistry(name) {
		return merger.mergeRegistry(existing, incoming, pointer, documentIndex)
	}
	return merger.resolve(existing, incoming, pointer, documentIndex)
}

func (merger *documentMerger) componentMember(
	name string,
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
) (jsonvalue.Value, error) {
	if componentRegistries[name] {
		return merger.mergeRegistry(existing, incoming, pointer, documentIndex)
	}
	return merger.resolve(existing, incoming, pointer, documentIndex)
}

func (merger *documentMerger) mergeRegistry(
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
) (jsonvalue.Value, error) {
	return merger.mergeObject(existing, incoming, pointer, documentIndex, nil)
}

type memberMerger func(
	name string,
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
) (jsonvalue.Value, error)

func (merger *documentMerger) mergeObject(
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
	nested memberMerger,
) (jsonvalue.Value, error) {
	existingMembers, existingObject := existing.Members()
	incomingMembers, incomingObject := incoming.Members()
	if !existingObject || !incomingObject {
		return merger.resolve(existing, incoming, pointer, documentIndex)
	}
	positions := make(map[string]int, len(existingMembers))
	for index, member := range existingMembers {
		positions[member.Name] = index
		memberPointer := pointer + "/" + escapePointer(member.Name)
		if _, known := merger.owners[memberPointer]; !known {
			merger.owners[memberPointer] = merger.owners[pointer]
		}
	}
	for _, incomingMember := range incomingMembers {
		if err := merger.ctx.Err(); err != nil {
			return jsonvalue.Value{}, err
		}
		memberPointer := pointer + "/" + escapePointer(incomingMember.Name)
		position, found := positions[incomingMember.Name]
		if !found {
			if err := merger.countEntry(); err != nil {
				return jsonvalue.Value{}, err
			}
			positions[incomingMember.Name] = len(existingMembers)
			existingMembers = append(existingMembers, incomingMember)
			merger.recordContribution(documentIndex, memberPointer, false)
			merger.owners[memberPointer] = documentIndex
			continue
		}
		current := existingMembers[position].Value
		equal, equalityErr := merger.semanticEqual(current, incomingMember.Value)
		if equalityErr != nil {
			return jsonvalue.Value{}, equalityErr
		}
		if equal {
			continue
		}
		if err := merger.countEntry(); err != nil {
			return jsonvalue.Value{}, err
		}
		var merged jsonvalue.Value
		var err error
		if nested == nil {
			merged, err = merger.resolve(
				current, incomingMember.Value, memberPointer, documentIndex,
			)
		} else {
			merged, err = nested(
				incomingMember.Name,
				current,
				incomingMember.Value,
				memberPointer,
				documentIndex,
			)
		}
		if err != nil {
			return jsonvalue.Value{}, err
		}
		existingMembers[position].Value = merged
	}
	// Members originate in valid immutable objects and positions prevents new
	// duplicate names, so construction cannot violate Object invariants.
	result, _ := jsonvalue.Object(existingMembers)
	return result, nil
}

func (merger *documentMerger) resolve(
	existing jsonvalue.Value,
	incoming jsonvalue.Value,
	pointer string,
	documentIndex int,
) (jsonvalue.Value, error) {
	conflict := Conflict{
		pointer:          pointer,
		existingDocument: merger.owners[pointer],
		incomingDocument: documentIndex,
		existing:         existing,
		incoming:         incoming,
	}
	decision := RejectConflict
	decisionKey := mergeDecisionKey{documentIndex: documentIndex, pointer: pointer}
	if prepared, ok := merger.decisions[decisionKey]; ok {
		decision = prepared
	} else if merger.options.ResolveConflict != nil {
		var err error
		decision, err = merger.options.ResolveConflict(conflict)
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	switch decision {
	case RejectConflict:
		return jsonvalue.Value{}, &ConflictError{conflict: conflict}
	case KeepExisting:
		merger.recordContribution(documentIndex, pointer, false)
		return existing, nil
	case UseIncoming:
		merger.recordContribution(documentIndex, pointer, true)
		merger.owners[pointer] = documentIndex
		return incoming, nil
	case RenameIncoming:
		return jsonvalue.Value{}, fmt.Errorf(
			"%w: rename is only valid for Components Object entries",
			ErrInvalidOptions,
		)
	default:
		return jsonvalue.Value{}, fmt.Errorf(
			"%w: conflict resolver returned decision %d",
			ErrInvalidOptions,
			decision,
		)
	}
}

func (merger *documentMerger) countEntry() error {
	merger.entries++
	if merger.entries > merger.options.MaxEntries {
		return ErrLimitExceeded
	}
	return nil
}

func (merger *documentMerger) recordContribution(
	documentIndex int,
	pointer string,
	replaced bool,
) {
	sourcePointer := pointer
	if original, ok := merger.sourcePointers[mergeDecisionKey{
		documentIndex: documentIndex,
		pointer:       pointer,
	}]; ok {
		sourcePointer = original
	}
	merger.contributions = append(merger.contributions, Contribution{
		documentIndex: documentIndex,
		sourcePointer: sourcePointer,
		targetPointer: pointer,
		replaced:      replaced,
	})
}

type referenceRename struct {
	oldPrefix string
	newPrefix string
}

var componentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func (merger *documentMerger) prepareIncoming(
	existingRoot jsonvalue.Value,
	incomingRoot jsonvalue.Value,
	documentIndex int,
) (jsonvalue.Value, error) {
	if merger.dialect == specversion.DialectSwagger20 ||
		merger.options.ResolveConflict == nil {
		return incomingRoot, nil
	}
	existingComponents, existingOK := existingRoot.Lookup("components")
	incomingComponents, incomingOK := incomingRoot.Lookup("components")
	if !existingOK || !incomingOK {
		return incomingRoot, nil
	}
	existingRegistries, existingOK := existingComponents.Members()
	incomingRegistries, incomingOK := incomingComponents.Members()
	if !existingOK || !incomingOK {
		return incomingRoot, nil
	}
	existingByRegistry := make(map[string]jsonvalue.Value, len(existingRegistries))
	for _, registry := range existingRegistries {
		existingByRegistry[registry.Name] = registry.Value
	}
	keyRenames := make(map[string]map[string]string)
	referenceRenames := make([]referenceRename, 0)
	for _, incomingRegistry := range incomingRegistries {
		if err := merger.countPreparedEntry(); err != nil {
			return jsonvalue.Value{}, err
		}
		if !componentRegistries[incomingRegistry.Name] {
			continue
		}
		existingRegistry, found := existingByRegistry[incomingRegistry.Name]
		if !found {
			continue
		}
		existingMembers, existingObject := existingRegistry.Members()
		incomingMembers, incomingObject := incomingRegistry.Value.Members()
		if !existingObject || !incomingObject {
			continue
		}
		existingByName := make(map[string]jsonvalue.Value, len(existingMembers))
		occupied := make(map[string]bool)
		for _, member := range existingMembers {
			existingByName[member.Name] = member.Value
			occupied[member.Name] = true
		}
		for _, member := range incomingMembers {
			occupied[member.Name] = true
		}
		for _, incomingMember := range incomingMembers {
			if err := merger.countPreparedEntry(); err != nil {
				return jsonvalue.Value{}, err
			}
			existingValue, collision := existingByName[incomingMember.Name]
			if !collision {
				continue
			}
			equal, err := merger.semanticEqual(existingValue, incomingMember.Value)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if equal {
				continue
			}
			pointer := "/components/" + escapePointer(incomingRegistry.Name) +
				"/" + escapePointer(incomingMember.Name)
			conflict := Conflict{
				pointer:          pointer,
				existingDocument: merger.ownerAt(pointer),
				incomingDocument: documentIndex,
				existing:         existingValue,
				incoming:         incomingMember.Value,
			}
			decision, err := merger.options.ResolveConflict(conflict)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if decision != RenameIncoming {
				if decision != RejectConflict && decision != KeepExisting &&
					decision != UseIncoming {
					return jsonvalue.Value{}, fmt.Errorf(
						"%w: conflict resolver returned decision %d",
						ErrInvalidOptions,
						decision,
					)
				}
				merger.decisions[mergeDecisionKey{
					documentIndex: documentIndex,
					pointer:       pointer,
				}] = decision
				continue
			}
			suggested := availableComponentName(
				incomingMember.Name, documentIndex+1, occupied,
			)
			target := suggested
			if merger.options.RenameComponent != nil {
				target, err = merger.options.RenameComponent(ComponentRename{
					conflict:  conflict,
					registry:  incomingRegistry.Name,
					original:  incomingMember.Name,
					suggested: suggested,
				})
				if err != nil {
					return jsonvalue.Value{}, err
				}
			}
			if len(target) > merger.options.MaxComponentNameBytes {
				return jsonvalue.Value{}, ErrLimitExceeded
			}
			if !componentNamePattern.MatchString(target) {
				return jsonvalue.Value{}, fmt.Errorf(
					"%w: %q", ErrInvalidComponentName, target,
				)
			}
			if occupied[target] {
				return jsonvalue.Value{}, fmt.Errorf(
					"%w: %q", ErrRenameConflict, target,
				)
			}
			occupied[target] = true
			registryPointer := "/components/" + escapePointer(incomingRegistry.Name)
			if keyRenames[registryPointer] == nil {
				keyRenames[registryPointer] = make(map[string]string)
			}
			keyRenames[registryPointer][incomingMember.Name] = target
			targetPointer := registryPointer + "/" + escapePointer(target)
			merger.sourcePointers[mergeDecisionKey{
				documentIndex: documentIndex,
				pointer:       targetPointer,
			}] = pointer
			referenceRenames = append(referenceRenames, referenceRename{
				oldPrefix: pointer,
				newPrefix: targetPointer,
			})
		}
	}
	if len(referenceRenames) == 0 {
		return incomingRoot, nil
	}
	return merger.rewriteIncoming(
		incomingRoot, "", 1, keyRenames, referenceRenames,
	)
}

func (merger *documentMerger) countPreparedEntry() error {
	merger.preparedEntries++
	if merger.preparedEntries > merger.options.MaxEntries {
		return ErrLimitExceeded
	}
	return nil
}

func (merger *documentMerger) rewriteIncoming(
	value jsonvalue.Value,
	pointer string,
	depth int,
	keyRenames map[string]map[string]string,
	referenceRenames []referenceRename,
) (jsonvalue.Value, error) {
	if err := merger.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	merger.valueNodes++
	if merger.valueNodes > merger.options.MaxValueNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	childCount, _ := value.Length()
	if !merger.childrenFit(childCount, 0, depth) {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	switch value.Kind() {
	case jsonvalue.ArrayKind:
		elements, _ := value.Elements()
		for index := range elements {
			transformed, err := merger.rewriteIncoming(
				elements[index], pointer, nextDepth(depth), keyRenames, referenceRenames,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			elements[index] = transformed
		}
		result, _ := jsonvalue.Array(elements)
		return result, nil
	case jsonvalue.ObjectKind:
		members, _ := value.Members()
		for index := range members {
			sourceName := members[index].Name
			if renamed, ok := keyRenames[pointer][sourceName]; ok {
				members[index].Name = renamed
			}
			memberPointer := pointer + "/" + escapePointer(members[index].Name)
			if sourceName == "$ref" {
				if referenceText, ok := members[index].Value.Text(); ok {
					for _, rename := range referenceRenames {
						if referenceText == "#"+rename.oldPrefix ||
							strings.HasPrefix(referenceText, "#"+rename.oldPrefix+"/") {
							referenceText = "#" + rename.newPrefix +
								referenceText[len("#"+rename.oldPrefix):]
							members[index].Value, _ = jsonvalue.String(referenceText)
							break
						}
					}
				}
			}
			transformed, err := merger.rewriteIncoming(
				members[index].Value,
				memberPointer,
				nextDepth(depth),
				keyRenames,
				referenceRenames,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = transformed
		}
		result, _ := jsonvalue.Object(members)
		return result, nil
	default:
		return value, nil
	}
}

func (merger *documentMerger) ownerAt(pointer string) int {
	for candidate := pointer; candidate != ""; {
		if owner, ok := merger.owners[candidate]; ok {
			return owner
		}
		separator := strings.LastIndex(candidate, "/")
		candidate = candidate[:separator]
	}
	return merger.owners[""]
}

func availableComponentName(original string, start int, occupied map[string]bool) string {
	for suffix := start; ; suffix++ {
		candidate := fmt.Sprintf("%s_%d", original, suffix)
		if !occupied[candidate] {
			return candidate
		}
	}
}

func (merger *documentMerger) rootRegistry(name string) bool {
	if name == "paths" {
		return true
	}
	if (merger.dialect == specversion.DialectOAS31 ||
		merger.dialect == specversion.DialectOAS32) && name == "webhooks" {
		return true
	}
	if merger.dialect != specversion.DialectSwagger20 {
		return false
	}
	switch name {
	case "definitions", "parameters", "responses", "securityDefinitions":
		return true
	default:
		return false
	}
}

var componentRegistries = map[string]bool{
	"schemas":         true,
	"responses":       true,
	"parameters":      true,
	"examples":        true,
	"requestBodies":   true,
	"headers":         true,
	"securitySchemes": true,
	"links":           true,
	"callbacks":       true,
	"pathItems":       true,
	"mediaTypes":      true,
}

type valueComparison struct {
	left  jsonvalue.Value
	right jsonvalue.Value
	depth int
}

func (merger *documentMerger) semanticEqual(
	left jsonvalue.Value,
	right jsonvalue.Value,
) (bool, error) {
	pending := []valueComparison{{left: left, right: right, depth: 1}}
	for len(pending) > 0 {
		if err := merger.ctx.Err(); err != nil {
			return false, err
		}
		comparison := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		merger.valueNodes++
		if merger.valueNodes > merger.options.MaxValueNodes {
			return false, ErrLimitExceeded
		}
		if comparison.left.Kind() != comparison.right.Kind() {
			return false, nil
		}
		switch comparison.left.Kind() {
		case jsonvalue.NullKind:
			continue
		case jsonvalue.BooleanKind:
			leftValue, _ := comparison.left.Bool()
			rightValue, _ := comparison.right.Bool()
			if leftValue != rightValue {
				return false, nil
			}
		case jsonvalue.NumberKind:
			leftValue, _ := comparison.left.NumberText()
			rightValue, _ := comparison.right.NumberText()
			if leftValue != rightValue {
				return false, nil
			}
		case jsonvalue.StringKind:
			leftValue, _ := comparison.left.Text()
			rightValue, _ := comparison.right.Text()
			if leftValue != rightValue {
				return false, nil
			}
		case jsonvalue.ArrayKind:
			leftCount, _ := comparison.left.Length()
			rightCount, _ := comparison.right.Length()
			if leftCount != rightCount {
				return false, nil
			}
			if !merger.childrenFit(leftCount, len(pending), comparison.depth) {
				return false, ErrLimitExceeded
			}
			leftValues, _ := comparison.left.Elements()
			rightValues, _ := comparison.right.Elements()
			for index := len(leftValues) - 1; index >= 0; index-- {
				pending = append(pending, valueComparison{
					left:  leftValues[index],
					right: rightValues[index],
					depth: nextDepth(comparison.depth),
				})
			}
		case jsonvalue.ObjectKind:
			leftCount, _ := comparison.left.Length()
			rightCount, _ := comparison.right.Length()
			if leftCount != rightCount {
				return false, nil
			}
			if !merger.childrenFit(leftCount, len(pending), comparison.depth) {
				return false, ErrLimitExceeded
			}
			leftMembers, _ := comparison.left.Members()
			rightMembers, _ := comparison.right.Members()
			rightByName := make(map[string]jsonvalue.Value, len(rightMembers))
			for _, member := range rightMembers {
				rightByName[member.Name] = member.Value
			}
			for index := len(leftMembers) - 1; index >= 0; index-- {
				member := leftMembers[index]
				other, ok := rightByName[member.Name]
				if !ok {
					return false, nil
				}
				pending = append(pending, valueComparison{
					left:  member.Value,
					right: other,
					depth: nextDepth(comparison.depth),
				})
			}
		default:
			return false, nil
		}
	}
	return true, nil
}

func (merger *documentMerger) childrenFit(
	childCount int,
	queued int,
	depth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= merger.options.MaxDepth {
		return false
	}
	remaining := merger.options.MaxValueNodes - merger.valueNodes
	return childCount <= remaining-queued
}
