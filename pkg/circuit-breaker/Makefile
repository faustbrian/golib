SHELL := /bin/sh

FUZZ_TIME ?= 1s
BENCH_TIME ?= 100ms
RACE_COUNT ?= 3
PROFILE_DIR ?= profiles

.PHONY: benchmark check compatibility coverage docs fmt fuzz integration leak lint profile race safety test tools vet

check: fmt vet lint test integration coverage race fuzz leak benchmark docs safety compatibility

fmt:
	@files="$$(find . -type f -name '*.go')"; \
		test -z "$$(gofmt -l $$files)" || { gofmt -l $$files; exit 1; }

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test ./...

integration:
	go test ./integration
	cd integration/consumers && go vet ./...
	cd integration/consumers && go test -race -count=$(RACE_COUNT) ./...

coverage:
	./scripts/check-coverage.sh

race:
	go test -race -count=$(RACE_COUNT) ./...

fuzz:
	go test -run=^$$ -fuzz=FuzzConfigurationNeverPanics -fuzztime=$(FUZZ_TIME) .
	go test -run=^$$ -fuzz=FuzzPermitOperationSequences -fuzztime=$(FUZZ_TIME) .
	go test -run=^$$ -fuzz=FuzzObserverSequences -fuzztime=$(FUZZ_TIME) .
	go test -run=^$$ -fuzz=FuzzConfigurationResourceBounds -fuzztime=$(FUZZ_TIME) .
	go test -run=^$$ -fuzz=FuzzExecutionDurationsAndOutcomes -fuzztime=$(FUZZ_TIME) .
	go test -run=^$$ -fuzz=FuzzCountMatchesBoundedReference -fuzztime=$(FUZZ_TIME) ./window
	go test -run=^$$ -fuzz=FuzzTimeWindowTimestamps -fuzztime=$(FUZZ_TIME) ./window

leak:
	go test -run='Test(CanceledHalfOpenWaitReleasesClockTimer|CloseDrainsObserverQueueAndDropsLaterEvents|AsynchronousObserverCanRequestCloseReentrantly|ShutdownHonorsContextWhileObserverIsBlocked)$$' -count=10 .
	go test -run='TestStoppedTimersAreNotRetained$$' -count=10 ./breakertest

benchmark:
	go test -run=^$$ -bench=. -benchtime=$(BENCH_TIME) -benchmem -cpu=1,2,4,8 ./...

profile:
	mkdir -p $(PROFILE_DIR)
	go test -o=$(PROFILE_DIR)/core.test -run=^$$ -bench='Benchmark(ClosedExecute|HalfOpenContention|SynchronousTransitionObserver|AsynchronousTransitionObserver)$$' -benchtime=5s -cpuprofile=$(PROFILE_DIR)/core-cpu.out -memprofile=$(PROFILE_DIR)/core-memory.out -mutexprofile=$(PROFILE_DIR)/core-mutex.out .
	go test -o=$(PROFILE_DIR)/window.test -run=^$$ -bench='Benchmark(CountRollover|TimeRollover|TimeSnapshot)$$' -benchtime=5s -cpuprofile=$(PROFILE_DIR)/window-cpu.out -memprofile=$(PROFILE_DIR)/window-memory.out -mutexprofile=$(PROFILE_DIR)/window-mutex.out ./window

docs:
	go test -run='^Example' ./...
	go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | \
		xargs -n 1 go doc >/dev/null

safety:
	./scripts/check-safety.sh
	govulncheck ./...
	cd integration/consumers && govulncheck ./...
	gitleaks dir --redact --no-banner .

compatibility:
	./scripts/check-compatibility.sh

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	go install github.com/zricethezav/gitleaks/v8@v8.30.0
	go install golang.org/x/exp/cmd/apidiff@v0.0.0-20260709172345-9ea1abe57597
