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
