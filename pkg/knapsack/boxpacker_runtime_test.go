package knapsack_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

type runtimeComparisonEvidence struct {
	SchemaVersion         string                     `json:"schema_version"`
	PackageCommit         string                     `json:"package_commit"`
	InputSHA256           string                     `json:"input_sha256"`
	Date                  string                     `json:"date"`
	Environment           string                     `json:"environment"`
	Processor             string                     `json:"processor"`
	Command               string                     `json:"command"`
	SamplesPerAdapter     int                        `json:"samples_per_adapter"`
	WarmupsPerAdapter     int                        `json:"warmups_per_adapter"`
	CommonSemantics       string                     `json:"common_semantics"`
	WallTimeIncludes      []string                   `json:"wall_time_includes"`
	WallTimeExcludes      []string                   `json:"wall_time_excludes"`
	AllocationMeasurement string                     `json:"allocation_measurement"`
	Verification          string                     `json:"verification"`
	Samples               []runtimeComparisonSample  `json:"samples"`
	Summaries             []runtimeComparisonSummary `json:"summaries"`
	Cancellation          []runtimeCancellation      `json:"cancellation"`
}

type runtimeComparisonSample struct {
	Implementation   string `json:"implementation"`
	RuntimeVersion   string `json:"runtime_version"`
	Sample           int    `json:"sample"`
	WallNanoseconds  int64  `json:"wall_nanoseconds"`
	SolveNanoseconds int64  `json:"solve_nanoseconds"`
	PeakRSSBytes     int64  `json:"peak_rss_bytes"`
	ContainerCount   uint32 `json:"container_count"`
	PackedItems      uint32 `json:"packed_items"`
}

type runtimeComparisonSummary struct {
	Implementation      string `json:"implementation"`
	RuntimeVersion      string `json:"runtime_version"`
	WallP50Nanoseconds  int64  `json:"wall_p50_nanoseconds"`
	WallP95Nanoseconds  int64  `json:"wall_p95_nanoseconds"`
	SolveP50Nanoseconds int64  `json:"solve_p50_nanoseconds"`
	SolveP95Nanoseconds int64  `json:"solve_p95_nanoseconds"`
	MaximumPeakRSSBytes int64  `json:"maximum_peak_rss_bytes"`
	ContainerCount      uint32 `json:"container_count"`
	PackedItems         uint32 `json:"packed_items"`
}

type runtimeCancellation struct {
	Implementation      string `json:"implementation"`
	DeadlineNanoseconds int64  `json:"deadline_nanoseconds"`
	ReturnNanoseconds   int64  `json:"return_nanoseconds"`
	ProcessStopped      bool   `json:"process_stopped"`
}

type comparisonCommand struct {
	implementation string
	path           string
	arguments      []string
}

func TestBoxPackerRuntimeComparison(t *testing.T) {
	if os.Getenv("BOXPACKER_RUNTIME_COMPARE") != "1" {
		t.Skip("run through make benchmark-compare")
	}
	goBinary := os.Getenv("BOXPACKER_GO_ADAPTER")
	outputPath := os.Getenv("BOXPACKER_RUNTIME_OUTPUT")
	packageCommit := os.Getenv("BOXPACKER_PACKAGE_COMMIT")
	inputSHA256 := os.Getenv("BOXPACKER_INPUT_SHA256")
	processor := os.Getenv("BOXPACKER_PROCESSOR")
	if goBinary == "" || outputPath == "" || packageCommit == "" ||
		inputSHA256 == "" || processor == "" {
		t.Fatal("runtime comparison environment is incomplete")
	}
	sampleCount, err := strconv.Atoi(os.Getenv("BOXPACKER_RUNTIME_SAMPLES"))
	if err != nil || sampleCount < 10 || sampleCount > 100 {
		t.Fatalf("BOXPACKER_RUNTIME_SAMPLES must be between 10 and 100: %v", err)
	}
	commands := []comparisonCommand{
		{implementation: "dvdoug/BoxPacker", path: "php", arguments: []string{"integration/boxpacker/compare.php"}},
		{implementation: "github.com/faustbrian/golib/pkg/knapsack", path: goBinary},
	}
	request := boxPackerRequest(t)
	for _, command := range commands {
		_ = runRuntimeSample(t, request, command, 0)
	}
	evidence := runtimeComparisonEvidence{
		SchemaVersion: "v1", PackageCommit: packageCommit,
		InputSHA256: inputSHA256,
		Date:        time.Now().Format(time.DateOnly), Environment: runtime.GOOS + "/" + runtime.GOARCH,
		Processor:         processor,
		Command:           "make benchmark-compare",
		SamplesPerAdapter: sampleCount, WarmupsPerAdapter: 1,
		CommonSemantics:       "integral cuboids; millimetres; grams; unrestricted orthogonal rotation; one unlimited 4x1x1 container type; two 2x1x1 items; pack-all; box-count objective",
		WallTimeIncludes:      []string{"process startup", "runtime startup", "autoload", "fixture construction", "solve", "serialization"},
		WallTimeExcludes:      []string{"adapter compilation", "Composer install", "independent verification"},
		AllocationMeasurement: "not reported because PHP and Go do not expose comparable allocation counts across process boundaries",
		Verification:          "every decoded plan independently verified by knapsack/verify before recording",
	}
	for sample := 1; sample <= sampleCount; sample++ {
		order := commands
		if sample%2 == 0 {
			order = []comparisonCommand{commands[1], commands[0]}
		}
		for _, command := range order {
			entry := runRuntimeSample(t, request, command, sample)
			evidence.Samples = append(evidence.Samples, entry)
		}
	}
	for _, command := range commands {
		evidence.Summaries = append(evidence.Summaries,
			summarizeRuntimeSamples(t, command.implementation, evidence.Samples),
		)
		evidence.Cancellation = append(evidence.Cancellation,
			probeRuntimeCancellation(t, command),
		)
	}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(outputPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRuntimeSample(
	t *testing.T,
	request knapsack.NormalizedRequest,
	command comparisonCommand,
	sample int,
) runtimeComparisonSample {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command.path, command.arguments...)
	started := time.Now()
	encoded, err := process.Output()
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("%s adapter failed: %v", command.implementation, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("%s adapter exceeded runtime limit: %v", command.implementation, ctx.Err())
	}
	var output boxPackerOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.AdapterSchema != "v2" || output.Implementation != command.implementation ||
		output.ImplementationVersion == "" || output.ImplementationRevision == "" ||
		output.RuntimeVersion == "" || output.Timing.SolveNanoseconds <= 0 ||
		output.Timing.ProcessStartupIncluded || output.Timing.AutoloadAndFixtureIncluded ||
		output.Timing.VerificationIncluded {
		t.Fatalf("invalid %s adapter disclosure: %+v", command.implementation, output)
	}
	plan := boxPackerPlan(t, request, output)
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("%s emitted invalid plan: %+v", command.implementation, result.Violations())
	}
	statistics := plan.Statistics()
	return runtimeComparisonSample{
		Implementation:   command.implementation,
		RuntimeVersion:   output.RuntimeVersion,
		Sample:           sample,
		WallNanoseconds:  elapsed.Nanoseconds(),
		SolveNanoseconds: output.Timing.SolveNanoseconds,
		PeakRSSBytes:     processPeakRSS(t, process.ProcessState),
		ContainerCount:   statistics.ContainerCount,
		PackedItems:      statistics.PackedItems,
	}
}

func processPeakRSS(t *testing.T, state *os.ProcessState) int64 {
	t.Helper()
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok {
		t.Fatalf("unsupported process usage type %T", state.SysUsage())
	}
	rss := usage.Maxrss
	if runtime.GOOS == "linux" {
		rss *= 1024
	} else if runtime.GOOS != "darwin" {
		t.Fatalf("peak RSS normalization is unsupported on %s", runtime.GOOS)
	}
	if rss <= 0 {
		t.Fatalf("invalid peak RSS %d", rss)
	}
	return rss
}

func summarizeRuntimeSamples(
	t *testing.T,
	implementation string,
	samples []runtimeComparisonSample,
) runtimeComparisonSummary {
	t.Helper()
	var walls, solves []int64
	var summary runtimeComparisonSummary
	for _, sample := range samples {
		if sample.Implementation != implementation {
			continue
		}
		walls = append(walls, sample.WallNanoseconds)
		solves = append(solves, sample.SolveNanoseconds)
		if sample.PeakRSSBytes > summary.MaximumPeakRSSBytes {
			summary.MaximumPeakRSSBytes = sample.PeakRSSBytes
		}
		summary.RuntimeVersion = sample.RuntimeVersion
		summary.ContainerCount = sample.ContainerCount
		summary.PackedItems = sample.PackedItems
	}
	if len(walls) < 10 {
		t.Fatalf("%s has only %d runtime samples", implementation, len(walls))
	}
	slices.Sort(walls)
	slices.Sort(solves)
	summary.Implementation = implementation
	summary.WallP50Nanoseconds = percentile(walls, 50)
	summary.WallP95Nanoseconds = percentile(walls, 95)
	summary.SolveP50Nanoseconds = percentile(solves, 50)
	summary.SolveP95Nanoseconds = percentile(solves, 95)
	return summary
}

func percentile(sorted []int64, percentage int) int64 {
	index := (percentage*len(sorted) + 99) / 100
	return sorted[index-1]
}

func probeRuntimeCancellation(t *testing.T, command comparisonCommand) runtimeCancellation {
	t.Helper()
	const deadline = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	process := exec.CommandContext(ctx, command.path, command.arguments...)
	process.Env = append(os.Environ(), "COMPARE_DELAY_MS=5000")
	started := time.Now()
	err := process.Run()
	elapsed := time.Since(started)
	stopped := process.ProcessState != nil &&
		errors.Is(process.Process.Signal(syscall.Signal(0)), os.ErrProcessDone)
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || err == nil ||
		!stopped || elapsed > time.Second {
		t.Fatalf("%s cancellation failed: err=%v ctx=%v elapsed=%s state=%v",
			command.implementation, err, ctx.Err(), elapsed, process.ProcessState)
	}
	return runtimeCancellation{
		Implementation:      command.implementation,
		DeadlineNanoseconds: deadline.Nanoseconds(),
		ReturnNanoseconds:   elapsed.Nanoseconds(),
		ProcessStopped:      stopped,
	}
}

func TestRuntimePercentileUsesNearestRank(t *testing.T) {
	values := []int64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5}
	slices.Sort(values)
	if got := percentile(values, 50); got != 5 {
		t.Fatalf("p50 = %d, want 5", got)
	}
	if got := percentile(values, 95); got != 10 {
		t.Fatalf("p95 = %d, want 10", got)
	}
}
