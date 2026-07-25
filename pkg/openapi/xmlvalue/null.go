// Package xmlvalue provides OpenAPI XML representation decisions without
// coupling them to a particular XML encoder or application model.
package xmlvalue

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// XMLSchemaInstanceNamespace is the namespace of xsi:nil.
const XMLSchemaInstanceNamespace = "http://www.w3.org/2001/XMLSchema-instance"

// ErrInvalidInput reports an invalid XML null-handling request.
var ErrInvalidInput = errors.New("invalid XML null input")

// NodeKind identifies the XML representation of a Schema Object property.
type NodeKind uint8

const (
	// Element represents a property serialized as an XML element.
	Element NodeKind = iota
	// Attribute represents a property serialized as an XML attribute.
	Attribute
)

// NullPlan describes the OpenAPI-recommended XML representation of null.
type NullPlan struct {
	Omit         bool
	EmptyElement bool
	Attributes   []xml.Attr
}

// PlanNull returns the OpenAPI-recommended representation for a null property.
// Elements become empty elements carrying xsi:nil="true"; attributes are
// omitted because XML has no distinct null attribute representation.
func PlanNull(kind NodeKind) (NullPlan, error) {
	switch kind {
	case Element:
		return NullPlan{
			EmptyElement: true,
			Attributes: []xml.Attr{{
				Name: xml.Name{
					Space: XMLSchemaInstanceNamespace,
					Local: "nil",
				},
				Value: "true",
			}},
		}, nil
	case Attribute:
		return NullPlan{Omit: true}, nil
	default:
		return NullPlan{}, ErrInvalidInput
	}
}

// RestoreOmittedAttribute restores a missing in-memory property as JSON null
// exactly when its compiled property schema accepts null. Existing properties
// and properties whose schemas reject null remain unchanged.
func RestoreOmittedAttribute(
	ctx context.Context,
	instance jsonvalue.Value,
	property string,
	schema *jsonschema.Schema,
) (jsonvalue.Value, bool, error) {
	if ctx == nil || instance.Kind() != jsonvalue.ObjectKind ||
		property == "" || schema == nil {
		return jsonvalue.Value{}, false, ErrInvalidInput
	}
	if _, present := instance.Lookup(property); present {
		return instance, false, nil
	}
	result, err := schema.ValidateValue(ctx, nil)
	if err != nil {
		return jsonvalue.Value{}, false, err
	}
	if !result.Valid {
		return instance, false, nil
	}
	members, _ := instance.Members()
	members = append(members, jsonvalue.Member{Name: property, Value: jsonvalue.Null()})
	restored, err := jsonvalue.Object(members)
	if err != nil {
		return jsonvalue.Value{}, false, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return restored, true, nil
}
