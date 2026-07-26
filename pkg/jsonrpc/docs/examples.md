# Examples

## HTTP Server

Create a dispatcher, register methods with explicit request and result types,
then expose it through the HTTP transport. Keep authentication, authorization,
request-size limits, and application dependency wiring outside protocol
handlers.

See the runnable server and client programs under [examples](../examples) and
the [quickstart](quickstart.md) for the smallest complete setup.

## Notifications

Notifications intentionally produce no JSON-RPC response. Handlers must still
record failures through application observability because the caller cannot
receive a protocol error.

## Batch Requests

Apply a total batch-size limit before dispatch. Decide whether independent
calls may execute concurrently and make that policy explicit. Preserve input
order when collecting response-bearing results.

## Client Correlation

Use the client API to allocate IDs and correlate out-of-order responses.
Applications must set transport deadlines and decide whether a transport retry
is safe for the invoked method.
