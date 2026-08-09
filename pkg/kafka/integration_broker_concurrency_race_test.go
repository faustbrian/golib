//go:build integration && race

package kafka_test

// Race instrumentation permits the same two bounded broker fixtures as the
// ordinary integration suite. Serializing the complete broker matrix makes
// independent tests spend their package-wide deadline waiting for a slot and
// can prevent their own bounded contexts from ever starting.
const integrationBrokerConcurrency = 2
