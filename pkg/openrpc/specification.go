package openrpc

import _ "embed"

//go:embed specification/openrpc-1.4.1/schema.json
var embeddedMetaSchema []byte

//go:embed specification/openrpc-1.4.1/json-schema-tools.json
var embeddedJSONSchemaToolsMetaSchema []byte

// MetaSchema returns an owned copy of the authoritative pinned OpenRPC 1.4.1
// Draft 7 meta-schema.
func MetaSchema() []byte {
	return append([]byte(nil), embeddedMetaSchema...)
}

// JSONSchemaToolsMetaSchema returns an owned copy of the companion meta-schema
// referenced by the authoritative OpenRPC 1.4.1 schema.
func JSONSchemaToolsMetaSchema() []byte {
	return append([]byte(nil), embeddedJSONSchemaToolsMetaSchema...)
}
