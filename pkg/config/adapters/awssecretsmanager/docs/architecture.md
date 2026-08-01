# Architecture

The adapter is a leaf module between the AWS SDK and `golib/config`. It owns no
credentials, client, retry loop, cache, refresh goroutine, or global state.

Each `Load` checks cancellation, performs one version-selective provider read,
copies and bounds exactly one payload, parses it with the strict golib JSON
source, and returns a sensitive document. AWS SDK configuration and client
lifetime remain composition-root responsibilities. Typed decoding and source
precedence remain responsibilities of the enclosing `config.Plan`.
