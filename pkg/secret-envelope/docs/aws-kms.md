# AWS KMS operations

The adapter uses `GenerateDataKey` with `AES_256` and `Decrypt` with
`SYMMETRIC_DEFAULT`. Both calls include the exact case-sensitive encryption
context. The generated plaintext key is used only for local AES-GCM and then
best-effort zeroized; the wrapped key is stored with the ciphertext.

KMS returns the resolved key ARN. Persist that ARN rather than the input alias
so later alias rotation does not make historical ciphertext ambiguous.

Applications construct the SDK client from `config.LoadDefaultConfig`. This
uses the AWS SDK default credential chain, including web identity in
Kubernetes. The module does not accept or store static AWS credentials.

## Asymmetric signature verification

`NewSignatureVerifier` exposes only `kms:Verify`; it does not expose `Sign`.
Each verifier fixes one reviewed algorithm. `Verify` sends the exact copied
message with `MessageType=RAW`, the explicit key reference, and the copied
signature. Raw messages are bounded to AWS KMS's 4096-byte limit.

The adapter accepts RSASSA-PSS, ECDSA, and non-prehashed Ed25519 algorithms at
SHA-256 strength or higher. It rejects PKCS#1 v1.5, digest mode, SM2, ML-DSA,
prehashed Ed25519, implicit algorithms, empty resolved keys, algorithm drift,
and false verification responses.

KMS signature verification is logged in CloudTrail. IAM should grant only
`kms:Verify` on the exact asymmetric signing-key ARNs required by the
application. Authorization of a verified signer or key for an application role
remains an application policy decision.
