# Models and source locations

`Document` contains exactly one `Definitions11` or `Description20`. Versioned
types expose the corresponding component vocabulary instead of flattening
incompatible WSDL versions into one lossy structure.

`Location` records system identifier, line, column, and byte offset. Parsed
components, documentation, extension data, and binding adjuncts retain source
locations used by diagnostics. `QName` stores expanded namespace and local
name; lexical prefixes are resolved during parsing and regenerated
deterministically.

Presence flags distinguish an absent attribute from an explicitly supplied
default or empty value. Extension attributes are expanded names. Extension
elements retain a bounded XML payload, required-presence state, and location.
WSDL 2.0 predefined operation extensions expose typed `wsdlx:safe` presence
and ordered `wrpc:signature` QName/direction pairs rather than leaving them in
the generic extension bag.

Construct documents with `NewDocument11` or `NewDocument20` when starting from
Go values. Constructors validate the caller model, marshal and reparse it to
take ownership, then validate the canonical result.
