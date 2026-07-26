// Package secretenvelope provides authenticated envelope encryption for
// application-owned secret payloads.
//
// Encryption contexts are non-secret associated data. They must use stable
// identifiers and field labels, never credentials or customer payloads.
package secretenvelope
