//go:build integration && race

package kafka_test

// Race instrumentation runs one broker fixture at a time to keep Docker and
// the instrumented test process within their startup and cleanup deadlines.
const integrationBrokerConcurrency = 1
