package jsonapi

import (
	"encoding/json"
	"strconv"
)

// MarshalAtomic validates and deterministically encodes an Atomic Operations
// document.
func MarshalAtomic(document AtomicDocument) ([]byte, error) {
	return MarshalAtomicWith(document, AtomicValidationOptions{})
}

// MarshalAtomicWith validates in the supplied Atomic protocol context and
// deterministically encodes a document.
func MarshalAtomicWith(
	document AtomicDocument,
	options AtomicValidationOptions,
) ([]byte, error) {
	if err := document.ValidateWith(options); err != nil {
		return nil, err
	}

	return json.Marshal(document)
}

// UnmarshalAtomic strictly decodes and validates an Atomic Operations
// document.
func UnmarshalAtomic(payload []byte) (AtomicDocument, error) {
	return UnmarshalAtomicWith(payload, AtomicValidationOptions{})
}

// UnmarshalAtomicWith strictly decodes and validates a document in the
// supplied Atomic protocol context.
func UnmarshalAtomicWith(
	payload []byte,
	options AtomicValidationOptions,
) (AtomicDocument, error) {
	return UnmarshalAtomicWithLimits(payload, options, DecodeLimits{})
}

// UnmarshalAtomicWithLimits strictly decodes and validates an Atomic document
// with explicit resource limits. Zero limit fields use production defaults.
func UnmarshalAtomicWithLimits(
	payload []byte,
	options AtomicValidationOptions,
	limits DecodeLimits,
) (AtomicDocument, error) {
	limits, err := normalizeDecodeLimits(limits)
	if err != nil {
		return AtomicDocument{}, err
	}
	if err := validateJSONPayload(payload, limits); err != nil {
		return AtomicDocument{}, err
	}
	if err := rejectDuplicateMembersWithLimits(payload, limits); err != nil {
		return AtomicDocument{}, err
	}

	root, err := decodeObject(payload, "")
	if err != nil {
		return AtomicDocument{}, err
	}
	if _, exists := root["data"]; exists {
		return AtomicDocument{}, decodeFailure(
			"/data",
			"forbidden",
			"atomic documents must not contain data",
			nil,
		)
	}
	if _, exists := root["included"]; exists {
		return AtomicDocument{}, decodeFailure(
			"/included",
			"forbidden",
			"atomic documents must not contain included",
			nil,
		)
	}
	if err := rejectUnknown(
		root,
		"",
		"jsonapi",
		"links",
		"atomic:operations",
		"atomic:results",
		"errors",
		"meta",
	); err != nil {
		return AtomicDocument{}, err
	}

	var document AtomicDocument
	if raw, exists := root["jsonapi"]; exists {
		object, decodeErr := decodeJSONAPI(raw, "/jsonapi")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.JSONAPI = &object
	}
	if raw, exists := root["links"]; exists {
		links, decodeErr := decodeLinks(raw, "/links")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.Links = links
	}
	if raw, exists := root["atomic:operations"]; exists {
		operations, decodeErr := decodeAtomicOperations(raw, "/atomic:operations")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.Operations = operations
	}
	if raw, exists := root["atomic:results"]; exists {
		results, decodeErr := decodeAtomicResults(raw, "/atomic:results")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.Results = results
	}
	if raw, exists := root["errors"]; exists {
		errorsMember, decodeErr := decodeErrors(raw, "/errors")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.Errors = errorsMember
	}
	if raw, exists := root["meta"]; exists {
		meta, decodeErr := decodeMeta(raw, "/meta")
		if decodeErr != nil {
			return AtomicDocument{}, decodeErr
		}
		document.Meta = meta
	}

	if err := document.ValidateWith(options); err != nil {
		return AtomicDocument{}, err
	}

	return document, nil
}

// MarshalJSON preserves explicitly empty Atomic Operations members.
func (document AtomicDocument) MarshalJSON() ([]byte, error) {
	var links *Links
	if document.Links != nil {
		links = &document.Links
	}
	var operations *[]AtomicOperation
	if document.Operations != nil {
		operations = &document.Operations
	}
	var results *[]AtomicResult
	if document.Results != nil {
		results = &document.Results
	}
	var errorsMember *[]ErrorObject
	if document.Errors != nil {
		errorsMember = &document.Errors
	}
	var meta *Meta
	if document.Meta != nil {
		meta = &document.Meta
	}

	return json.Marshal(struct {
		JSONAPI    *JSONAPI           `json:"jsonapi,omitempty"`
		Links      *Links             `json:"links,omitempty"`
		Operations *[]AtomicOperation `json:"atomic:operations,omitempty"`
		Results    *[]AtomicResult    `json:"atomic:results,omitempty"`
		Errors     *[]ErrorObject     `json:"errors,omitempty"`
		Meta       *Meta              `json:"meta,omitempty"`
	}{
		JSONAPI:    document.JSONAPI,
		Links:      links,
		Operations: operations,
		Results:    results,
		Errors:     errorsMember,
		Meta:       meta,
	})
}

// MarshalJSON preserves an explicitly empty operation meta object.
func (operation AtomicOperation) MarshalJSON() ([]byte, error) {
	var meta *Meta
	if operation.Meta != nil {
		meta = &operation.Meta
	}

	return json.Marshal(struct {
		Op   AtomicOperationCode `json:"op"`
		Ref  *AtomicReference    `json:"ref,omitempty"`
		Href *string             `json:"href,omitempty"`
		Data *PrimaryData        `json:"data,omitempty"`
		Meta *Meta               `json:"meta,omitempty"`
	}{
		Op:   operation.Op,
		Ref:  operation.Ref,
		Href: optionalString(operation.Href, operation.hrefPresent),
		Data: operation.Data,
		Meta: meta,
	})
}

// MarshalJSON preserves explicitly empty reference identity members.
func (reference AtomicReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string  `json:"type"`
		ID           *string `json:"id,omitempty"`
		LID          *string `json:"lid,omitempty"`
		Relationship *string `json:"relationship,omitempty"`
	}{
		Type:         reference.Type,
		ID:           optionalString(reference.ID, reference.idPresent),
		LID:          optionalString(reference.LID, reference.lidPresent),
		Relationship: optionalString(reference.Relationship, reference.relPresent),
	})
}

// MarshalJSON preserves an explicitly empty result meta object.
func (result AtomicResult) MarshalJSON() ([]byte, error) {
	var meta *Meta
	if result.Meta != nil {
		meta = &result.Meta
	}

	return json.Marshal(struct {
		Data *PrimaryData `json:"data,omitempty"`
		Meta *Meta        `json:"meta,omitempty"`
	}{Data: result.Data, Meta: meta})
}

func decodeAtomicOperations(raw json.RawMessage, path string) ([]AtomicOperation, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, decodeFailure(path, "type", "operations must be an array", err)
	}

	operations := make([]AtomicOperation, len(items))
	for index, item := range items {
		operation, err := decodeAtomicOperation(item, path+"/"+strconv.Itoa(index))
		if err != nil {
			return nil, err
		}
		operations[index] = operation
	}

	return operations, nil
}

func decodeAtomicOperation(raw json.RawMessage, path string) (AtomicOperation, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return AtomicOperation{}, err
	}
	if err := rejectUnknown(object, path, "op", "ref", "href", "data", "meta"); err != nil {
		return AtomicOperation{}, err
	}

	var operation AtomicOperation
	if value, exists := object["op"]; exists {
		var code string
		if err := decodeString(value, path+"/op", &code); err != nil {
			return AtomicOperation{}, err
		}
		operation.Op = AtomicOperationCode(code)
	}
	if value, exists := object["ref"]; exists {
		reference, decodeErr := decodeAtomicReference(value, path+"/ref")
		if decodeErr != nil {
			return AtomicOperation{}, decodeErr
		}
		operation.Ref = &reference
	}
	if value, exists := object["href"]; exists {
		if err := decodeString(value, path+"/href", &operation.Href); err != nil {
			return AtomicOperation{}, err
		}
		operation.hrefPresent = true
	}
	if value, exists := object["data"]; exists {
		data, decodeErr := decodePrimaryData(value, path+"/data")
		if decodeErr != nil {
			return AtomicOperation{}, decodeErr
		}
		operation.Data = data
	}
	if value, exists := object["meta"]; exists {
		meta, decodeErr := decodeMeta(value, path+"/meta")
		if decodeErr != nil {
			return AtomicOperation{}, decodeErr
		}
		operation.Meta = meta
	}

	return operation, nil
}

func decodeAtomicReference(raw json.RawMessage, path string) (AtomicReference, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return AtomicReference{}, err
	}
	if err := rejectUnknown(object, path, "type", "id", "lid", "relationship"); err != nil {
		return AtomicReference{}, err
	}

	var reference AtomicReference
	fields := []struct {
		name   string
		target *string
	}{
		{"type", &reference.Type},
		{"id", &reference.ID},
		{"lid", &reference.LID},
		{"relationship", &reference.Relationship},
	}
	for _, field := range fields {
		name := field.name
		if value, exists := object[name]; exists {
			if err := decodeString(value, path+"/"+name, field.target); err != nil {
				return AtomicReference{}, err
			}
			switch name {
			case "id":
				reference.idPresent = true
			case "lid":
				reference.lidPresent = true
			case "relationship":
				reference.relPresent = true
			}
		}
	}

	return reference, nil
}

func decodeAtomicResults(raw json.RawMessage, path string) ([]AtomicResult, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, decodeFailure(path, "type", "results must be an array", err)
	}

	results := make([]AtomicResult, len(items))
	for index, item := range items {
		result, err := decodeAtomicResult(item, path+"/"+strconv.Itoa(index))
		if err != nil {
			return nil, err
		}
		results[index] = result
	}

	return results, nil
}

func decodeAtomicResult(raw json.RawMessage, path string) (AtomicResult, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return AtomicResult{}, err
	}
	if err := rejectUnknown(object, path, "data", "meta"); err != nil {
		return AtomicResult{}, err
	}

	var result AtomicResult
	if value, exists := object["data"]; exists {
		data, decodeErr := decodePrimaryData(value, path+"/data")
		if decodeErr != nil {
			return AtomicResult{}, decodeErr
		}
		result.Data = data
	}
	if value, exists := object["meta"]; exists {
		meta, decodeErr := decodeMeta(value, path+"/meta")
		if decodeErr != nil {
			return AtomicResult{}, decodeErr
		}
		result.Meta = meta
	}

	return result, nil
}
