// Package awskms adapts AWS KMS data-key operations to secret-envelope key
// wrapping and authenticates bounded externally signed raw statements with
// asymmetric KMS keys.
//
// Encryption context values are visible to AWS and CloudTrail. They must
// remain non-secret.
package awskms
