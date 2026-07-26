package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

var (
	version  = "development"
	revision = "unbound"
)

type adapterOutput struct {
	AdapterSchema          string            `json:"adapter_schema"`
	Implementation         string            `json:"implementation"`
	ImplementationVersion  string            `json:"implementation_version"`
	ImplementationRevision string            `json:"implementation_revision"`
	RuntimeVersion         string            `json:"runtime_version"`
	Timing                 timingDisclosure  `json:"timing"`
	Containers             []outputContainer `json:"containers"`
}

type timingDisclosure struct {
	ProcessStartupIncluded     bool  `json:"process_startup_included"`
	AutoloadAndFixtureIncluded bool  `json:"autoload_and_fixture_setup_included"`
	VerificationIncluded       bool  `json:"verification_included"`
	SolveNanoseconds           int64 `json:"solve_nanoseconds"`
}

type outputContainer struct {
	ID         string            `json:"id"`
	TypeID     string            `json:"type_id"`
	Placements []outputPlacement `json:"placements"`
}

type outputPlacement struct {
	ItemID string `json:"item_id"`
	X      int64  `json:"x"`
	Y      int64  `json:"y"`
	Z      int64  `json:"z"`
	Width  int64  `json:"width"`
	Length int64  `json:"length"`
	Depth  int64  `json:"depth"`
	Weight int64  `json:"weight"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if value := os.Getenv("COMPARE_DELAY_MS"); value != "" {
		delay, err := strconv.ParseUint(value, 10, 16)
		if err != nil || delay > 60_000 {
			return errors.New("COMPARE_DELAY_MS must be an integer from 0 through 60000")
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	request, err := comparisonRequest()
	if err != nil {
		return err
	}
	started := time.Now()
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	solveNanoseconds := time.Since(started).Nanoseconds()
	if err != nil {
		return err
	}
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		return fmt.Errorf("adapter solver returned invalid plan: %v", result.Violations())
	}
	output := adapterOutput{
		AdapterSchema:          "v2",
		Implementation:         "github.com/faustbrian/golib/pkg/knapsack",
		ImplementationVersion:  version,
		ImplementationRevision: revision,
		RuntimeVersion:         runtime.Version(),
		Timing: timingDisclosure{
			SolveNanoseconds: solveNanoseconds,
		},
	}
	for _, container := range plan.Containers() {
		entry := outputContainer{ID: container.ID, TypeID: container.TypeID}
		for _, placement := range plan.Placements() {
			if placement.ContainerID != container.ID {
				continue
			}
			entry.Placements = append(entry.Placements, outputPlacement{
				ItemID: placement.ItemID,
				X:      placement.Origin.X,
				Y:      placement.Origin.Y,
				Z:      placement.Origin.Z,
				Width:  placement.Dimensions.X,
				Length: placement.Dimensions.Y,
				Depth:  placement.Dimensions.Z,
				Weight: placement.Weight,
			})
		}
		output.Containers = append(output.Containers, entry)
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}

func comparisonRequest() (knapsack.NormalizedRequest, error) {
	dimensions := geometry.Dimensions{X: 2, Y: 1, Z: 1}
	orientations, err := geometry.Orientations(dimensions)
	if err != nil {
		return knapsack.NormalizedRequest{}, err
	}
	return knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{
			{ID: "a", Dimensions: dimensions, Weight: 1, Orientations: orientations},
			{ID: "b", Dimensions: dimensions, Weight: 1, Orientations: orientations},
		},
		Containers: []knapsack.NormalizedContainer{{
			ID: "box", Dimensions: geometry.Dimensions{X: 4, Y: 1, Z: 1},
			MaxContentWeight: 2, Stock: knapsack.UnlimitedStock(),
		}},
		Resolution: knapsack.Resolution{
			Length: measurement.MustNew(decimal.New(1), measurement.Millimetre),
			Mass:   measurement.MustNew(decimal.New(1), measurement.Gram),
		},
		Limits: knapsack.DefaultLimits(),
	})
}
