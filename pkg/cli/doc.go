// Package cli builds explicit, typed, composable command-line applications.
//
// Applications construct commands in their composition root, compile immutable
// metadata, and execute already-tokenized argv with caller-owned context and IO.
// Cobra is an internal parsing engine and does not appear in public contracts.
package cli
