package datatype

var builtInBases = map[string]string{
	"string": "anySimpleType", "boolean": "anySimpleType", "decimal": "anySimpleType",
	"float": "anySimpleType", "double": "anySimpleType", "duration": "anySimpleType",
	"dateTime": "anySimpleType", "time": "anySimpleType", "date": "anySimpleType",
	"gYearMonth": "anySimpleType", "gYear": "anySimpleType", "gMonthDay": "anySimpleType",
	"gDay": "anySimpleType", "gMonth": "anySimpleType", "hexBinary": "anySimpleType",
	"base64Binary": "anySimpleType", "anyURI": "anySimpleType", "QName": "anySimpleType",
	"NOTATION": "anySimpleType", "normalizedString": "string", "token": "normalizedString",
	"language": "token", "Name": "token", "NCName": "Name", "ID": "NCName",
	"IDREF": "NCName", "ENTITY": "NCName", "NMTOKEN": "token",
	"integer": "decimal", "nonPositiveInteger": "integer", "negativeInteger": "nonPositiveInteger",
	"long": "integer", "int": "long", "short": "int", "byte": "short",
	"nonNegativeInteger": "integer", "unsignedLong": "nonNegativeInteger",
	"unsignedInt": "unsignedLong", "unsignedShort": "unsignedInt", "unsignedByte": "unsignedShort",
	"positiveInteger": "nonNegativeInteger",
	"IDREFS":          "anySimpleType", "ENTITIES": "anySimpleType", "NMTOKENS": "anySimpleType",
}

// BuiltInBase returns the immediate XSD 1.0 base of a built-in simple type.
func BuiltInBase(local string) (string, bool) {
	base, ok := builtInBases[local]
	return base, ok
}

// BuiltInDerivation returns the immediate base and derivation method.
func BuiltInDerivation(local string) (string, string, bool) {
	base, ok := builtInBases[local]
	if !ok {
		return "", "", false
	}
	switch local {
	case "IDREFS", "ENTITIES", "NMTOKENS":
		return base, "list", true
	default:
		return base, "restriction", true
	}
}
