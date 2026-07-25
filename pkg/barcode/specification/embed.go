// Package specification exposes pinned, redistributable standards artifacts.
package specification

import _ "embed"

//go:embed gs1/gs1-syntax-dictionary.txt
var gs1SyntaxDictionary string

// GS1SyntaxDictionary returns the pinned GS1 syntax dictionary bytes.
func GS1SyntaxDictionary() string { return gs1SyntaxDictionary }
