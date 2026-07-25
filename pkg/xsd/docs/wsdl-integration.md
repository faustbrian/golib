# WSDL integration

WSDL tooling should compile embedded and imported schemas once, retain the
immutable `compile.Set`, and query declarations by expanded QName. It should
not duplicate chameleon include, group expansion, datatype, or substitution
logic.

Provide WSDL and XSD resources through one application-owned resolver so base
URI and access policy stay consistent. Do not infer namespace prefixes from
compiled QNames; prefixes are serialization details, while component identity
is the namespace URI plus local name.

The set accessors expose global elements, attributes, simple and complex types,
model groups, attribute groups, notation declarations, and substitution
membership. Corresponding `*Names` methods provide deterministic expanded-name
inventories so generators do not need access to mutable compiler maps.
`compile/wsdl_contract_test.go` fixes this public consumer surface as an
executable contract. Future `wsdl` requirements must extend that contract
and the requirement matrix instead of reaching into compiler internals.
