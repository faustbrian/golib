package jsonschema

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OutputFormat selects one of the standard JSON Schema output forms.
type OutputFormat string

const (
	// OutputFlag emits only the overall validity flag.
	OutputFlag OutputFormat = "flag"
	// OutputBasic emits a flat list of errors or annotations.
	OutputBasic OutputFormat = "basic"
	// OutputDetailed emits location-aware nested validation results.
	OutputDetailed OutputFormat = "detailed"
	// OutputVerbose emits the complete location-aware validation result tree.
	OutputVerbose OutputFormat = "verbose"
)

// OutputUnit is one unit in a standard JSON Schema validation output.
type OutputUnit struct {
	Valid                   bool         `json:"valid"`
	KeywordLocation         string       `json:"keywordLocation"`
	AbsoluteKeywordLocation string       `json:"absoluteKeywordLocation,omitempty"`
	InstanceLocation        string       `json:"instanceLocation"`
	Error                   string       `json:"error,omitempty"`
	Errors                  []OutputUnit `json:"errors,omitempty"`
	Annotations             []OutputUnit `json:"annotations,omitempty"`
	Annotation              any          `json:"annotation,omitempty"`
	format                  OutputFormat
}

// MarshalJSON emits the compact standard representation selected for the
// root output unit.
func (unit OutputUnit) MarshalJSON() ([]byte, error) {
	if unit.format == OutputFlag {
		return json.Marshal(struct {
			Valid bool `json:"valid"`
		}{Valid: unit.Valid})
	}
	type outputUnit OutputUnit
	return json.Marshal(outputUnit(unit))
}

// CollectAnnotations validates raw JSON and returns the retained annotation
// results as a flat, deterministic list. Failed schema branches do not
// contribute annotations.
func (schema *Schema) CollectAnnotations(
	ctx context.Context,
	raw []byte,
) ([]OutputUnit, error) {
	if schema == nil || schema.plan == nil {
		return nil, fmt.Errorf("%w: nil compiled schema", ErrInvalidSchema)
	}
	instance, err := decodeJSON(ctx, raw, schema.limits)
	if err != nil {
		return nil, fmt.Errorf("parse instance: %w", err)
	}
	state := schemaEvaluationState(ctx, schema.plan, schema.limits)
	annotations, err := schema.plan.collectAnnotations(
		instance, schema.dialect, "", &state,
	)
	if err != nil {
		return nil, err
	}
	if err := state.consumeOutputUnits(countOutputUnits(annotations)); err != nil {
		return nil, err
	}
	return annotations, nil
}

// ValidateOutput validates raw JSON and returns the selected standard output.
func (schema *Schema) ValidateOutput(
	ctx context.Context,
	raw []byte,
	format OutputFormat,
) (OutputUnit, error) {
	if schema == nil || schema.plan == nil {
		return OutputUnit{}, fmt.Errorf("%w: nil compiled schema", ErrInvalidSchema)
	}
	switch format {
	case OutputFlag, OutputBasic, OutputDetailed, OutputVerbose:
	default:
		return OutputUnit{}, fmt.Errorf("%w: unknown output format %q", ErrInvalidSchema, format)
	}
	instance, err := decodeJSON(ctx, raw, schema.limits)
	if err != nil {
		return OutputUnit{}, fmt.Errorf("parse instance: %w", err)
	}
	state := schemaEvaluationState(ctx, schema.plan, schema.limits)
	valid, err := schema.plan.evaluate(instance, schema.dialect, &state)
	if err != nil {
		return OutputUnit{}, err
	}
	output := OutputUnit{
		Valid:            valid,
		KeywordLocation:  "",
		InstanceLocation: "",
		format:           format,
	}
	if format == OutputFlag {
		return output, nil
	}
	errors, annotations, err := schema.plan.collectOutput(
		instance,
		schema.dialect,
		"",
		"",
		false,
		true,
		&state,
	)
	if err != nil {
		return OutputUnit{}, err
	}
	verboseAnnotations := annotations
	if format == OutputVerbose {
		verboseAnnotations, err = schema.plan.collectAnnotations(
			instance,
			schema.dialect,
			"",
			&state,
		)
		if err != nil {
			return OutputUnit{}, err
		}
	}
	if valid {
		if format == OutputVerbose {
			output.Annotations, err = schema.plan.verboseOutputUnits(
				instance, errors, verboseAnnotations, "", "", false,
				schema.dialect, &state,
			)
			if err != nil {
				return OutputUnit{}, err
			}
			if err := consumeVerboseOutputUnits(
				&state, output.Annotations, errors, annotations,
			); err != nil {
				return OutputUnit{}, err
			}
		} else {
			output.Annotations = annotations
		}
	} else {
		switch format {
		case OutputBasic:
			output.Errors = errors
		case OutputVerbose:
			output.Errors, err = schema.plan.verboseOutputUnits(
				instance, errors, verboseAnnotations, "", "", false,
				schema.dialect, &state,
			)
			if err != nil {
				return OutputUnit{}, err
			}
			if err := consumeVerboseOutputUnits(
				&state, output.Errors, errors, annotations,
			); err != nil {
				return OutputUnit{}, err
			}
		default:
			output.Errors = detailedErrors(errors)
		}
		if len(output.Errors) == 0 {
			if err := state.consumeOutputUnits(1); err != nil {
				return OutputUnit{}, err
			}
			output.Errors = []OutputUnit{schema.plan.outputError("", "schema validation failed")}
		}
	}
	return output, nil
}

func standardOutputKeywords(object map[string]*jsonValue, dialect Dialect) []string {
	if dialect.referenceReplacesSiblings() {
		if _, exists := object["$ref"]; exists {
			return []string{"$ref"}
		}
	}
	keywords := make([]string, 0, len(object))
	for _, keyword := range sortedStringKeys(object) {
		switch keyword {
		case "$anchor", "$comment", "$dynamicAnchor", "$id", "$recursiveAnchor",
			"$schema", "$vocabulary", "id":
			continue
		default:
			keywords = append(keywords, keyword)
		}
	}
	return keywords
}

func (plan *schemaPlan) verboseOutputUnits(
	instance *jsonValue,
	errors []OutputUnit,
	annotations []OutputUnit,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	dialect Dialect,
	state *evaluationState,
) ([]OutputUnit, error) {
	units := make([]OutputUnit, 0, len(plan.outputKeywords))
	for _, keyword := range plan.outputKeywords {
		emit, err := plan.verboseKeywordIsEvaluated(keyword, instance, dialect, state)
		if err != nil {
			return nil, err
		}
		if !emit {
			continue
		}
		keywordLocation := joinEvaluationPath(evaluationPath, keyword)
		if keyword == plan.referenceKeyword && plan.reference != nil {
			unit, err := plan.verboseReferenceUnit(
				instance,
				dialect,
				keywordLocation,
				instanceLocation,
				state,
			)
			if err != nil {
				return nil, err
			}
			units = append(units, unit)
			continue
		}
		keywordErrors := outputUnitsWithin(errors, keywordLocation)
		keywordAnnotations := outputUnitsWithin(annotations, keywordLocation)
		if keyword == "$defs" || keyword == "definitions" {
			keywordAnnotations = nil
		}
		if annotation, exists := plan.annotations[keyword]; exists {
			if !plan.annotationApplies(keyword, instance) {
				continue
			}
			if len(keywordAnnotations) == 0 {
				keywordAnnotations = []OutputUnit{plan.keywordAnnotationAt(
					keyword,
					evaluationPath,
					instanceLocation,
					referenced,
					annotation,
				)}
			}
		}
		if len(keywordErrors) > 0 {
			unit := plan.verboseKeywordError(
				keyword,
				keywordLocation,
				instanceLocation,
				referenced,
				keywordErrors,
			)
			children, err := plan.verboseKeywordChildren(
				keyword, instance, dialect, keywordLocation,
				instanceLocation, referenced, state,
			)
			if err != nil {
				return nil, err
			}
			if children != nil {
				unit.Errors = children
				unit.Error = ""
			}
			units = append(units, unit)
			continue
		}
		if len(keywordAnnotations) == 1 &&
			keywordAnnotations[0].KeywordLocation == keywordLocation {
			units = append(units, keywordAnnotations[0])
			continue
		}
		unit := OutputUnit{
			Valid:            true,
			KeywordLocation:  keywordLocation,
			InstanceLocation: instanceLocation,
		}
		if referenced {
			unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(
				plan.keywordLocation(keyword),
			)
		}
		unit.Annotations = keywordAnnotations
		if verboseKeywordExpandsSuccessfulEvaluation(keyword) {
			children, err := plan.verboseKeywordChildren(
				keyword, instance, dialect, keywordLocation,
				instanceLocation, referenced, state,
			)
			if err != nil {
				return nil, err
			}
			if children != nil {
				unit.Annotations = children
			}
		}
		units = append(units, unit)
	}
	for _, annotation := range annotations {
		if outputUnitCoveredByKeywords(
			annotation, evaluationPath, plan.outputKeywords,
		) || outputContainsAnnotationAt(units, annotation) ||
			referenced && plan.ownsDirectAnnotation(annotation, instanceLocation) {
			continue
		}
		units = append(units, annotation)
	}
	return units, nil
}

func verboseKeywordExpandsSuccessfulEvaluation(keyword string) bool {
	switch keyword {
	case "allOf", "anyOf", "else", "if", "not", "oneOf", "then":
		return true
	default:
		return false
	}
}

func (plan *schemaPlan) verboseKeywordIsEvaluated(
	keyword string,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (bool, error) {
	if keyword != "then" && keyword != "else" {
		return true, nil
	}
	if plan.condition == nil {
		return false, nil
	}
	matched, err := plan.condition.evaluate(instance, dialect, state)
	if err != nil {
		return false, err
	}
	return keyword == "then" && matched || keyword == "else" && !matched, nil
}

func (plan *schemaPlan) ownsDirectAnnotation(
	annotation OutputUnit,
	instanceLocation string,
) bool {
	if annotation.InstanceLocation != instanceLocation {
		return false
	}
	separator := strings.LastIndex(annotation.KeywordLocation, "/")
	if separator == -1 {
		return false
	}
	_, exists := plan.annotations[annotation.KeywordLocation[separator+1:]]
	return exists
}

func outputContainsAnnotationAt(units []OutputUnit, annotation OutputUnit) bool {
	for _, unit := range units {
		sameLocation := unit.AbsoluteKeywordLocation != "" &&
			unit.AbsoluteKeywordLocation == annotation.AbsoluteKeywordLocation
		if unit.AbsoluteKeywordLocation == "" && annotation.AbsoluteKeywordLocation == "" {
			sameLocation = unit.KeywordLocation == annotation.KeywordLocation
		}
		sameKeyword := strings.HasSuffix(
			unit.KeywordLocation,
			unitKeywordSuffix(annotation.KeywordLocation),
		)
		if unit.Annotation != nil && unit.InstanceLocation == annotation.InstanceLocation &&
			(sameLocation || sameKeyword && reflect.DeepEqual(
				unit.Annotation, annotation.Annotation,
			)) {
			return true
		}
		if outputContainsAnnotationAt(unit.Annotations, annotation) ||
			outputContainsAnnotationAt(unit.Errors, annotation) {
			return true
		}
	}
	return false
}

func unitKeywordSuffix(keywordLocation string) string {
	separator := strings.LastIndex(keywordLocation, "/")
	if separator == -1 {
		return keywordLocation
	}
	return keywordLocation[separator:]
}

func (plan *schemaPlan) verboseKeywordChildren(
	keyword string,
	instance *jsonValue,
	dialect Dialect,
	keywordLocation string,
	instanceLocation string,
	referenced bool,
	state *evaluationState,
) ([]OutputUnit, error) {
	switch keyword {
	case "allOf":
		return verboseSchemaArray(
			plan.allOf, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "anyOf":
		return verboseSchemaArray(
			plan.anyOf, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "oneOf":
		return verboseSchemaArray(
			plan.oneOf, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "not":
		return verboseSingleSchema(
			plan.not, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "if":
		return verboseSingleSchema(
			plan.condition, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "then":
		return verboseSingleSchema(
			plan.then, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "else":
		return verboseSingleSchema(
			plan.otherwise, instance, dialect, keywordLocation,
			instanceLocation, referenced, state,
		)
	case "properties":
		if instance.kind != kindObject {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(plan.properties) {
			value, exists := instance.object[name]
			if !exists {
				continue
			}
			applied, err := plan.properties[name].verboseAppliedSchemaUnits(
				value,
				dialect,
				joinEvaluationPath(keywordLocation, name),
				instanceLocation+"/"+escapePointerToken(name),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "patternProperties":
		if instance.kind != kindObject {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(instance.object) {
			for _, pattern := range plan.patternProperties {
				matched, err := pattern.pattern.matchString(name)
				if err != nil {
					return nil, err
				}
				if !matched {
					continue
				}
				applied, err := pattern.schema.verboseAppliedSchemaUnits(
					instance.object[name],
					dialect,
					joinEvaluationPath(keywordLocation, pattern.name),
					instanceLocation+"/"+escapePointerToken(name),
					referenced,
					state,
				)
				if err != nil {
					return nil, err
				}
				children = append(children, applied...)
			}
		}
		return children, nil
	case "additionalProperties":
		if plan.additionalProperties == nil || instance.kind != kindObject {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(instance.object) {
			configured, err := plan.propertyIsConfigured(name)
			if err != nil {
				return nil, err
			}
			if configured {
				continue
			}
			applied, err := plan.additionalProperties.verboseAppliedSchemaUnits(
				instance.object[name],
				dialect,
				keywordLocation,
				instanceLocation+"/"+escapePointerToken(name),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "propertyNames":
		if plan.propertyNames == nil || instance.kind != kindObject {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(instance.object) {
			applied, err := plan.propertyNames.verboseAppliedSchemaUnits(
				&jsonValue{kind: kindString, text: name},
				dialect,
				keywordLocation,
				instanceLocation+"/"+escapePointerToken(name),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "dependentSchemas", "dependencies":
		if instance.kind != kindObject {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(plan.dependentSchemas) {
			if _, exists := instance.object[name]; !exists {
				continue
			}
			applied, err := plan.dependentSchemas[name].verboseAppliedSchemaUnits(
				instance,
				dialect,
				joinEvaluationPath(keywordLocation, name),
				instanceLocation,
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "unevaluatedProperties":
		if plan.unevaluatedProperties == nil || instance.kind != kindObject {
			return nil, nil
		}
		evaluated, err := plan.collectEvaluatedProperties(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		children := make([]OutputUnit, 0)
		for _, name := range sortedStringKeys(instance.object) {
			if _, exists := evaluated[name]; exists {
				continue
			}
			applied, err := plan.unevaluatedProperties.verboseAppliedSchemaUnits(
				instance.object[name],
				dialect,
				keywordLocation,
				instanceLocation+"/"+escapePointerToken(name),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "prefixItems":
		if instance.kind != kindArray {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for index, child := range plan.prefixItems {
			if index >= len(instance.array) {
				break
			}
			applied, err := child.verboseAppliedSchemaUnits(
				instance.array[index],
				dialect,
				joinEvaluationPath(keywordLocation, strconv.Itoa(index)),
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "items":
		if instance.kind != kindArray {
			return nil, nil
		}
		if dialect != Draft202012 && len(plan.prefixItems) > 0 {
			return verboseTupleItems(
				plan.prefixItems, instance, dialect, keywordLocation,
				instanceLocation, referenced, state,
			)
		}
		if plan.items == nil {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for index := len(plan.prefixItems); index < len(instance.array); index++ {
			applied, err := plan.items.verboseAppliedSchemaUnits(
				instance.array[index],
				dialect,
				keywordLocation,
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "additionalItems":
		if plan.items == nil || instance.kind != kindArray {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for index := len(plan.prefixItems); index < len(instance.array); index++ {
			applied, err := plan.items.verboseAppliedSchemaUnits(
				instance.array[index],
				dialect,
				keywordLocation,
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "contains":
		if plan.contains == nil || instance.kind != kindArray {
			return nil, nil
		}
		children := make([]OutputUnit, 0)
		for index, item := range instance.array {
			applied, err := plan.contains.verboseAppliedSchemaUnits(
				item,
				dialect,
				keywordLocation,
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	case "unevaluatedItems":
		if plan.unevaluatedItems == nil || instance.kind != kindArray {
			return nil, nil
		}
		evaluated, err := plan.collectEvaluatedItems(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		children := make([]OutputUnit, 0)
		for index, item := range instance.array {
			if _, exists := evaluated[index]; exists {
				continue
			}
			applied, err := plan.unevaluatedItems.verboseAppliedSchemaUnits(
				item,
				dialect,
				keywordLocation,
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				state,
			)
			if err != nil {
				return nil, err
			}
			children = append(children, applied...)
		}
		return children, nil
	default:
		return nil, nil
	}
}

func (plan *schemaPlan) propertyIsConfigured(name string) (bool, error) {
	if _, exists := plan.properties[name]; exists {
		return true, nil
	}
	for _, pattern := range plan.patternProperties {
		matched, err := pattern.pattern.matchString(name)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func verboseTupleItems(
	plans []*schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	state *evaluationState,
) ([]OutputUnit, error) {
	children := make([]OutputUnit, 0)
	for index, child := range plans {
		if index >= len(instance.array) {
			break
		}
		applied, err := child.verboseAppliedSchemaUnits(
			instance.array[index],
			dialect,
			joinEvaluationPath(evaluationPath, strconv.Itoa(index)),
			instanceLocation+"/"+strconv.Itoa(index),
			referenced,
			state,
		)
		if err != nil {
			return nil, err
		}
		children = append(children, applied...)
	}
	return children, nil
}

func verboseSchemaArray(
	plans []*schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	state *evaluationState,
) ([]OutputUnit, error) {
	children := make([]OutputUnit, 0)
	for index, child := range plans {
		applied, err := child.verboseAppliedSchemaUnits(
			instance,
			dialect,
			joinEvaluationPath(evaluationPath, strconv.Itoa(index)),
			instanceLocation,
			referenced,
			state,
		)
		if err != nil {
			return nil, err
		}
		children = append(children, applied...)
	}
	return children, nil
}

func verboseSingleSchema(
	plan *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	state *evaluationState,
) ([]OutputUnit, error) {
	if plan == nil {
		return nil, nil
	}
	return plan.verboseAppliedSchemaUnits(
		instance,
		dialect,
		evaluationPath,
		instanceLocation,
		referenced,
		state,
	)
}

func (plan *schemaPlan) verboseAppliedSchemaUnits(
	instance *jsonValue,
	dialect Dialect,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	state *evaluationState,
) ([]OutputUnit, error) {
	if plan.boolean != nil {
		unit := OutputUnit{
			Valid:            *plan.boolean,
			KeywordLocation:  evaluationPath,
			InstanceLocation: instanceLocation,
		}
		if !unit.Valid {
			unit.Error = "boolean schema is false"
		}
		if referenced {
			unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(plan.location)
		}
		return []OutputUnit{unit}, nil
	}
	valid, err := plan.evaluate(instance, dialect, state)
	if err != nil {
		return nil, err
	}
	errors, annotations, err := plan.collectOutput(
		instance,
		dialect,
		evaluationPath,
		instanceLocation,
		referenced,
		true,
		state,
	)
	if err != nil {
		return nil, err
	}
	if valid {
		annotations, err = plan.collectAnnotations(
			instance, dialect, instanceLocation, state,
		)
		if err != nil {
			return nil, err
		}
	}
	return plan.verboseOutputUnits(
		instance,
		errors,
		annotations,
		evaluationPath,
		instanceLocation,
		referenced,
		dialect,
		state,
	)
}

func (plan *schemaPlan) verboseReferenceUnit(
	instance *jsonValue,
	dialect Dialect,
	keywordLocation string,
	instanceLocation string,
	state *evaluationState,
) (OutputUnit, error) {
	target := plan.referenceTarget(state)
	pushed := pushReferenceResource(target, state)
	valid, err := target.evaluate(instance, dialect, state)
	if err == nil {
		var children []OutputUnit
		children, err = target.verboseAppliedSchemaUnits(
			instance,
			dialect,
			keywordLocation,
			instanceLocation,
			true,
			state,
		)
		if err == nil {
			targetUnit := OutputUnit{
				Valid:                   valid,
				KeywordLocation:         keywordLocation,
				AbsoluteKeywordLocation: target.absoluteKeywordLocation(target.location),
				InstanceLocation:        instanceLocation,
			}
			if valid {
				targetUnit.Annotations = children
			} else {
				targetUnit.Errors = children
			}
			unit := OutputUnit{
				Valid:           valid,
				KeywordLocation: keywordLocation,
				AbsoluteKeywordLocation: plan.absoluteKeywordLocation(
					plan.keywordLocation(plan.referenceKeyword),
				),
				InstanceLocation: instanceLocation,
			}
			if valid {
				unit.Annotations = []OutputUnit{targetUnit}
			} else {
				unit.Errors = []OutputUnit{targetUnit}
			}
			if pushed {
				state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
			}
			return unit, nil
		}
	}
	if pushed {
		state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
	}
	return OutputUnit{}, err
}

func outputUnitCoveredByKeywords(
	unit OutputUnit,
	evaluationPath string,
	keywords []string,
) bool {
	for _, keyword := range keywords {
		if keyword == "$defs" || keyword == "definitions" {
			continue
		}
		location := joinEvaluationPath(evaluationPath, keyword)
		if unit.KeywordLocation == location ||
			strings.HasPrefix(unit.KeywordLocation, location+"/") {
			return true
		}
	}
	return false
}

func consumeVerboseOutputUnits(
	state *evaluationState,
	verbose []OutputUnit,
	errors []OutputUnit,
	annotations []OutputUnit,
) error {
	additional := countOutputUnits(verbose) -
		countOutputUnits(errors) - countOutputUnits(annotations)
	if additional <= -1 {
		additional = 0
	}
	return state.consumeOutputUnits(additional)
}

func outputUnitsWithin(units []OutputUnit, keywordLocation string) []OutputUnit {
	result := make([]OutputUnit, 0)
	for _, unit := range units {
		if unit.KeywordLocation == keywordLocation ||
			strings.HasPrefix(unit.KeywordLocation, keywordLocation+"/") {
			result = append(result, unit)
		}
	}
	return result
}

func (plan *schemaPlan) verboseKeywordError(
	keyword string,
	keywordLocation string,
	instanceLocation string,
	referenced bool,
	errors []OutputUnit,
) OutputUnit {
	if len(errors) == 1 && errors[0].KeywordLocation == keywordLocation &&
		errors[0].InstanceLocation == instanceLocation {
		return errors[0]
	}
	unit := OutputUnit{
		Valid:            false,
		KeywordLocation:  keywordLocation,
		InstanceLocation: instanceLocation,
		Errors:           detailedErrors(errors),
	}
	if referenced {
		unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(
			plan.keywordLocation(keyword),
		)
	}
	return unit
}

func countOutputUnits(units []OutputUnit) int {
	count := len(units)
	for _, unit := range units {
		count += countOutputUnits(unit.Errors)
		count += countOutputUnits(unit.Annotations)
	}
	return count
}

func detailedErrors(flat []OutputUnit) []OutputUnit {
	for len(flat) > 0 && flat[0].KeywordLocation == "" &&
		flat[0].Error == "schema evaluation had errors" {
		flat = flat[1:]
	}
	result, _ := nestOutputErrors(flat, "")
	return result
}

func nestOutputErrors(flat []OutputUnit, parent string) ([]OutputUnit, int) {
	result := make([]OutputUnit, 0)
	consumed := 0
	for consumed < len(flat) {
		unit := flat[consumed]
		if parent != "" && !strings.HasPrefix(unit.KeywordLocation, parent+"/") {
			break
		}
		consumed++
		if consumed < len(flat) && unit.KeywordLocation != "" &&
			strings.HasPrefix(flat[consumed].KeywordLocation, unit.KeywordLocation+"/") {
			children, childCount := nestOutputErrors(flat[consumed:], unit.KeywordLocation)
			unit.Errors = children
			unit.Error = ""
			consumed += childCount
		}
		result = append(result, unit)
	}
	return result, consumed
}

func (plan *schemaPlan) collectAnnotations(
	instance *jsonValue,
	dialect Dialect,
	instanceLocation string,
	state *evaluationState,
) ([]OutputUnit, error) {
	dialect = effectiveDialect(plan.dialect, dialect)
	if err := state.consumeOperation(); err != nil {
		return nil, err
	}
	valid, err := plan.evaluate(instance, dialect, state)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, nil
	}
	annotations := make([]OutputUnit, 0)
	for _, keyword := range sortedStringKeys(plan.annotations) {
		if plan.annotationApplies(keyword, instance) {
			annotations = append(
				annotations,
				plan.keywordAnnotation(keyword, instanceLocation, plan.annotations[keyword]),
			)
		}
	}
	for _, keyword := range plan.custom {
		if err := state.consumeCustomKeywordCall(); err != nil {
			return nil, err
		}
		result, err := callKeywordEvaluator(
			state.ctx,
			keyword.evaluator,
			Value{value: instance},
		)
		if err != nil {
			return nil, fmt.Errorf("custom keyword %q: %w", keyword.name, err)
		}
		if result.Annotation == nil {
			continue
		}
		if !annotationWithinLimit(result.Annotation, state.limits.MaxAnnotationBytes) {
			return nil, &LimitError{
				Resource: "annotation bytes",
				Limit:    state.limits.MaxAnnotationBytes,
			}
		}
		annotation, err := decodeJSON(state.ctx, result.Annotation, state.limits)
		if err != nil {
			return nil, fmt.Errorf(
				"custom keyword %q annotation: %w",
				keyword.name,
				err,
			)
		}
		annotations = append(annotations, plan.customKeywordAnnotation(
			keyword.name,
			instanceLocation,
			annotation,
		))
	}
	if plan.reference != nil {
		target := plan.referenceTarget(state)
		pushed := pushReferenceResource(target, state)
		fromReference, err := target.collectAnnotations(
			instance,
			dialect,
			instanceLocation,
			state,
		)
		if pushed {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, fromReference...)
	}
	if instance.kind == kindObject {
		objectAnnotations, err := plan.collectObjectAnnotations(
			instance,
			dialect,
			instanceLocation,
			state,
		)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, objectAnnotations...)
	}
	if instance.kind == kindArray {
		arrayAnnotations, err := plan.collectArrayAnnotations(
			instance,
			dialect,
			instanceLocation,
			state,
		)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, arrayAnnotations...)
	}
	for _, child := range plan.allOf {
		childAnnotations, err := child.collectAnnotations(
			instance, dialect, instanceLocation, state,
		)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, childAnnotations...)
	}
	for _, children := range [][]*schemaPlan{plan.anyOf, plan.oneOf} {
		for _, child := range children {
			childAnnotations, err := child.collectAnnotations(
				instance, dialect, instanceLocation, state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, childAnnotations...)
		}
	}
	if plan.condition != nil {
		matched, err := plan.condition.evaluate(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		branch := plan.otherwise
		if matched {
			conditionAnnotations, err := plan.condition.collectAnnotations(
				instance, dialect, instanceLocation, state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, conditionAnnotations...)
			branch = plan.then
		}
		if branch != nil {
			branchAnnotations, err := branch.collectAnnotations(
				instance, dialect, instanceLocation, state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, branchAnnotations...)
		}
	}
	return annotations, nil
}

func (plan *schemaPlan) collectObjectAnnotations(
	instance *jsonValue,
	dialect Dialect,
	instanceLocation string,
	state *evaluationState,
) ([]OutputUnit, error) {
	annotations := make([]OutputUnit, 0)
	for _, name := range sortedStringKeys(instance.object) {
		value := instance.object[name]
		location := instanceLocation + "/" + escapePointerToken(name)
		matched := false
		if property := plan.properties[name]; property != nil {
			matched = true
			child, err := property.collectAnnotations(value, dialect, location, state)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
		for _, pattern := range plan.patternProperties {
			patternMatched, err := pattern.pattern.matchString(name)
			if err != nil {
				return nil, err
			}
			if !patternMatched {
				continue
			}
			matched = true
			child, err := pattern.schema.collectAnnotations(value, dialect, location, state)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
		if !matched && plan.additionalProperties != nil {
			child, err := plan.additionalProperties.collectAnnotations(
				value, dialect, location, state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
	}
	for _, name := range sortedStringKeys(plan.dependentSchemas) {
		if _, exists := instance.object[name]; !exists {
			continue
		}
		child, err := plan.dependentSchemas[name].collectAnnotations(
			instance, dialect, instanceLocation, state,
		)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, child...)
	}
	if plan.unevaluatedProperties != nil {
		evaluated, err := plan.collectEvaluatedProperties(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedStringKeys(instance.object) {
			if _, exists := evaluated[name]; exists {
				continue
			}
			child, err := plan.unevaluatedProperties.collectAnnotations(
				instance.object[name],
				dialect,
				instanceLocation+"/"+escapePointerToken(name),
				state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
	}
	return annotations, nil
}

func (plan *schemaPlan) collectArrayAnnotations(
	instance *jsonValue,
	dialect Dialect,
	instanceLocation string,
	state *evaluationState,
) ([]OutputUnit, error) {
	annotations := make([]OutputUnit, 0)
	for index, childPlan := range plan.prefixItems {
		if index >= len(instance.array) {
			break
		}
		child, err := childPlan.collectAnnotations(
			instance.array[index],
			dialect,
			instanceLocation+"/"+fmt.Sprint(index),
			state,
		)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, child...)
	}
	if plan.items != nil {
		for index := len(plan.prefixItems); index < len(instance.array); index++ {
			child, err := plan.items.collectAnnotations(
				instance.array[index],
				dialect,
				instanceLocation+"/"+fmt.Sprint(index),
				state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
	}
	if plan.contains != nil {
		for index, item := range instance.array {
			child, err := plan.contains.collectAnnotations(
				item,
				dialect,
				instanceLocation+"/"+fmt.Sprint(index),
				state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
	}
	if plan.unevaluatedItems != nil {
		evaluated, err := plan.collectEvaluatedItems(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		for index, item := range instance.array {
			if _, exists := evaluated[index]; exists {
				continue
			}
			child, err := plan.unevaluatedItems.collectAnnotations(
				item,
				dialect,
				instanceLocation+"/"+fmt.Sprint(index),
				state,
			)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, child...)
		}
	}
	return annotations, nil
}

func (plan *schemaPlan) collectOutput(
	instance *jsonValue,
	dialect Dialect,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	branchRequired bool,
	state *evaluationState,
) ([]OutputUnit, []OutputUnit, error) {
	outputStart := state.outputUnits
	if err := state.consumeOperation(); err != nil {
		return nil, nil, err
	}
	if plan.boolean != nil {
		if *plan.boolean {
			return nil, nil, nil
		}
		if err := state.consumeOutputUnits(1); err != nil {
			return nil, nil, err
		}
		return []OutputUnit{plan.outputErrorAt(
			evaluationPath,
			instanceLocation,
			referenced,
			"boolean schema is false",
		)}, nil, nil
	}
	errors := make([]OutputUnit, 0)
	annotations := make([]OutputUnit, 0)
	if plan.reference != nil {
		referenceKeyword := plan.referenceKeyword
		if referenceKeyword == "" {
			referenceKeyword = "$ref"
		}
		target := plan.referenceTarget(state)
		pushed := pushReferenceResource(target, state)
		childErrors, childAnnotations, err := target.collectOutput(
			instance,
			dialect,
			joinEvaluationPath(evaluationPath, referenceKeyword),
			instanceLocation,
			true,
			true,
			state,
		)
		if pushed {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}
		if err != nil {
			return nil, nil, err
		}
		errors = append(errors, childErrors...)
		annotations = append(annotations, childAnnotations...)
	}
	for _, applicator := range []struct {
		keyword  string
		children []*schemaPlan
	}{
		{keyword: "allOf", children: plan.allOf},
		{keyword: "anyOf", children: plan.anyOf},
		{keyword: "oneOf", children: plan.oneOf},
	} {
		keyword := applicator.keyword
		children := applicator.children
		if len(children) == 0 {
			continue
		}
		validBranches := make([]struct{}, 0, len(children))
		childErrors := make([]OutputUnit, 0)
		childAnnotations := make([]OutputUnit, 0)
		for index, child := range children {
			valid, err := child.evaluate(instance, dialect, state)
			if err != nil {
				return nil, nil, err
			}
			if valid {
				validBranches = append(validBranches, struct{}{})
			}
			branchErrors, branchAnnotations, err := child.collectOutput(
				instance,
				dialect,
				joinEvaluationPath(
					joinEvaluationPath(evaluationPath, keyword), strconv.Itoa(index),
				),
				instanceLocation,
				referenced,
				false,
				state,
			)
			if err != nil {
				return nil, nil, err
			}
			childErrors = append(childErrors, branchErrors...)
			if valid {
				childAnnotations = append(childAnnotations, branchAnnotations...)
			}
		}
		keywordValid := len(validBranches) == len(children)
		switch keyword {
		case "anyOf":
			keywordValid = len(validBranches) > 0
		case "oneOf":
			keywordValid = len(validBranches) == 1
		}
		if !keywordValid {
			errors = append(errors, plan.keywordErrorAt(
				keyword, evaluationPath, instanceLocation, referenced,
				"applicator did not satisfy its branch requirement",
			))
			errors = append(errors, childErrors...)
		} else {
			annotations = append(annotations, childAnnotations...)
		}
	}
	if plan.not != nil {
		valid, err := plan.not.evaluate(instance, dialect, state)
		if err != nil {
			return nil, nil, err
		}
		if valid {
			errors = append(errors, plan.keywordErrorAt(
				"not", evaluationPath, instanceLocation, referenced,
				"instance satisfies the prohibited schema",
			))
		}
	}
	if plan.condition != nil {
		matched, err := plan.condition.evaluate(instance, dialect, state)
		if err != nil {
			return nil, nil, err
		}
		keyword := "else"
		branch := plan.otherwise
		if matched {
			keyword = "then"
			branch = plan.then
		}
		if branch != nil {
			childErrors, childAnnotations, err := branch.collectOutput(
				instance,
				dialect,
				joinEvaluationPath(evaluationPath, keyword),
				instanceLocation,
				referenced,
				false,
				state,
			)
			if err != nil {
				return nil, nil, err
			}
			errors = append(errors, childErrors...)
			annotations = append(annotations, childAnnotations...)
		}
	}
	for _, keyword := range plan.custom {
		if err := state.consumeCustomKeywordCall(); err != nil {
			return nil, nil, err
		}
		result, err := callKeywordEvaluator(
			state.ctx,
			keyword.evaluator,
			Value{value: instance},
		)
		if err != nil {
			return nil, nil, fmt.Errorf("custom keyword %q: %w", keyword.name, err)
		}
		if !result.Valid {
			errors = append(errors, plan.keywordErrorAt(
				keyword.name,
				evaluationPath,
				instanceLocation,
				referenced,
				"custom keyword validation failed",
			))
		}
		if result.Valid && result.Annotation != nil {
			if !annotationWithinLimit(result.Annotation, state.limits.MaxAnnotationBytes) {
				return nil, nil, &LimitError{
					Resource: "annotation bytes",
					Limit:    state.limits.MaxAnnotationBytes,
				}
			}
			annotation, err := decodeJSON(state.ctx, result.Annotation, state.limits)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"custom keyword %q annotation: %w", keyword.name, err,
				)
			}
			annotations = append(annotations, plan.keywordAnnotationAt(
				keyword.name,
				evaluationPath,
				instanceLocation,
				referenced,
				annotation,
			))
		}
	}
	if len(plan.types) > 0 {
		matched := false
		for _, candidate := range plan.types {
			if candidate.schema != nil {
				valid, err := candidate.schema.evaluate(instance, dialect, state)
				if err != nil {
					return nil, nil, err
				}
				matched = matched || valid
			} else {
				matched = matched || matchesType(instance, candidate.name, dialect)
			}
		}
		if !matched {
			errors = append(errors, plan.keywordErrorAt(
				"type",
				evaluationPath,
				instanceLocation,
				referenced,
				"instance does not match an allowed type",
			))
		}
	}
	for _, disallowed := range plan.disallowedTypes {
		matched := false
		if disallowed.schema != nil {
			valid, err := disallowed.schema.evaluate(instance, dialect, state)
			if err != nil {
				return nil, nil, err
			}
			matched = valid
		} else {
			matched = matchesType(instance, disallowed.name, dialect)
		}
		if matched {
			errors = append(errors, plan.keywordErrorAt(
				"disallow",
				evaluationPath,
				instanceLocation,
				referenced,
				"instance matches a disallowed type or schema",
			))
			break
		}
	}
	if plan.hasEnum {
		matched := false
		for _, candidate := range plan.enum {
			matched = matched || equalJSON(candidate, instance)
		}
		if !matched {
			errors = append(errors, plan.keywordErrorAt(
				"enum", evaluationPath, instanceLocation, referenced,
				"instance is not one of the allowed values",
			))
		}
	}
	if plan.hasConst && !equalJSON(plan.constant, instance) {
		errors = append(errors, plan.keywordErrorAt(
			"const", evaluationPath, instanceLocation, referenced,
			"instance does not equal the required value",
		))
	}
	if instance.kind == kindNumber {
		for _, minimum := range plan.minimums {
			comparison := compareNumber(instance.number, minimum.number)
			if belowMinimum(comparison, minimum.exclusive) {
				keyword := boundKeyword("minimum", minimum.exclusive, dialect)
				errors = append(errors, plan.keywordErrorAt(
					keyword, evaluationPath, instanceLocation, referenced,
					"number is below the allowed bound",
				))
			}
		}
		for _, maximum := range plan.maximums {
			comparison := compareNumber(instance.number, maximum.number)
			if aboveMaximum(comparison, maximum.exclusive) {
				keyword := boundKeyword("maximum", maximum.exclusive, dialect)
				errors = append(errors, plan.keywordErrorAt(
					keyword, evaluationPath, instanceLocation, referenced,
					"number is above the allowed bound",
				))
			}
		}
		if plan.multipleOf != "" {
			if !numberIsMultiple(instance.number, plan.multipleOf) {
				keyword := "multipleOf"
				if dialect == Draft3 {
					keyword = "divisibleBy"
				}
				errors = append(errors, plan.keywordErrorAt(
					keyword, evaluationPath, instanceLocation, referenced,
					"number is not a multiple of the required value",
				))
			}
		}
	}
	if instance.kind == kindString {
		length := utf8.RuneCountInString(instance.text)
		if belowConfiguredMinimum(strconv.Itoa(length), plan.minLength) {
			errors = append(errors, plan.keywordErrorAt(
				"minLength", evaluationPath, instanceLocation, referenced,
				"string is shorter than allowed",
			))
		}
		if aboveConfiguredMaximum(strconv.Itoa(length), plan.maxLength) {
			errors = append(errors, plan.keywordErrorAt(
				"maxLength", evaluationPath, instanceLocation, referenced,
				"string is longer than allowed",
			))
		}
		if plan.pattern != nil {
			matched, err := plan.pattern.matchString(instance.text)
			if err != nil {
				return nil, nil, err
			}
			if !matched {
				errors = append(errors, plan.keywordErrorAt(
					"pattern", evaluationPath, instanceLocation, referenced,
					"string does not match the required pattern",
				))
			}
		}
		if plan.format != nil {
			if err := state.consumeFormatCheck(); err != nil {
				return nil, nil, err
			}
			valid, err := callFormatChecker(state.ctx, plan.format, instance.text)
			if err != nil {
				return nil, nil, fmt.Errorf("format validation: %w", err)
			}
			if !valid {
				errors = append(errors, plan.keywordErrorAt(
					"format", evaluationPath, instanceLocation, referenced,
					"string does not satisfy the required format",
				))
			}
		}
		if plan.contentEncoding != "" || plan.contentMediaType != "" {
			valid, err := plan.validateContent(instance.text, state)
			if err != nil {
				return nil, nil, err
			}
			if !valid {
				keyword := "contentMediaType"
				if plan.contentEncoding != "" {
					keyword = "contentEncoding"
				}
				errors = append(errors, plan.keywordErrorAt(
					keyword, evaluationPath, instanceLocation, referenced,
					"string content is invalid",
				))
			}
		}
	}
	if instance.kind == kindObject {
		propertyCount := strconv.Itoa(len(instance.object))
		if belowConfiguredMinimum(propertyCount, plan.minProperties) {
			errors = append(errors, plan.keywordErrorAt(
				"minProperties", evaluationPath, instanceLocation, referenced,
				"object has too few properties",
			))
		}
		if aboveConfiguredMaximum(propertyCount, plan.maxProperties) {
			errors = append(errors, plan.keywordErrorAt(
				"maxProperties", evaluationPath, instanceLocation, referenced,
				"object has too many properties",
			))
		}
		for _, name := range plan.required {
			if _, exists := instance.object[name]; !exists {
				errors = append(errors, plan.keywordErrorAt(
					"required",
					evaluationPath,
					instanceLocation,
					referenced,
					fmt.Sprintf("required property %q is missing", name),
				))
			}
		}
		names := make([]string, 0, len(plan.properties))
		for name := range plan.properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			childInstance, exists := instance.object[name]
			if !exists {
				continue
			}
			childErrors, childAnnotations, err := plan.properties[name].collectOutput(
				childInstance,
				dialect,
				joinEvaluationPath(
					joinEvaluationPath(evaluationPath, "properties"),
					name,
				),
				instanceLocation+"/"+escapePointerToken(name),
				referenced,
				false,
				state,
			)
			if err != nil {
				return nil, nil, err
			}
			errors = append(errors, childErrors...)
			annotations = append(annotations, childAnnotations...)
		}
		for _, name := range sortedStringKeys(instance.object) {
			_, configured := plan.properties[name]
			matchedPattern := false
			for _, pattern := range plan.patternProperties {
				matched, err := pattern.pattern.matchString(name)
				if err != nil {
					return nil, nil, err
				}
				if !matched {
					continue
				}
				matchedPattern = true
				childErrors, childAnnotations, err := pattern.schema.collectOutput(
					instance.object[name],
					dialect,
					joinEvaluationPath(
						joinEvaluationPath(evaluationPath, "patternProperties"),
						pattern.name,
					),
					instanceLocation+"/"+escapePointerToken(name),
					referenced,
					false,
					state,
				)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
			if !configured && !matchedPattern && plan.additionalProperties != nil {
				childErrors, childAnnotations, err := plan.additionalProperties.collectOutput(
					instance.object[name],
					dialect,
					joinEvaluationPath(evaluationPath, "additionalProperties"),
					instanceLocation+"/"+escapePointerToken(name),
					referenced,
					false,
					state,
				)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
			if plan.propertyNames != nil {
				childErrors, childAnnotations, err := plan.propertyNames.collectOutput(
					&jsonValue{kind: kindString, text: name},
					dialect,
					joinEvaluationPath(evaluationPath, "propertyNames"),
					instanceLocation+"/"+escapePointerToken(name),
					referenced,
					false,
					state,
				)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
		}
		for _, name := range sortedStringKeys(plan.dependentRequired) {
			if _, exists := instance.object[name]; !exists {
				continue
			}
			for _, required := range plan.dependentRequired[name] {
				if _, exists := instance.object[required]; !exists {
					errors = append(errors, plan.keywordErrorAt(
						"dependentRequired", evaluationPath, instanceLocation, referenced,
						fmt.Sprintf("property %q requires %q", name, required),
					))
				}
			}
		}
		for _, name := range sortedStringKeys(plan.dependentSchemas) {
			if _, exists := instance.object[name]; !exists {
				continue
			}
			childErrors, childAnnotations, err := plan.dependentSchemas[name].collectOutput(
				instance,
				dialect,
				joinEvaluationPath(
					joinEvaluationPath(evaluationPath, "dependentSchemas"), name,
				),
				instanceLocation,
				referenced,
				false,
				state,
			)
			if err != nil {
				return nil, nil, err
			}
			errors = append(errors, childErrors...)
			annotations = append(annotations, childAnnotations...)
		}
		if plan.unevaluatedProperties != nil {
			evaluated, err := plan.collectEvaluatedProperties(instance, dialect, state)
			if err != nil {
				return nil, nil, err
			}
			for _, name := range sortedStringKeys(instance.object) {
				if _, exists := evaluated[name]; exists {
					continue
				}
				childErrors, childAnnotations, err :=
					plan.unevaluatedProperties.collectOutput(
						instance.object[name],
						dialect,
						joinEvaluationPath(evaluationPath, "unevaluatedProperties"),
						instanceLocation+"/"+escapePointerToken(name),
						referenced,
						false,
						state,
					)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
		}
	}
	if instance.kind == kindArray {
		prefixKeyword := "prefixItems"
		if dialect != Draft202012 {
			prefixKeyword = "items"
		}
		for index, childPlan := range plan.prefixItems {
			if index >= len(instance.array) {
				break
			}
			childErrors, childAnnotations, err := childPlan.collectOutput(
				instance.array[index],
				dialect,
				joinEvaluationPath(
					joinEvaluationPath(evaluationPath, prefixKeyword),
					strconv.Itoa(index),
				),
				instanceLocation+"/"+strconv.Itoa(index),
				referenced,
				false,
				state,
			)
			if err != nil {
				return nil, nil, err
			}
			errors = append(errors, childErrors...)
			annotations = append(annotations, childAnnotations...)
		}
		if plan.items != nil {
			itemsKeyword := "items"
			if dialect != Draft202012 && len(plan.prefixItems) > 0 {
				itemsKeyword = "additionalItems"
			}
			for index := len(plan.prefixItems); index < len(instance.array); index++ {
				childErrors, childAnnotations, err := plan.items.collectOutput(
					instance.array[index],
					dialect,
					joinEvaluationPath(evaluationPath, itemsKeyword),
					instanceLocation+"/"+strconv.Itoa(index),
					referenced,
					false,
					state,
				)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
		}
		if belowConfiguredMinimum(strconv.Itoa(len(instance.array)), plan.minItems) {
			errors = append(errors, plan.keywordErrorAt(
				"minItems",
				evaluationPath,
				instanceLocation,
				referenced,
				fmt.Sprintf("expected at least %s items", *plan.minItems),
			))
		}
		if aboveConfiguredMaximum(strconv.Itoa(len(instance.array)), plan.maxItems) {
			errors = append(errors, plan.keywordErrorAt(
				"maxItems",
				evaluationPath,
				instanceLocation,
				referenced,
				fmt.Sprintf("expected at most %s items", *plan.maxItems),
			))
		}
		if plan.contains != nil {
			matched, err := plan.matchingContainsItems(instance, dialect, state)
			if err != nil {
				return nil, nil, err
			}
			minimum := "1"
			if plan.minContains != nil {
				minimum = *plan.minContains
			}
			actual := strconv.Itoa(len(matched))
			if outsideConfiguredCardinality(actual, minimum, plan.maxContains) {
				errors = append(errors, plan.keywordErrorAt(
					"contains", evaluationPath, instanceLocation, referenced,
					"array has an invalid number of matching items",
				))
			}
		}
		if plan.uniqueItems {
			unique, err := uniqueJSON(instance.array, state)
			if err != nil {
				return nil, nil, err
			}
			if !unique {
				errors = append(errors, plan.keywordErrorAt(
					"uniqueItems", evaluationPath, instanceLocation, referenced,
					"array items are not unique",
				))
			}
		}
		if plan.unevaluatedItems != nil {
			evaluated, err := plan.collectEvaluatedItems(instance, dialect, state)
			if err != nil {
				return nil, nil, err
			}
			for index, item := range instance.array {
				if _, exists := evaluated[index]; exists {
					continue
				}
				childErrors, childAnnotations, err := plan.unevaluatedItems.collectOutput(
					item,
					dialect,
					joinEvaluationPath(evaluationPath, "unevaluatedItems"),
					instanceLocation+"/"+strconv.Itoa(index),
					referenced,
					false,
					state,
				)
				if err != nil {
					return nil, nil, err
				}
				errors = append(errors, childErrors...)
				annotations = append(annotations, childAnnotations...)
			}
		}
	}
	valid, err := plan.evaluate(instance, dialect, state)
	if err != nil {
		return nil, nil, err
	}
	if valid {
		for _, keyword := range sortedStringKeys(plan.annotations) {
			if !plan.annotationApplies(keyword, instance) {
				continue
			}
			annotations = append(
				annotations,
				plan.keywordAnnotationAt(
					keyword,
					evaluationPath,
					instanceLocation,
					referenced,
					plan.annotations[keyword],
				),
			)
		}
	}
	if shouldWrapBranchError(valid, len(errors), branchRequired) {
		errors = append(
			[]OutputUnit{plan.outputErrorAt(
				evaluationPath,
				instanceLocation,
				referenced,
				"schema evaluation had errors",
			)},
			errors...,
		)
	}
	directUnits := uncountedOutputUnits(
		countOutputUnits(errors),
		countOutputUnits(annotations),
		outputStart,
		state.outputUnits,
	)
	if err := state.consumeOutputUnits(directUnits); err != nil {
		return nil, nil, err
	}
	return errors, annotations, nil
}

func annotationWithinLimit(annotation []byte, limit int) bool {
	return len(annotation) <= limit
}

func belowMinimum(comparison int, exclusive bool) bool {
	return comparison < 0 || comparison == 0 && exclusive
}

func aboveMaximum(comparison int, exclusive bool) bool {
	return comparison > 0 || comparison == 0 && exclusive
}

func boundKeyword(keyword string, exclusive bool, dialect Dialect) string {
	if exclusive && dialect != Draft3 && dialect != Draft4 {
		return "exclusive" + strings.ToUpper(keyword[:1]) + keyword[1:]
	}
	return keyword
}

func belowConfiguredMinimum(actual string, minimum *string) bool {
	return minimum != nil && compareNumber(actual, *minimum) < 0
}

func aboveConfiguredMaximum(actual string, maximum *string) bool {
	return maximum != nil && compareNumber(actual, *maximum) > 0
}

func outsideConfiguredCardinality(actual, minimum string, maximum *string) bool {
	return compareNumber(actual, minimum) < 0 ||
		maximum != nil && compareNumber(actual, *maximum) > 0
}

func shouldWrapBranchError(valid bool, errorCount int, branchRequired bool) bool {
	return !valid && errorCount > 0 && branchRequired
}

func uncountedOutputUnits(
	errors int,
	annotations int,
	previouslyCounted int,
	currentlyCounted int,
) int {
	return errors + annotations - (currentlyCounted - previouslyCounted)
}

func joinEvaluationPath(path string, token string) string {
	return path + "/" + escapePointerToken(token)
}

func (plan *schemaPlan) keywordErrorAt(
	keyword string,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	message string,
) OutputUnit {
	unit := OutputUnit{
		Valid:            false,
		KeywordLocation:  joinEvaluationPath(evaluationPath, keyword),
		InstanceLocation: instanceLocation,
		Error:            message,
	}
	if referenced {
		unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(
			plan.keywordLocation(keyword),
		)
	}
	return unit
}

func (plan *schemaPlan) outputErrorAt(
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	message string,
) OutputUnit {
	unit := OutputUnit{
		Valid:            false,
		KeywordLocation:  evaluationPath,
		InstanceLocation: instanceLocation,
		Error:            message,
	}
	if referenced {
		unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(plan.location)
	}
	return unit
}

func (plan *schemaPlan) keywordAnnotationAt(
	keyword string,
	evaluationPath string,
	instanceLocation string,
	referenced bool,
	value *jsonValue,
) OutputUnit {
	unit := OutputUnit{
		Valid:            true,
		KeywordLocation:  joinEvaluationPath(evaluationPath, keyword),
		InstanceLocation: instanceLocation,
		Annotation:       jsonValueOutput(value),
	}
	if referenced {
		unit.AbsoluteKeywordLocation = plan.absoluteKeywordLocation(
			plan.keywordLocation(keyword),
		)
	}
	return unit
}

func (plan *schemaPlan) outputError(instanceLocation string, message string) OutputUnit {
	return OutputUnit{
		Valid:                   false,
		KeywordLocation:         plan.location,
		AbsoluteKeywordLocation: plan.absoluteKeywordLocation(plan.location),
		InstanceLocation:        instanceLocation,
		Error:                   message,
	}
}

func (plan *schemaPlan) keywordAnnotation(
	keyword string,
	instanceLocation string,
	value *jsonValue,
) OutputUnit {
	location := plan.keywordLocation(keyword)
	return OutputUnit{
		Valid:                   true,
		KeywordLocation:         location,
		AbsoluteKeywordLocation: plan.absoluteKeywordLocation(location),
		InstanceLocation:        instanceLocation,
		Annotation:              jsonValueOutput(value),
	}
}

func (plan *schemaPlan) customKeywordAnnotation(
	keyword string,
	instanceLocation string,
	value *jsonValue,
) OutputUnit {
	location := plan.keywordLocation(keyword)
	return OutputUnit{
		Valid:                   true,
		KeywordLocation:         location,
		AbsoluteKeywordLocation: plan.absoluteKeywordLocation(location),
		InstanceLocation:        instanceLocation,
		Annotation:              jsonValueOutput(value),
	}
}

func (plan *schemaPlan) keywordLocation(keyword string) string {
	return plan.location + "/" + escapePointerToken(keyword)
}

func (plan *schemaPlan) absoluteKeywordLocation(location string) string {
	if plan.absoluteBase == "" {
		return ""
	}
	return strings.TrimSuffix(plan.absoluteBase, "#") + "#" + location
}

func escapePointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func jsonValueOutput(value *jsonValue) any {
	switch value.kind {
	case kindNull:
		return nil
	case kindBoolean:
		return value.boolean
	case kindNumber:
		return json.Number(value.number)
	case kindString:
		return value.text
	case kindArray:
		result := make([]any, len(value.array))
		for index, item := range value.array {
			result[index] = jsonValueOutput(item)
		}
		return result
	case kindObject:
		result := make(map[string]any, len(value.object))
		for name, item := range value.object {
			result[name] = jsonValueOutput(item)
		}
		return result
	default:
		return nil
	}
}

func (plan *schemaPlan) annotationApplies(keyword string, instance *jsonValue) bool {
	switch keyword {
	case "contentSchema":
		_, hasMediaType := plan.annotations["contentMediaType"]
		return instance.kind == kindString && hasMediaType
	case "contentEncoding", "contentMediaType", "format":
		return instance.kind == kindString
	default:
		return true
	}
}

func isAnnotationKeyword(keyword string) bool {
	switch keyword {
	case "contentEncoding", "contentMediaType", "contentSchema", "default",
		"deprecated", "description", "examples", "format", "readOnly",
		"title", "writeOnly":
		return true
	default:
		return false
	}
}

func isKnownKeyword(keyword string) bool {
	switch keyword {
	case "$anchor", "$comment", "$defs", "$dynamicAnchor", "$dynamicRef",
		"$id", "$recursiveAnchor", "$recursiveRef", "$ref", "$schema",
		"$vocabulary", "additionalItems", "additionalProperties", "allOf",
		"anyOf", "const", "contains", "contentEncoding", "contentMediaType",
		"contentSchema", "default", "definitions", "dependencies",
		"dependentRequired", "dependentSchemas", "deprecated", "description",
		"disallow", "divisibleBy", "else", "enum", "examples", "exclusiveMaximum",
		"exclusiveMinimum", "extends", "format", "id", "if", "items",
		"maxContains", "maximum", "maxItems", "maxLength", "maxProperties",
		"minContains", "minimum", "minItems", "minLength", "minProperties",
		"multipleOf", "not", "oneOf", "pattern", "patternProperties",
		"prefixItems", "properties", "propertyNames", "readOnly", "required",
		"then", "title", "type", "unevaluatedItems", "unevaluatedProperties",
		"uniqueItems", "writeOnly":
		return true
	default:
		return false
	}
}
