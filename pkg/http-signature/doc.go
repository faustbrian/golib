// Package httpsignature implements RFC 9421 HTTP Message Signatures and RFC
// 9530 digest fields for net/http.
//
// Parsing and cryptographic validity do not authenticate or authorize an
// operation by themselves. Applications must construct explicit signing and
// verification profiles, resolve algorithm-bound keys, enforce replay policy,
// supply trusted external request context behind proxies, and map safe typed
// failures to their own protocol.
package httpsignature
