package concurrencylimit_test

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestSeededWorkloadCampaignsRemainBoundedAndDeterministic(t *testing.T) {
	t.Parallel()

	campaigns := []workloadCampaign{
		{name: "constant", seed: 101, windows: 120, demand: constantDemand(24), capacity: constantCapacity(16), latency: constantLatency(10)},
		{name: "bursty", seed: 102, windows: 120, demand: periodicDemand(12, 40, 8), capacity: constantCapacity(16), latency: burstyLatency},
		{name: "ramp", seed: 103, windows: 120, demand: rampDemand, capacity: constantCapacity(18), latency: rampLatency},
		{name: "bimodal", seed: 104, windows: 120, demand: constantDemand(24), capacity: constantCapacity(16), latency: bimodalLatency},
		{name: "heavy-tail", seed: 105, windows: 120, demand: constantDemand(24), capacity: constantCapacity(16), latency: heavyTailLatency},
		{name: "periodic", seed: 106, windows: 120, demand: periodicDemand(16, 28, 6), capacity: constantCapacity(16), latency: periodicLatency},
		{name: "sparse", seed: 107, windows: 120, demand: sparseDemand, capacity: constantCapacity(16), latency: constantLatency(10)},
		{name: "capacity-collapse", seed: 108, windows: 140, demand: constantDemand(32), capacity: collapseCapacity, latency: constantLatency(10)},
		{name: "workload-class-shift", seed: 109, windows: 140, demand: constantDemand(24), capacity: constantCapacity(16), latency: classShiftLatency},
	}

	for _, campaign := range campaigns {
		campaign := campaign
		t.Run(campaign.name, func(t *testing.T) {
			t.Parallel()
			first := runWorkloadCampaign(t, campaign)
			second := runWorkloadCampaign(t, campaign)
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("seeded executions differ:\nfirst  = %+v\nsecond = %+v", first, second)
			}
			if first.goodput == 0 || first.admittedLast20 == 0 {
				t.Fatalf("campaign collapsed to permanent rejection: %+v", first)
			}
			if first.minimumLimit < 1 || first.maximumLimit > 64 || !first.finite {
				t.Fatalf("campaign escaped numeric bounds: %+v", first)
			}
			if first.maximumTail > 500*time.Millisecond {
				t.Fatalf("campaign produced unbounded modeled tail latency: %+v", first)
			}
			if campaign.name == "capacity-collapse" {
				preCollapse := averageLimits(first.limits[30:40])
				collapsed := averageLimits(first.limits[65:80])
				recovered := averageLimits(first.limits[120:140])
				if collapsed >= preCollapse || recovered <= collapsed ||
					first.adaptationWindow < 0 || first.adaptationWindow > 40 {
					t.Fatalf("collapse/recovery limits = pre %.2f collapsed %.2f recovered %.2f", preCollapse, collapsed, recovered)
				}
			}
		})
	}
}

type workloadCampaign struct {
	name     string
	seed     int64
	windows  int
	demand   func(int) int
	capacity func(int) int
	latency  func(*rand.Rand, int, int) time.Duration
}

type campaignResult struct {
	limits           []int
	minimumLimit     int
	maximumLimit     int
	goodput          int
	rejections       int
	admittedLast20   int
	maximumTail      time.Duration
	adaptationWindow int
	finite           bool
}

func runWorkloadCampaign(t *testing.T, campaign workloadCampaign) campaignResult {
	t.Helper()
	algorithm, err := concurrencylimit.NewVegasAlgorithm(concurrencylimit.VegasConfig{
		Alpha: 2, Beta: 5, Increase: 1, Decrease: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	algorithm.Reset(4)
	random := rand.New(rand.NewSource(campaign.seed))
	result := campaignResult{
		limits: make([]int, 0, campaign.windows), minimumLimit: 64, finite: true, adaptationWindow: -1,
	}
	limit := 4
	previousThroughput := 0.0
	previousMaxInFlight := 0
	for windowIndex := range campaign.windows {
		demand := max(campaign.demand(windowIndex), 0)
		capacity := max(campaign.capacity(windowIndex), 1)
		admitted := min(demand, limit)
		result.rejections += demand - admitted
		if admitted == 0 {
			result.limits = append(result.limits, limit)
			result.minimumLimit = min(result.minimumLimit, limit)
			result.maximumLimit = max(result.maximumLimit, limit)
			continue
		}
		latencies := make([]time.Duration, 0, admitted)
		for request := range admitted {
			latency := campaign.latency(random, windowIndex, request)
			if admitted > capacity {
				latency += time.Duration(admitted-capacity) * 4 * time.Millisecond
			}
			latencies = append(latencies, latency)
		}
		sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
		tail := latencies[int(math.Ceil(0.9*float64(len(latencies))))-1]
		result.maximumTail = max(result.maximumTail, tail)
		successes := min(admitted, capacity)
		dependencyFailures := 0
		overloads := 0
		if admitted > 0 && random.Intn(13) == 0 {
			dependencyFailures = 1
			successes--
		}
		if admitted > capacity || (admitted > 0 && random.Intn(37) == 0) {
			overloads = 1
		}
		throughput := float64(max(successes, 0)) / max(tail.Seconds(), 1e-9)
		decision := algorithm.Update(concurrencylimit.Window{
			CurrentLimit: limit, Samples: len(latencies), MaxInFlight: admitted,
			RecentLatency: tail, BaselineLatency: 10 * time.Millisecond,
			Throughput: throughput, PreviousThroughput: previousThroughput,
			PreviousMaxInFlight: previousMaxInFlight,
			Overloads:           uint64(overloads), DependencyFailures: uint64(dependencyFailures),
		})
		limit = clampReferenceLimit(decision.Limit)
		result.limits = append(result.limits, limit)
		result.minimumLimit = min(result.minimumLimit, limit)
		result.maximumLimit = max(result.maximumLimit, limit)
		result.goodput += max(successes, 0)
		if windowIndex >= campaign.windows-20 {
			result.admittedLast20 += admitted
		}
		result.finite = result.finite && finiteAlgorithmState(decision.State)
		if campaign.name == "capacity-collapse" && windowIndex >= 80 && result.adaptationWindow < 0 && limit >= 10 {
			result.adaptationWindow = windowIndex - 80
		}
		previousThroughput = throughput
		previousMaxInFlight = admitted
	}
	return result
}

func finiteAlgorithmState(state concurrencylimit.AlgorithmState) bool {
	return !math.IsNaN(state.Estimate) && !math.IsInf(state.Estimate, 0) &&
		!math.IsNaN(state.QueueEstimate) && !math.IsInf(state.QueueEstimate, 0) &&
		!math.IsNaN(state.Throughput) && !math.IsInf(state.Throughput, 0)
}

func constantDemand(demand int) func(int) int     { return func(int) int { return demand } }
func constantCapacity(capacity int) func(int) int { return func(int) int { return capacity } }
func constantLatency(milliseconds int) func(*rand.Rand, int, int) time.Duration {
	return func(*rand.Rand, int, int) time.Duration { return time.Duration(milliseconds) * time.Millisecond }
}

func periodicDemand(low, high, period int) func(int) int {
	return func(window int) int {
		if window%period == 0 {
			return high
		}
		return low
	}
}

func rampDemand(window int) int { return min(4+window/3, 40) }
func sparseDemand(window int) int {
	if window%5 == 0 {
		return 1
	}
	return 0
}

func collapseCapacity(window int) int {
	switch {
	case window < 40:
		return 16
	case window < 80:
		return 5
	default:
		return 20
	}
}

func burstyLatency(random *rand.Rand, window, _ int) time.Duration {
	if window%8 == 0 {
		return time.Duration(35+random.Intn(20)) * time.Millisecond
	}
	return time.Duration(8+random.Intn(5)) * time.Millisecond
}

func rampLatency(random *rand.Rand, window, _ int) time.Duration {
	return time.Duration(8+window/8+random.Intn(4)) * time.Millisecond
}

func bimodalLatency(random *rand.Rand, _, _ int) time.Duration {
	if random.Intn(2) == 0 {
		return time.Duration(5+random.Intn(3)) * time.Millisecond
	}
	return time.Duration(35+random.Intn(10)) * time.Millisecond
}

func heavyTailLatency(random *rand.Rand, _, _ int) time.Duration {
	if random.Intn(20) == 0 {
		return time.Duration(150+random.Intn(100)) * time.Millisecond
	}
	return time.Duration(5+random.Intn(5)) * time.Millisecond
}

func periodicLatency(random *rand.Rand, window, _ int) time.Duration {
	base := 8
	if (window/6)%2 == 1 {
		base = 28
	}
	return time.Duration(base+random.Intn(5)) * time.Millisecond
}

func classShiftLatency(random *rand.Rand, window, _ int) time.Duration {
	if window < 60 {
		return time.Duration(5+random.Intn(4)) * time.Millisecond
	}
	return time.Duration(35+random.Intn(8)) * time.Millisecond
}

func averageLimits(values []int) float64 {
	total := 0
	for _, value := range values {
		total += value
	}
	return float64(total) / float64(len(values))
}
