package gomoney_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
	"github.com/faustbrian/golib/pkg/money"
)

const deterministicRankingHelper = "GOMONEY_DETERMINISTIC_RANKING_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(deterministicRankingHelper) == "1" {
		output, err := deterministicRanking()
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(output)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDeterministicRankingAcrossProcessesAndArchitectures(t *testing.T) {
	t.Parallel()

	want, err := deterministicRanking()
	if err != nil {
		t.Fatal(err)
	}
	const wantSHA256 = "1e228c03ec098ab0478bba65f35385fb2c1a3f1ec8910890c7382aaaf9e0e3f8"
	if digest := fmt.Sprintf("%x", sha256.Sum256(want)); digest != wantSHA256 {
		t.Fatalf("ranking digest = %s, want %s", digest, wantSHA256)
	}

	for process := range 4 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, os.Args[0])
		command.Env = append(os.Environ(),
			deterministicRankingHelper+"=1",
			"GOMAXPROCS="+strconv.Itoa(process%4+1),
			"LANG=C",
			"LC_ALL=C",
			"TZ=UTC",
		)
		got, commandErr := command.Output()
		cancel()
		if commandErr != nil {
			t.Fatalf("ranking process %d: %v", process, commandErr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("ranking process %d produced different bytes", process)
		}
	}
}

func TestExactSolverRankingIsIndependentOfContainerSearchOrder(t *testing.T) {
	t.Parallel()

	smallCost := mustEuro(t, "1.20")
	largeCost, err := money.Parse("1.20", smallCost.Currency(), smallCost.Context())
	if err != nil {
		t.Fatal(err)
	}
	costs, err := gomoney.New(map[string]money.Money{
		"small": smallCost,
		"large": largeCost,
	})
	if err != nil {
		t.Fatal(err)
	}

	var want string
	for index, priorities := range [][2]int64{{-10, 10}, {10, -10}} {
		request := exactMoneyRequestWithContainerPriorities(t, priorities[0], priorities[1], true)
		plan, solveErr := (solver.Exact{}).PackAll(
			context.Background(), request.Normalized(), solver.Options{PlanObjective: costs},
		)
		if solveErr != nil {
			t.Fatal(solveErr)
		}
		if index == 0 {
			want = plan.CanonicalString()
			continue
		}
		if plan.CanonicalString() != want {
			t.Fatalf("container priority order changed canonical winner: %s != %s", plan.CanonicalString(), want)
		}
	}
}

func TestProductionDependencyBoundaryExcludesAmbientPolicy(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve hardening test path")
	}
	productionFiles, err := filepath.Glob(filepath.Join(filepath.Dir(currentFile), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"context": true, "errors": true, "fmt": true, "math/big": true,
		"slices": true, "strings": true,
		"github.com/faustbrian/golib/pkg/knapsack":     true,
		"github.com/faustbrian/golib/pkg/math":         true,
		"github.com/faustbrian/golib/pkg/math/decimal": true,
		"github.com/faustbrian/golib/pkg/money":        true,
	}
	inspected := 0
	for _, productionFile := range productionFiles {
		if strings.HasSuffix(productionFile, "_test.go") {
			continue
		}
		inspected++
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), productionFile, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, imported := range parsed.Imports {
			path, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			if !allowed[path] {
				t.Errorf("production dependency %q in %s lacks an exact-money audit", path, filepath.Base(productionFile))
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, identifierOK := node.(*ast.Ident)
			if identifierOK && (identifier.Name == "float32" || identifier.Name == "float64") {
				t.Errorf("production file %s uses binary floating-point type %s", filepath.Base(productionFile), identifier.Name)
			}
			literal, literalOK := node.(*ast.BasicLit)
			if literalOK && literal.Kind == token.FLOAT {
				t.Errorf("production file %s uses binary floating-point literal %s", filepath.Base(productionFile), literal.Value)
			}
			return true
		})
	}
	if inspected == 0 {
		t.Fatal("no production Go files inspected")
	}
}

func deterministicRanking() ([]byte, error) {
	euro, err := currency.Parse("EUR")
	if err != nil {
		return nil, err
	}
	moneyContext, err := money.CustomContext(2)
	if err != nil {
		return nil, err
	}
	amounts := map[string]string{"free": "0.00", "small": "0.60", "large": "1.20"}
	entries := make([]gomoney.Entry, 0, len(amounts))
	for _, typeID := range []string{"large", "free", "small"} {
		cost, parseErr := money.Parse(amounts[typeID], euro, moneyContext)
		if parseErr != nil {
			return nil, parseErr
		}
		entries = append(entries, gomoney.Entry{TypeID: typeID, Cost: cost})
	}
	costs, err := gomoney.NewFromEntries(entries, gomoney.DefaultPolicy())
	if err != nil {
		return nil, err
	}

	type rankedPlan struct {
		name string
		plan knapsack.Plan
	}
	specs := []struct {
		name    string
		typeIDs []string
	}{
		{name: "two-small", typeIDs: []string{"small", "small"}},
		{name: "one-large", typeIDs: []string{"large"}},
		{name: "one-small", typeIDs: []string{"small"}},
		{name: "free", typeIDs: []string{"free"}},
	}
	plans := make([]rankedPlan, len(specs))
	for index, spec := range specs {
		plan, planErr := planWithoutTesting(spec.typeIDs...)
		if planErr != nil {
			return nil, planErr
		}
		plans[index] = rankedPlan{name: spec.name, plan: plan}
	}
	var comparisonErr error
	slices.SortStableFunc(plans, func(left, right rankedPlan) int {
		comparison, compareErr := costs.Compare(left.plan, right.plan)
		if compareErr != nil && comparisonErr == nil {
			comparisonErr = compareErr
		}
		return comparison
	})
	if comparisonErr != nil {
		return nil, comparisonErr
	}

	var output bytes.Buffer
	for _, ranked := range plans {
		total, totalErr := costs.Total(ranked.plan)
		if totalErr != nil {
			return nil, totalErr
		}
		_, _ = fmt.Fprintf(&output, "%s|%s|%s\n", ranked.name, total.Amount(), ranked.plan.CanonicalString())
	}
	return output.Bytes(), nil
}

func planWithoutTesting(typeIDs ...string) (knapsack.Plan, error) {
	containers := make([]knapsack.ContainerInstance, len(typeIDs))
	for index, typeID := range typeIDs {
		containers[index] = knapsack.ContainerInstance{
			ID: fmt.Sprintf("container-%d", index), TypeID: typeID,
		}
	}
	return knapsack.NewPlan(knapsack.PlanSpec{
		Containers: containers, Status: knapsack.StatusFeasible,
		Termination: knapsack.TerminationCompleted,
	})
}

func exactMoneyRequest(t testing.TB) knapsack.Request {
	return exactMoneyRequestWithContainerPriorities(t, 0, 0, false)
}

func exactMoneyRequestWithContainerPriorities(t testing.TB, smallPriority, largePriority int64, equalCapacity bool) knapsack.Request {
	t.Helper()
	quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	itemDimensions := knapsack.PhysicalDimensions{
		X: quantity(2, measurement.Metre),
		Y: quantity(1, measurement.Metre),
		Z: quantity(1, measurement.Metre),
	}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		item, err := knapsack.NewItem(knapsack.ItemSpec{
			ID: id, Dimensions: itemDimensions,
			Weight:       quantity(1, measurement.Kilogram),
			Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		})
		if err != nil {
			t.Fatal(err)
		}
		items[index] = item
	}
	smallDimensions := itemDimensions
	if equalCapacity {
		smallDimensions.X = quantity(4, measurement.Metre)
	}
	small, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "small", InternalDimensions: smallDimensions,
		MaxContentWeight: quantity(2, measurement.Kilogram),
		Stock:            knapsack.UnlimitedStock(),
		Priority:         smallPriority,
	})
	if err != nil {
		t.Fatal(err)
	}
	large, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "large",
		InternalDimensions: knapsack.PhysicalDimensions{
			X: quantity(4, measurement.Metre),
			Y: quantity(1, measurement.Metre),
			Z: quantity(1, measurement.Metre),
		},
		MaxContentWeight: quantity(2, measurement.Kilogram),
		Stock:            knapsack.UnlimitedStock(),
		Priority:         largePriority,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewRequest(
		items,
		[]knapsack.ContainerType{small, large},
		knapsack.Resolution{
			Length: quantity(1, measurement.Metre),
			Mass:   quantity(1, measurement.Kilogram),
		},
		knapsack.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func canceledContext(cancelled bool) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if cancelled {
		cancel()
	}
	return ctx, cancel
}
