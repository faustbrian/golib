SHELL := /bin/sh

FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark conformance coverage docs fuzz leak race stress test

test:
	go test ./... -count=1

race:
	go test -race ./... -count=3

coverage:
	../../scripts/check-coverage.sh pkg/concurrency-limit

fuzz:
	go test -run='^$$' -fuzz=FuzzLimitHistoriesRemainFiniteAndBounded -fuzztime=$(FUZZ_TIME) .
	go test -run='^$$' -fuzz=FuzzPermitSequencesRecordAtMostOneOutcome -fuzztime=$(FUZZ_TIME) .

leak:
	go test -run='^TestCanceledWaitersTerminateWithoutBackgroundWorkers$$' -count=20 .

conformance:
	go test -run='Test(AIMDReferenceEquation|VegasReferenceQueueEquationAndThroughputSignal|Gradient2ReferenceEquation|EveryAlgorithmUpdateMatchesDeterministicReference|VegasSimulationConvergesAndRecoversWithReproducibleWorkloads|AlgorithmsRemainDeterministicAcrossNoisyWorkloadClasses|SeededWorkloadCampaignsRemainBoundedAndDeterministic)$$' -count=1 .

stress:
	go test -race -run='Test(CompletionCancellationTimeoutResetSnapshotDrainRaceMatrix|FIFOAdmissionPreventsStarvationAcrossMetadataAndDurations)$$' -count=50 -timeout=5m .

benchmark:
	go test -run='^$$' -bench=. -benchmem -benchtime=$(BENCH_TIME) .

docs:
	go test -run='^Example' ./...
	go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | xargs -n 1 go doc >/dev/null

api-compat:
	../../scripts/check-api-baseline.sh pkg/concurrency-limit
