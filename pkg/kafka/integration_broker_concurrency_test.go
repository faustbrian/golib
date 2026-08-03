//go:build integration && !race

package kafka_test

// Two broker fixtures keep the integration suite bounded without serializing
// every independent Kafka topology.
const integrationBrokerConcurrency = 2
