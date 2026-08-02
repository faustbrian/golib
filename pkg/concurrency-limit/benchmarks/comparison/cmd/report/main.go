// Command report generates the checked-in adaptive-limiter comparison data and
// convergence plot from pinned implementations and deterministic workloads.
package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
	"github.com/faustbrian/golib/pkg/concurrency-limit/benchmarks/comparison/internal/netflix"
	"github.com/platinummonkey/go-concurrency-limits/core"
	platinumlimit "github.com/platinummonkey/go-concurrency-limits/limit"
)

const (
	minimumLimit = 1
	maximumLimit = 64
	initialLimit = 16
)

type workload struct {
	name    string
	seed    int64
	windows int
	model   func(*rand.Rand, int) (demand, capacity int, baseRTT time.Duration, explicitOverload bool)
}

type result struct {
	workload       string
	implementation string
	limits         []int
	goodput        int
	rejections     int
	capacity       int
	capacityTrace  []int
	queueTotal     time.Duration
	queueSamples   int
	latencies      []time.Duration
	collapseAdapt  int
	recoveryAdapt  int
}

type driver interface {
	name() string
	limit() int
	observe(time.Duration, int, bool)
}

func main() {
	output := "results"
	if len(os.Args) == 2 {
		output = os.Args[1]
	} else if len(os.Args) > 2 {
		fatalf("usage: report [output-directory]")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		fatalf("create output: %v", err)
	}

	workloads := campaigns()
	results := make([]result, 0, len(workloads)*4)
	for _, candidate := range workloads {
		for _, implementation := range newDrivers() {
			results = append(results, run(candidate, implementation))
		}
	}
	if err := writeCSV(filepath.Join(output, "metrics.csv"), results); err != nil {
		fatalf("write metrics: %v", err)
	}
	if err := writeTraceCSV(filepath.Join(output, "convergence.csv"), results); err != nil {
		fatalf("write convergence: %v", err)
	}
	for _, candidate := range workloads {
		if err := writeSVG(filepath.Join(output, "convergence-"+candidate.name+".svg"), candidate.name, results); err != nil {
			fatalf("write %s plot: %v", candidate.name, err)
		}
	}
}

func campaigns() []workload {
	return []workload{
		{name: "constant", seed: 201, windows: 100, model: constantWorkload},
		{name: "bursty", seed: 202, windows: 100, model: burstyWorkload},
		{name: "ramp", seed: 203, windows: 100, model: rampWorkload},
		{name: "bimodal", seed: 204, windows: 100, model: bimodalWorkload},
		{name: "heavy-tail", seed: 205, windows: 100, model: heavyTailWorkload},
		{name: "periodic", seed: 206, windows: 100, model: periodicWorkload},
		{name: "sparse", seed: 207, windows: 100, model: sparseWorkload},
		{name: "capacity-collapse", seed: 208, windows: 140, model: collapseWorkload},
		{name: "class-shift", seed: 209, windows: 100, model: classShiftWorkload},
	}
}

func run(candidate workload, implementation driver) result {
	random := rand.New(rand.NewSource(candidate.seed))
	measurement := result{
		workload: candidate.name, implementation: implementation.name(),
		limits: make([]int, 0, candidate.windows), capacityTrace: make([]int, 0, candidate.windows),
		collapseAdapt: -1, recoveryAdapt: -1,
	}
	for window := range candidate.windows {
		demand, capacity, baseRTT, explicitOverload := candidate.model(random, window)
		limit := implementation.limit()
		admitted := min(demand, limit)
		queue := time.Duration(max(admitted-capacity, 0)) * 4 * time.Millisecond
		rtt := baseRTT + queue
		measurement.goodput += min(admitted, capacity)
		measurement.rejections += demand - admitted
		measurement.capacity += capacity
		measurement.capacityTrace = append(measurement.capacityTrace, capacity)
		measurement.queueTotal += queue * time.Duration(admitted)
		measurement.queueSamples += admitted
		for range admitted {
			measurement.latencies = append(measurement.latencies, rtt)
		}
		implementation.observe(rtt, admitted, explicitOverload || admitted > capacity)
		measurement.limits = append(measurement.limits, implementation.limit())
		if candidate.name == "capacity-collapse" {
			if window >= 40 && window < 80 && measurement.collapseAdapt < 0 && implementation.limit() <= 8 {
				measurement.collapseAdapt = window - 40
			}
			if window >= 80 && measurement.collapseAdapt >= 0 && measurement.recoveryAdapt < 0 && implementation.limit() >= 15 {
				measurement.recoveryAdapt = window - 80
			}
		}
	}
	return measurement
}

func constantWorkload(_ *rand.Rand, window int) (int, int, time.Duration, bool) {
	return 28, 18, 10 * time.Millisecond, window%41 == 0
}

func burstyWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	demand := 14
	if window%8 == 0 {
		demand = 48
	}
	return demand, 18, time.Duration(8+random.Intn(8)) * time.Millisecond, window%43 == 0
}

func rampWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	return 4 + window/2, 18, time.Duration(8+random.Intn(5)) * time.Millisecond, window%46 == 0
}

func bimodalWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	rtt := time.Duration(7+random.Intn(4)) * time.Millisecond
	if random.Intn(2) == 0 {
		rtt = time.Duration(38+random.Intn(15)) * time.Millisecond
	}
	return 28, 18, rtt, window%44 == 0
}

func heavyTailWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	rtt := time.Duration(7+random.Intn(5)) * time.Millisecond
	if random.Intn(12) == 0 {
		rtt = time.Duration(80+random.Intn(80)) * time.Millisecond
	}
	return 28, 18, rtt, window%47 == 0
}

func periodicWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	demand := 12
	if window%20 < 5 {
		demand = 44
	}
	return demand, 18, time.Duration(8+random.Intn(6)) * time.Millisecond, window%45 == 0
}

func sparseWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	demand := 1
	if window%17 == 0 {
		demand = 20
	}
	return demand, 18, time.Duration(8+random.Intn(4)) * time.Millisecond, window%51 == 0
}

func collapseWorkload(_ *rand.Rand, window int) (int, int, time.Duration, bool) {
	capacity := 18
	if window >= 40 && window < 80 {
		capacity = 5
	} else if window >= 80 {
		capacity = 20
	}
	return 32, capacity, 10 * time.Millisecond, window%37 == 0
}

func classShiftWorkload(random *rand.Rand, window int) (int, int, time.Duration, bool) {
	if window < 50 {
		return 16, 12, time.Duration(7+random.Intn(4)) * time.Millisecond, window%43 == 0
	}
	return 40, 24, time.Duration(18+random.Intn(9)) * time.Millisecond, window%43 == 0
}

func newDrivers() []driver {
	return []driver{newLocal(), newNetflix(), newPlatinum(), newFailsafe()}
}

type localDriver struct {
	algorithm concurrencylimit.Algorithm
	current   int
}

func newLocal() *localDriver {
	algorithm, err := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{
		LongWindow: 20, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 4,
	})
	if err != nil {
		panic(err)
	}
	algorithm.Reset(initialLimit)
	return &localDriver{algorithm: algorithm, current: initialLimit}
}

func (*localDriver) name() string      { return "local-gradient2" }
func (driver *localDriver) limit() int { return driver.current }
func (driver *localDriver) observe(rtt time.Duration, inflight int, overload bool) {
	window := concurrencylimit.Window{
		CurrentLimit: driver.current, Samples: max(inflight, 1), MaxInFlight: inflight,
		RecentLatency: rtt, BaselineLatency: 10 * time.Millisecond,
	}
	if overload {
		window.Overloads = 1
	}
	driver.current = clamp(driver.algorithm.Update(window).Limit)
}

type netflixDriver struct{ algorithm *netflix.Gradient2 }

func newNetflix() *netflixDriver {
	return &netflixDriver{algorithm: netflix.New(initialLimit, minimumLimit, maximumLimit)}
}
func (*netflixDriver) name() string      { return "netflix-reference" }
func (driver *netflixDriver) limit() int { return driver.algorithm.Limit() }
func (driver *netflixDriver) observe(rtt time.Duration, inflight int, _ bool) {
	driver.algorithm.Update(float64(rtt), inflight)
}

type platinumDriver struct{ algorithm *platinumlimit.Gradient2Limit }

func newPlatinum() *platinumDriver {
	algorithm, err := platinumlimit.NewGradient2Limit(
		"comparison", initialLimit, maximumLimit, minimumLimit,
		func(int) int { return 4 }, 0.2, 20,
		platinumlimit.NoopLimitLogger{}, core.EmptyMetricRegistryInstance,
	)
	if err != nil {
		panic(err)
	}
	return &platinumDriver{algorithm: algorithm}
}

func (*platinumDriver) name() string      { return "platinum-gradient2" }
func (driver *platinumDriver) limit() int { return driver.algorithm.EstimatedLimit() }
func (driver *platinumDriver) observe(rtt time.Duration, inflight int, overload bool) {
	driver.algorithm.OnSample(time.Now().UnixNano(), int64(rtt), inflight, overload)
}

type failsafeDriver struct {
	limiter adaptivelimiter.AdaptiveLimiter[any]
}

func newFailsafe() *failsafeDriver {
	return &failsafeDriver{limiter: adaptivelimiter.NewBuilder[any]().
		WithLimits(minimumLimit, maximumLimit, initialLimit).
		WithRecentWindow(0, 0, 1).
		Build()}
}

func (*failsafeDriver) name() string      { return "failsafe-go" }
func (driver *failsafeDriver) limit() int { return driver.limiter.Limit() }
func (driver *failsafeDriver) observe(rtt time.Duration, inflight int, overload bool) {
	permits := make([]adaptivelimiter.Permit, 0, inflight)
	for range inflight {
		permit, ok := driver.limiter.TryAcquirePermit()
		if !ok {
			break
		}
		permits = append(permits, permit)
	}
	time.Sleep(rtt)
	for _, permit := range permits {
		if overload {
			permit.Drop()
		} else {
			permit.Record()
		}
	}
}

func writeCSV(path string, results []result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := csv.NewWriter(file)
	if err = writer.Write([]string{"workload", "implementation", "utilization", "goodput", "rejections", "mean_queue_ms", "p99_ms", "collapse_adaptation_windows", "recovery_adaptation_windows"}); err != nil {
		_ = file.Close()
		return err
	}
	for _, measurement := range results {
		queueMean := 0.0
		if measurement.queueSamples != 0 {
			queueMean = float64(measurement.queueTotal) / float64(time.Millisecond) / float64(measurement.queueSamples)
		}
		row := []string{
			measurement.workload, measurement.implementation,
			fmt.Sprintf("%.6f", float64(measurement.goodput)/float64(measurement.capacity)),
			strconv.Itoa(measurement.goodput), strconv.Itoa(measurement.rejections),
			fmt.Sprintf("%.3f", queueMean), fmt.Sprintf("%.3f", percentileMillis(measurement.latencies, 0.99)),
			strconv.Itoa(measurement.collapseAdapt), strconv.Itoa(measurement.recoveryAdapt),
		}
		if err = writer.Write(row); err != nil {
			_ = file.Close()
			return err
		}
	}
	return finishCSV(file, writer)
}

func writeTraceCSV(path string, results []result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := csv.NewWriter(file)
	if err = writer.Write([]string{"workload", "window", "implementation", "limit", "capacity"}); err != nil {
		_ = file.Close()
		return err
	}
	for _, measurement := range results {
		for window, limit := range measurement.limits {
			if err = writer.Write([]string{
				measurement.workload, strconv.Itoa(window), measurement.implementation,
				strconv.Itoa(limit), strconv.Itoa(measurement.capacityTrace[window]),
			}); err != nil {
				_ = file.Close()
				return err
			}
		}
	}
	return finishCSV(file, writer)
}

func finishCSV(file *os.File, writer *csv.Writer) error {
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeSVG(path, workloadName string, results []result) error {
	const width, height = 960, 540
	var selected []result
	for _, measurement := range results {
		if measurement.workload == workloadName {
			selected = append(selected, measurement)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no results for workload %q", workloadName)
	}
	lastWindow := len(selected[0].limits) - 1
	colors := map[string]string{
		"local-gradient2": "#2563eb", "netflix-reference": "#dc2626",
		"platinum-gradient2": "#16a34a", "failsafe-go": "#9333ea",
	}
	var body strings.Builder
	fmt.Fprintf(&body, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, width, height, width, height)
	fmt.Fprintf(&body, `<rect width="100%%" height="100%%" fill="white"/><text x="70" y="35" font-family="sans-serif" font-size="20">%s convergence</text>`, workloadName)
	body.WriteString(`<line x1="70" y1="470" x2="930" y2="470" stroke="#111"/><line x1="70" y1="55" x2="70" y2="470" stroke="#111"/>`)
	for tick := 0; tick <= 64; tick += 8 {
		y := 470 - float64(tick)/64*400
		fmt.Fprintf(&body, `<line x1="65" y1="%.1f" x2="930" y2="%.1f" stroke="#e5e7eb"/><text x="35" y="%.1f" font-family="sans-serif" font-size="12">%d</text>`, y, y, y+4, tick)
	}
	for tickIndex := range 8 {
		tick := tickIndex * lastWindow / 7
		x := 70 + float64(tick)/float64(lastWindow)*860
		fmt.Fprintf(&body, `<line x1="%.1f" y1="470" x2="%.1f" y2="476" stroke="#111"/><text x="%.1f" y="492" font-family="sans-serif" font-size="11">%d</text>`, x, x, x-8, tick)
	}
	legendY := 70
	capacityPoints := make([]string, 0, len(selected[0].capacityTrace))
	for window, capacity := range selected[0].capacityTrace {
		x := 70 + float64(window)/float64(lastWindow)*860
		y := 470 - float64(capacity)/64*400
		capacityPoints = append(capacityPoints, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	fmt.Fprintf(&body, `<polyline fill="none" stroke="#111827" stroke-width="2" stroke-dasharray="6 4" points="%s"/>`, strings.Join(capacityPoints, " "))
	body.WriteString(`<line x1="100" y1="70" x2="125" y2="70" stroke="#111827" stroke-width="2" stroke-dasharray="6 4"/><text x="135" y="74" font-family="sans-serif" font-size="12">modeled capacity</text>`)
	legendY += 20
	for _, measurement := range selected {
		points := make([]string, 0, len(measurement.limits))
		for window, limit := range measurement.limits {
			x := 70 + float64(window)/float64(lastWindow)*860
			y := 470 - float64(limit)/64*400
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		color := colors[measurement.implementation]
		fmt.Fprintf(&body, `<polyline fill="none" stroke="%s" stroke-width="2" points="%s"/>`, color, strings.Join(points, " "))
		fmt.Fprintf(&body, `<line x1="100" y1="%d" x2="125" y2="%d" stroke="%s" stroke-width="3"/><text x="135" y="%d" font-family="sans-serif" font-size="12">%s</text>`, legendY, legendY, color, legendY+4, measurement.implementation)
		legendY += 20
	}
	body.WriteString(`<text x="475" y="515" font-family="sans-serif" font-size="13">window</text><text x="16" y="280" transform="rotate(-90 16 280)" font-family="sans-serif" font-size="13">limit</text></svg>`)
	return os.WriteFile(path, []byte(body.String()), 0o644)
}

func percentileMillis(values []time.Duration, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	slices.Sort(ordered)
	index := min(int(math.Ceil(quantile*float64(len(ordered))))-1, len(ordered)-1)
	return float64(ordered[index]) / float64(time.Millisecond)
}

func clamp(limit int) int { return min(max(limit, minimumLimit), maximumLimit) }

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
