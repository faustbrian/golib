package openrpc

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestSerializationStandardFieldCollisions(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func() error{
		"info title": func() error { _, err := infoObject(Info{objectFields: collisionObjectFields(t, "title")}); return err },
		"info version": func() error {
			_, err := infoObject(Info{objectFields: collisionObjectFields(t, "version")})
			return err
		},
		"info description": func() error {
			_, err := infoObject(Info{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"info terms": func() error {
			_, err := infoObject(Info{objectFields: collisionObjectFields(t, "termsOfService"), termsOfService: optionalString{present: true}})
			return err
		},
		"info contact": func() error {
			_, err := infoObject(Info{objectFields: collisionObjectFields(t, "contact"), hasContact: true})
			return err
		},
		"info license": func() error {
			_, err := infoObject(Info{objectFields: collisionObjectFields(t, "license"), hasLicense: true})
			return err
		},
		"contact name": func() error {
			_, err := contactObject(Contact{objectFields: collisionObjectFields(t, "name"), name: optionalString{present: true}})
			return err
		},
		"contact email": func() error {
			_, err := contactObject(Contact{objectFields: collisionObjectFields(t, "email"), email: optionalString{present: true}})
			return err
		},
		"contact URL": func() error {
			_, err := contactObject(Contact{objectFields: collisionObjectFields(t, "url"), url: optionalString{present: true}})
			return err
		},
		"license name": func() error {
			_, err := licenseObject(License{objectFields: collisionObjectFields(t, "name"), name: optionalString{present: true}})
			return err
		},
		"license URL": func() error {
			_, err := licenseObject(License{objectFields: collisionObjectFields(t, "url"), url: optionalString{present: true}})
			return err
		},
		"docs URL": func() error {
			_, err := externalDocumentationObject(ExternalDocumentation{objectFields: collisionObjectFields(t, "url")})
			return err
		},
		"docs description": func() error {
			_, err := externalDocumentationObject(ExternalDocumentation{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"server URL": func() error {
			_, err := serverObject(Server{objectFields: collisionObjectFields(t, "url")})
			return err
		},
		"server name": func() error {
			_, err := serverObject(Server{objectFields: collisionObjectFields(t, "name"), name: optionalString{present: true}})
			return err
		},
		"server description": func() error {
			_, err := serverObject(Server{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"server summary": func() error {
			_, err := serverObject(Server{objectFields: collisionObjectFields(t, "summary"), summary: optionalString{present: true}})
			return err
		},
		"server variables": func() error {
			_, err := serverObject(Server{objectFields: collisionObjectFields(t, "variables"), hasVariables: true})
			return err
		},
		"variable default": func() error {
			_, err := serverVariableObject(ServerVariable{unknown: collisionFields(t, "default")})
			return err
		},
		"variable description": func() error {
			_, err := serverVariableObject(ServerVariable{unknown: collisionFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"variable enum": func() error {
			_, err := serverVariableObject(ServerVariable{unknown: collisionFields(t, "enum"), hasEnum: true})
			return err
		},
		"method name": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "name")})
			return err
		},
		"method description": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"method summary": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "summary"), summary: optionalString{present: true}})
			return err
		},
		"method servers": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "servers"), hasServers: true})
			return err
		},
		"method tags": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "tags"), hasTags: true})
			return err
		},
		"method params": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "params")})
			return err
		},
		"method structure": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "paramStructure"), hasStructure: true})
			return err
		},
		"method result": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "result"), hasResult: true, result: ContentDescriptorValue(ContentDescriptor{})})
			return err
		},
		"method errors": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "errors"), hasErrors: true})
			return err
		},
		"method links": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "links"), hasLinks: true})
			return err
		},
		"method examples": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "examples"), hasExamples: true})
			return err
		},
		"method deprecated": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "deprecated"), deprecated: optionalBool{present: true}})
			return err
		},
		"method docs": func() error {
			_, err := methodObject(Method{objectFields: collisionObjectFields(t, "externalDocs"), hasDocs: true})
			return err
		},
		"descriptor name": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "name")})
			return err
		},
		"descriptor description": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"descriptor summary": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "summary"), summary: optionalString{present: true}})
			return err
		},
		"descriptor schema": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "schema")})
			return err
		},
		"descriptor required": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "required"), required: optionalBool{present: true}})
			return err
		},
		"descriptor deprecated": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: collisionObjectFields(t, "deprecated"), deprecated: optionalBool{present: true}})
			return err
		},
		"tag name": func() error { _, err := tagObject(Tag{objectFields: collisionObjectFields(t, "name")}); return err },
		"tag description": func() error {
			_, err := tagObject(Tag{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"tag docs": func() error {
			_, err := tagObject(Tag{objectFields: collisionObjectFields(t, "externalDocs"), hasDocs: true})
			return err
		},
		"error code": func() error { _, err := errorObject(Error{objectFields: collisionObjectFields(t, "code")}); return err },
		"error message": func() error {
			_, err := errorObject(Error{objectFields: collisionObjectFields(t, "message")})
			return err
		},
		"error data": func() error {
			_, err := errorObject(Error{objectFields: collisionObjectFields(t, "data"), data: optionalValue{present: true}})
			return err
		},
		"link name": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "name"), name: optionalValue{present: true}})
			return err
		},
		"link summary": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "summary"), summary: optionalString{present: true}})
			return err
		},
		"link description": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"link method": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "method"), method: optionalString{present: true}})
			return err
		},
		"link params": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "params"), params: optionalValue{present: true}})
			return err
		},
		"link server": func() error {
			_, err := linkObject(Link{objectFields: collisionObjectFields(t, "server"), hasServer: true})
			return err
		},
		"pairing name": func() error {
			_, err := examplePairingObject(ExamplePairing{unknown: collisionFields(t, "name")})
			return err
		},
		"pairing description": func() error {
			_, err := examplePairingObject(ExamplePairing{unknown: collisionFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"pairing params": func() error {
			_, err := examplePairingObject(ExamplePairing{unknown: collisionFields(t, "params")})
			return err
		},
		"pairing result": func() error {
			_, err := examplePairingObject(ExamplePairing{unknown: collisionFields(t, "result"), hasResult: true, result: ExampleReference(Reference{})})
			return err
		},
		"example name": func() error {
			_, err := exampleObject(Example{objectFields: collisionObjectFields(t, "name")})
			return err
		},
		"example summary": func() error {
			_, err := exampleObject(Example{objectFields: collisionObjectFields(t, "summary"), summary: optionalString{present: true}})
			return err
		},
		"example description": func() error {
			_, err := exampleObject(Example{objectFields: collisionObjectFields(t, "description"), description: optionalString{present: true}})
			return err
		},
		"example value": func() error {
			_, err := exampleObject(Example{objectFields: collisionObjectFields(t, "value")})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrFieldCollision) {
			t.Errorf("%s error = %v", name, err)
		}
	}
}

func TestSerializationUnionAndNestedFailures(t *testing.T) {
	t.Parallel()

	reference := Reference{ref: "#/value"}
	for name, call := range map[string]func() error{
		"method zero":      func() error { _, err := methodUnionObject(MethodOrReference{}); return err },
		"method array":     func() error { _, err := methodUnionObjects([]MethodOrReference{{}}); return err },
		"descriptor zero":  func() error { _, err := descriptorUnionObject(ContentDescriptorOrReference{}); return err },
		"descriptor array": func() error { _, err := descriptorUnionObjects([]ContentDescriptorOrReference{{}}); return err },
		"tag zero":         func() error { _, err := tagUnionObjects([]TagOrReference{{}}); return err },
		"error zero":       func() error { _, err := errorUnionObjects([]ErrorOrReference{{}}); return err },
		"link zero":        func() error { _, err := linkUnionObjects([]LinkOrReference{{}}); return err },
		"pairing zero":     func() error { _, err := pairingUnionObjects([]ExamplePairingOrReference{{}}); return err },
		"example zero":     func() error { _, err := exampleUnionObject(ExampleOrReference{}); return err },
		"example array":    func() error { _, err := exampleUnionObjects([]ExampleOrReference{{}}); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidUnion) {
			t.Errorf("%s error = %v", name, err)
		}
	}
	if _, err := methodUnionObject(MethodOrReference{kind: 2, reference: reference}); err != nil {
		t.Fatal(err)
	}
	if _, err := descriptorUnionObject(ContentDescriptorOrReference{kind: 2, reference: reference}); err != nil {
		t.Fatal(err)
	}
	if _, err := exampleUnionObject(ExampleOrReference{kind: 2, reference: reference}); err != nil {
		t.Fatal(err)
	}
	if _, err := tagUnionObjects([]TagOrReference{{kind: 2, reference: reference}}); err != nil {
		t.Fatal(err)
	}
	if _, err := errorUnionObjects([]ErrorOrReference{{kind: 2, reference: reference}}); err != nil {
		t.Fatal(err)
	}
	if _, err := linkUnionObjects([]LinkOrReference{{kind: 2, reference: reference}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pairingUnionObjects([]ExamplePairingOrReference{{kind: 2, reference: reference}}); err != nil {
		t.Fatal(err)
	}

	for name, call := range map[string]func() error{
		"info contact child": func() error {
			_, err := infoObject(Info{hasContact: true, contact: Contact{objectFields: collisionObjectFields(t, "name"), name: optionalString{present: true}}})
			return err
		},
		"info license child": func() error {
			_, err := infoObject(Info{hasLicense: true, license: License{objectFields: collisionObjectFields(t, "name"), name: optionalString{present: true}}})
			return err
		},
		"server array child": func() error {
			_, err := serverObjects([]Server{{objectFields: collisionObjectFields(t, "url")}})
			return err
		},
		"server variable child": func() error {
			_, err := serverObject(Server{hasVariables: true, variables: map[string]ServerVariable{"x": {unknown: collisionFields(t, "default")}}})
			return err
		},
		"method server child": func() error {
			_, err := methodObject(Method{hasServers: true, servers: []Server{{objectFields: collisionObjectFields(t, "url")}}})
			return err
		},
		"method tag child":    func() error { _, err := methodObject(Method{hasTags: true, tags: []TagOrReference{{}}}); return err },
		"method param child":  func() error { _, err := methodObject(Method{params: []ContentDescriptorOrReference{{}}}); return err },
		"method result child": func() error { _, err := methodObject(Method{hasResult: true}); return err },
		"method error child": func() error {
			_, err := methodObject(Method{hasErrors: true, errors: []ErrorOrReference{{}}})
			return err
		},
		"method link child": func() error { _, err := methodObject(Method{hasLinks: true, links: []LinkOrReference{{}}}); return err },
		"method example child": func() error {
			_, err := methodObject(Method{hasExamples: true, examples: []ExamplePairingOrReference{{}}})
			return err
		},
		"method docs child": func() error {
			_, err := methodObject(Method{hasDocs: true, externalDocs: ExternalDocumentation{objectFields: collisionObjectFields(t, "url")}})
			return err
		},
		"tag docs child": func() error {
			_, err := tagObject(Tag{hasDocs: true, externalDocs: ExternalDocumentation{objectFields: collisionObjectFields(t, "url")}})
			return err
		},
		"link server child": func() error {
			_, err := linkObject(Link{hasServer: true, server: Server{objectFields: collisionObjectFields(t, "url")}})
			return err
		},
		"pairing param child": func() error {
			_, err := examplePairingObject(ExamplePairing{params: []ExampleOrReference{{}}})
			return err
		},
		"pairing result child": func() error { _, err := examplePairingObject(ExamplePairing{hasResult: true}); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s succeeded", name)
		}
	}
	if _, err := extensibleObject(objectFields{extensions: duplicateFields(t)}); !errors.Is(err, ErrFieldCollision) {
		t.Fatalf("extension field collision error = %v", err)
	}
}

func TestSerializationComponentsAndFieldHelpers(t *testing.T) {
	t.Parallel()

	components := Components{
		hasSchemas: true, schemas: map[string]jsonschema.Schema{},
		hasLinks: true, links: map[string]Link{},
		hasErrors: true, errors: map[string]Error{},
		hasExamples: true, examples: map[string]Example{},
		hasExamplePairings: true, examplePairings: map[string]ExamplePairing{},
		hasDescriptors: true, contentDescriptors: map[string]ContentDescriptor{},
		hasTags: true, tags: map[string]Tag{},
	}
	if object, err := componentsObject(components); err != nil || len(object) != 7 {
		t.Fatalf("components object = %#v, %v", object, err)
	}
	for name, call := range map[string]func() error{
		"schemas collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "schemas"), hasSchemas: true})
			return err
		},
		"links collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "links"), hasLinks: true})
			return err
		},
		"errors collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "errors"), hasErrors: true})
			return err
		},
		"examples collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "examples"), hasExamples: true})
			return err
		},
		"pairings collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "examplePairings"), hasExamplePairings: true})
			return err
		},
		"descriptors collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "contentDescriptors"), hasDescriptors: true})
			return err
		},
		"tags collision": func() error {
			_, err := componentsObject(Components{unknown: collisionFields(t, "tags"), hasTags: true})
			return err
		},
		"link child": func() error {
			_, err := componentsObject(Components{hasLinks: true, links: map[string]Link{"x": {objectFields: collisionObjectFields(t, "name"), name: optionalValue{present: true}}}})
			return err
		},
		"error child": func() error {
			_, err := componentsObject(Components{hasErrors: true, errors: map[string]Error{"x": {objectFields: collisionObjectFields(t, "code")}}})
			return err
		},
		"example child": func() error {
			_, err := componentsObject(Components{hasExamples: true, examples: map[string]Example{"x": {objectFields: collisionObjectFields(t, "name")}}})
			return err
		},
		"pairing child": func() error {
			_, err := componentsObject(Components{hasExamplePairings: true, examplePairings: map[string]ExamplePairing{"x": {unknown: collisionFields(t, "name")}}})
			return err
		},
		"descriptor child": func() error {
			_, err := componentsObject(Components{hasDescriptors: true, contentDescriptors: map[string]ContentDescriptor{"x": {objectFields: collisionObjectFields(t, "name")}}})
			return err
		},
		"tag child": func() error {
			_, err := componentsObject(Components{hasTags: true, tags: map[string]Tag{"x": {objectFields: collisionObjectFields(t, "name")}}})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s succeeded", name)
		}
	}
	if _, err := mapObjects(map[string]int{"x": 1}, func(int) (map[string]any, error) { return nil, ErrInvalidUnion }); !errors.Is(err, ErrInvalidUnion) {
		t.Fatalf("map conversion error = %v", err)
	}
	if err := mergeFields(map[string]any{}, Fields{names: []string{"x", "x"}, values: map[string]jsonvalue.Value{"x": mustRootJSON(t, `true`)}}); !errors.Is(err, ErrFieldCollision) {
		t.Fatalf("merge collision error = %v", err)
	}
	if err := putOptionalString(map[string]any{"x": true}, "x", optionalString{present: true}); !errors.Is(err, ErrFieldCollision) {
		t.Fatalf("optional string collision error = %v", err)
	}
	if err := putOptionalValue(map[string]any{"x": true}, "x", optionalValue{present: true}); !errors.Is(err, ErrFieldCollision) {
		t.Fatalf("optional value collision error = %v", err)
	}
}

func TestSerializationInitialAndDocumentFailures(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func() error{
		"info":    func() error { _, err := infoObject(Info{objectFields: invalidObjectFields(t)}); return err },
		"contact": func() error { _, err := contactObject(Contact{objectFields: invalidObjectFields(t)}); return err },
		"license": func() error { _, err := licenseObject(License{objectFields: invalidObjectFields(t)}); return err },
		"docs": func() error {
			_, err := externalDocumentationObject(ExternalDocumentation{objectFields: invalidObjectFields(t)})
			return err
		},
		"server":   func() error { _, err := serverObject(Server{objectFields: invalidObjectFields(t)}); return err },
		"variable": func() error { _, err := serverVariableObject(ServerVariable{unknown: duplicateFields(t)}); return err },
		"method":   func() error { _, err := methodObject(Method{objectFields: invalidObjectFields(t)}); return err },
		"descriptor": func() error {
			_, err := contentDescriptorObject(ContentDescriptor{objectFields: invalidObjectFields(t)})
			return err
		},
		"tag":        func() error { _, err := tagObject(Tag{objectFields: invalidObjectFields(t)}); return err },
		"error":      func() error { _, err := errorObject(Error{objectFields: invalidObjectFields(t)}); return err },
		"link":       func() error { _, err := linkObject(Link{objectFields: invalidObjectFields(t)}); return err },
		"pairing":    func() error { _, err := examplePairingObject(ExamplePairing{unknown: duplicateFields(t)}); return err },
		"example":    func() error { _, err := exampleObject(Example{objectFields: invalidObjectFields(t)}); return err },
		"components": func() error { _, err := componentsObject(Components{unknown: duplicateFields(t)}); return err },
		"fields":     func() error { _, err := fieldsObject(duplicateFields(t)); return err },
	} {
		if err := call(); !errors.Is(err, ErrFieldCollision) {
			t.Errorf("%s initial error = %v", name, err)
		}
	}

	for name, call := range map[string]func() error{
		"tag inline": func() error {
			_, err := tagUnionObjects([]TagOrReference{{kind: 1, tag: Tag{objectFields: invalidObjectFields(t)}}})
			return err
		},
		"error inline": func() error {
			_, err := errorUnionObjects([]ErrorOrReference{{kind: 1, object: Error{objectFields: invalidObjectFields(t)}}})
			return err
		},
		"link inline": func() error {
			_, err := linkUnionObjects([]LinkOrReference{{kind: 1, link: Link{objectFields: invalidObjectFields(t)}}})
			return err
		},
		"pairing inline": func() error {
			_, err := pairingUnionObjects([]ExamplePairingOrReference{{kind: 1, pairing: ExamplePairing{unknown: duplicateFields(t)}}})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrFieldCollision) {
			t.Errorf("%s child error = %v", name, err)
		}
	}

	base := Document{
		version: Version{value: "1.4.1"},
		info:    Info{title: "x", version: "1"},
		methods: []MethodOrReference{},
	}
	for name, mutate := range map[string]func(*Document){
		"initial": func(document *Document) { document.objectFields = invalidObjectFields(t) },
		"openrpc": func(document *Document) { document.objectFields = collisionObjectFields(t, "openrpc") },
		"schema": func(document *Document) {
			document.objectFields = collisionObjectFields(t, "$schema")
			document.schemaURI = optionalString{present: true}
		},
		"info child": func(document *Document) { document.info.objectFields = invalidObjectFields(t) },
		"info field": func(document *Document) { document.objectFields = collisionObjectFields(t, "info") },
		"docs child": func(document *Document) {
			document.hasDocs = true
			document.externalDocs.objectFields = invalidObjectFields(t)
		},
		"docs field": func(document *Document) {
			document.objectFields = collisionObjectFields(t, "externalDocs")
			document.hasDocs = true
		},
		"servers child": func(document *Document) {
			document.hasServers = true
			document.servers = []Server{{objectFields: invalidObjectFields(t)}}
		},
		"servers field": func(document *Document) {
			document.objectFields = collisionObjectFields(t, "servers")
			document.hasServers = true
		},
		"methods child": func(document *Document) { document.methods = []MethodOrReference{{}} },
		"methods field": func(document *Document) { document.objectFields = collisionObjectFields(t, "methods") },
		"components child": func(document *Document) {
			document.hasComponents = true
			document.components.unknown = duplicateFields(t)
		},
		"components field": func(document *Document) {
			document.objectFields = collisionObjectFields(t, "components")
			document.hasComponents = true
		},
	} {
		document := base
		mutate(&document)
		if _, err := documentObject(document); err == nil {
			t.Errorf("document %s succeeded", name)
		}
	}
	if _, err := MarshalCanonical(Document{version: Version{value: "1.4.1"}}); !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("missing info error = %v", err)
	}
	encodingFailure := base
	encodingFailure.methods = []MethodOrReference{{kind: 1, method: Method{
		name: "x", params: []ContentDescriptorOrReference{{kind: 1, descriptor: ContentDescriptor{name: "x"}}},
	}}}
	if _, err := MarshalCanonical(encodingFailure); err == nil {
		t.Fatal("invalid encoded schema succeeded")
	}
}

func TestRequiredConstructorsAndVersionZeroPaths(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func() error{
		"info version":      func() error { _, err := NewInfo(InfoInput{Title: "x"}); return err },
		"descriptor name":   func() error { _, err := NewContentDescriptor(ContentDescriptorInput{}); return err },
		"descriptor schema": func() error { _, err := NewContentDescriptor(ContentDescriptorInput{Name: "x"}); return err },
		"tag name":          func() error { _, err := NewTag(TagInput{}); return err },
		"error code":        func() error { _, err := NewError(ErrorInput{}); return err },
		"error message": func() error {
			integer, err := ParseInteger("1")
			if err != nil {
				return err
			}
			_, err = NewError(ErrorInput{Code: integer})
			return err
		},
		"example name":   func() error { _, err := NewExample(ExampleInput{}); return err },
		"example value":  func() error { _, err := NewExample(ExampleInput{Name: "x"}); return err },
		"pairing name":   func() error { _, err := NewExamplePairing(ExamplePairingInput{}); return err },
		"pairing params": func() error { _, err := NewExamplePairing(ExamplePairingInput{Name: "x"}); return err },
		"method name":    func() error { _, err := NewMethod(MethodInput{}); return err },
		"method params":  func() error { _, err := NewMethod(MethodInput{Name: "x"}); return err },
	} {
		if err := call(); !errors.Is(err, ErrMissingRequiredField) {
			t.Errorf("%s error = %v", name, err)
		}
	}
	invalidStructure := ParamStructure("invalid")
	if _, err := NewMethod(MethodInput{Name: "x", Params: []ContentDescriptorOrReference{}, ParamStructure: &invalidStructure}); !errors.Is(err, ErrInvalidParamStructure) {
		t.Fatalf("invalid parameter structure error = %v", err)
	}
	if (Version{}).FeatureSet() != "" {
		t.Fatal("zero version has a feature set")
	}
	if _, err := NewUnknownFields(Field{Name: "", Value: mustRootJSON(t, `true`)}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("empty field name error = %v", err)
	}
}

func TestConstructorPresenceMutationBoundaries(t *testing.T) {
	t.Parallel()

	defaultValue := "x"
	variable, err := NewServerVariable(ServerVariableInput{Default: &defaultValue})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := variable.Enum(); present {
		t.Fatal("nil server enum is present")
	}
	variable, err = NewServerVariable(ServerVariableInput{Default: &defaultValue, Enum: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if values, present := variable.Enum(); !present || len(values) != 0 {
		t.Fatalf("empty server enum = %#v, %t", values, present)
	}
	server, err := NewServer(ServerInput{URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := server.Variables(); present {
		t.Fatal("nil server variables are present")
	}
	server, err = NewServer(ServerInput{URL: "https://example.com", Variables: map[string]ServerVariable{}})
	if err != nil {
		t.Fatal(err)
	}
	if values, present := server.Variables(); !present || len(values) != 0 {
		t.Fatalf("empty server variables = %#v, %t", values, present)
	}

	method, err := NewMethod(MethodInput{Name: "x", Params: []ContentDescriptorOrReference{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := method.Servers(); present {
		t.Fatal("nil method servers are present")
	}
	if _, present := method.Tags(); present {
		t.Fatal("nil method tags are present")
	}
	if _, present := method.Errors(); present {
		t.Fatal("nil method errors are present")
	}
	if _, present := method.Links(); present {
		t.Fatal("nil method links are present")
	}
	if _, present := method.Examples(); present {
		t.Fatal("nil method examples are present")
	}
	method, err = NewMethod(MethodInput{
		Name: "x", Params: []ContentDescriptorOrReference{}, Servers: []Server{},
		Tags: []TagOrReference{}, Errors: []ErrorOrReference{}, Links: []LinkOrReference{},
		Examples: []ExamplePairingOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if values, present := method.Servers(); !present || len(values) != 0 {
		t.Fatalf("empty method servers = %#v, %t", values, present)
	}
	if values, present := method.Tags(); !present || len(values) != 0 {
		t.Fatalf("empty method tags = %#v, %t", values, present)
	}
	if values, present := method.Errors(); !present || len(values) != 0 {
		t.Fatalf("empty method errors = %#v, %t", values, present)
	}
	if values, present := method.Links(); !present || len(values) != 0 {
		t.Fatalf("empty method links = %#v, %t", values, present)
	}
	if values, present := method.Examples(); !present || len(values) != 0 {
		t.Fatalf("empty method examples = %#v, %t", values, present)
	}
}

func collisionObjectFields(t *testing.T, name string) objectFields {
	t.Helper()
	return objectFields{unknown: collisionFields(t, name)}
}

func collisionFields(t *testing.T, name string) Fields {
	t.Helper()
	return Fields{names: []string{name}, values: map[string]jsonvalue.Value{name: mustRootJSON(t, `true`)}}
}

func invalidObjectFields(t *testing.T) objectFields {
	t.Helper()
	return objectFields{extensions: collisionFields(t, "x-a"), unknown: collisionFields(t, "x-a")}
}

func duplicateFields(t *testing.T) Fields {
	t.Helper()
	value := mustRootJSON(t, `true`)
	return Fields{names: []string{"x", "x"}, values: map[string]jsonvalue.Value{"x": value}}
}

func mustRootJSON(t *testing.T, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
