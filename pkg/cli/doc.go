// Package cli builds explicit, typed, composable command-line applications.
//
// Applications construct commands in their composition root, compile immutable
// metadata, and execute already-tokenized argv with caller-owned context and IO.
// Bounded one-binary service processes can compile a direct CommandSet without
// linking features they do not publish.
// The dependency-free parser is internal and does not appear in public
// contracts.
package cli
