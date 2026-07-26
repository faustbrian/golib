# Extensions

Foreign namespace attributes and elements are preserved on extensible WSDL
components. Expanded names avoid prefix confusion. Element payload bytes are
bounded by document, text, element, depth, and extension-count limits.

WSDL-required flags retain both boolean value and lexical presence. Validation
fails a required extension unless its QName appears in
`ValidationOptions.UnderstoodExtensions`. Listing a QName means the caller has
validated its semantics; core only records that declaration.

WS-* vocabularies are ordinary unknown extensions until a separately scoped
package and support matrix explicitly implements them.
